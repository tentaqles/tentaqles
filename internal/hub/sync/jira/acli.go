package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// The cli credential source for Jira is acli, the Atlassian CLI. acli never hands out its token,
// so its hook returns a marker token (scheme ACLIScheme, no value) that never goes on the wire:
// an adapter whose credential comes from the cli source runs acli for each call instead of REST.

// ACLIScheme is the scheme of the marker token the jira cli hook returns.
const ACLIScheme = "acli"

const (
	statusTimeout = 10 * time.Second
	acliTimeout   = 60 * time.Second
	maxStderr     = 400
)

// runFunc runs acli with args, env added to the inherited environment, and returns its stdout.
type runFunc func(ctx context.Context, env []string, args ...string) ([]byte, error)

var lookPath = exec.LookPath

func runACLI(ctx context.Context, env []string, args ...string) ([]byte, error) {
	bin, err := lookPath("acli")
	if err != nil {
		return nil, fmt.Errorf("acli is not installed: %w", err)
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, acliTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.WaitDelay = 2 * time.Second // a child holding the pipes open never stalls the tick
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if len(msg) > maxStderr {
			msg = msg[:maxStderr] + "..."
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("acli %s: timed out", strings.Join(args[:min(3, len(args))], " "))
		}
		if msg != "" {
			return nil, fmt.Errorf("acli %s: %v: %s", strings.Join(args[:min(3, len(args))], " "), err, msg)
		}
		return nil, fmt.Errorf("acli %s: %v", strings.Join(args[:min(3, len(args))], " "), err)
	}
	return stdout.Bytes(), nil
}

// cliHook is the jira cli source: acli, only when it is installed and logged in. Otherwise it
// reports ErrNoCredential and the resolver falls through to the keychain.
func cliHook(ctx context.Context, env []string) (cred.Token, error) {
	if _, err := lookPath("acli"); err != nil {
		return cred.Token{}, fmt.Errorf("%w: acli is not installed", cred.ErrNoCredential)
	}
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	if _, err := runACLI(ctx, env, "jira", "auth", "status"); err != nil {
		return cred.Token{}, fmt.Errorf("%w: acli is not logged in (acli jira auth login)", cred.ErrNoCredential)
	}
	return cred.Token{Scheme: ACLIScheme}, nil
}

func (a *Adapter) acli(ctx context.Context, args ...string) ([]byte, error) {
	out, err := a.run(ctx, a.cfg.Env, args...)
	if err != nil {
		return nil, &cred.SourceError{Source: cred.SourceCLI, Provider: Provider, Err: err}
	}
	return out, nil
}

// cliCheck reads the account from acli jira auth status (its Email line, else its Site line).
func (a *Adapter) cliCheck(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	out, err := a.acli(ctx, "jira", "auth", "status")
	if err != nil {
		return "", err
	}
	var site string
	for _, l := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "email", "user", "account":
			if v = strings.TrimSpace(v); v != "" {
				return v, nil
			}
		case "site":
			site = strings.TrimSpace(v)
		}
	}
	if site != "" {
		return "acli (" + site + ")", nil
	}
	return "acli", nil
}

// decodeIssues reads acli JSON: an array of issues, or an object holding them under issues.
func decodeIssues(out []byte) ([]issue, error) {
	out = bytes.TrimSpace(out)
	var list []issue
	if err := json.Unmarshal(out, &list); err == nil {
		return list, nil
	}
	var wrapped struct {
		Issues []issue `json:"issues"`
	}
	if err := json.Unmarshal(out, &wrapped); err != nil {
		return nil, errors.New("jira: acli returned no JSON issue list")
	}
	return wrapped.Issues, nil
}

func (a *Adapter) cliSearch(ctx context.Context, jql string) ([]issue, error) {
	out, err := a.acli(ctx, "jira", "workitem", "search", "--jql", jql, "--fields", strings.Join(a.fields(), ","), "--json", "--paginate")
	if err != nil {
		return nil, err
	}
	return decodeIssues(out)
}

func (a *Adapter) cliView(ctx context.Context, id string) (issue, error) {
	out, err := a.acli(ctx, "jira", "workitem", "view", id, "--fields", "*all", "--json")
	if err != nil {
		return issue{}, err
	}
	var is issue
	if err := json.Unmarshal(bytes.TrimSpace(out), &is); err != nil {
		return issue{}, errors.New("jira: acli returned no JSON work item")
	}
	return is, nil
}

func (a *Adapter) cliCreate(ctx context.Context, project string, in hubsync.NewItem, body string) (string, error) {
	typ := in.Type
	if typ == "" {
		typ = a.issueType
		if in.ParentID != "" {
			typ = subtaskType
		}
	}
	args := []string{"jira", "workitem", "create", "--project", project, "--type", typ, "--summary", in.Title}
	if in.ParentID != "" {
		args = append(args, "--parent", in.ParentID)
	}
	if strings.TrimSpace(body) != "" {
		args = append(args, "--description", body)
	}
	if len(in.Labels) > 0 {
		args = append(args, "--label", strings.Join(in.Labels, ","))
	}
	out, err := a.acli(ctx, append(args, "--json")...)
	if err != nil {
		return "", err
	}
	var created struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &created) != nil || (created.Key == "" && created.ID == "") {
		return "", errors.New("jira: acli created the work item but returned no key")
	}
	if created.Key != "" {
		return created.Key, nil
	}
	return created.ID, nil
}
