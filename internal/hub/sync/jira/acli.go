package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	// maxArgv bounds the command line under Windows' 32,767-character limit (CreateProcess), with
	// room for the binary path and quoting. Longer text needs the REST path (a keychain or env token).
	maxArgv = 30000
)

// The acli argv surface below follows Atlassian's acli reference for acli 1.x (the jira workitem
// commands, 2025): search --jql --fields --json --paginate; view <key> --fields --json; create
// --project --type --summary --parent --description --label --json; edit --key --summary
// --description --labels --remove-labels --yes; transition --key --status --yes; comment create
// --key --body. acli itself spells the label flag differently on create (--label) and edit
// (--labels), so both are kept as acli spells them. No test runs a live acli, so a flag acli
// renames shows up as an acli usage error in the sync status, not as a silent miss.

// runFunc runs acli with args, env added to the inherited environment, and returns its stdout.
type runFunc func(ctx context.Context, env []string, args ...string) ([]byte, error)

var lookPath = exec.LookPath

// acliPath finds acli. On Windows a .bat or .cmd shim is refused: Go quotes arguments for
// CommandLineToArgvW, not for cmd.exe, so free text (comments, descriptions) could run commands
// through a batch file. The official acli is an .exe.
func acliPath() (string, error) {
	bin, err := lookPath("acli")
	if err != nil {
		return "", fmt.Errorf("acli is not installed: %w", err)
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(bin)) {
		case ".bat", ".cmd":
			return "", fmt.Errorf("acli at %s is a batch file; tq only runs the acli executable (acli.exe)", bin)
		}
	}
	return bin, nil
}

func runACLI(ctx context.Context, env []string, args ...string) ([]byte, error) {
	n := 0
	for _, a := range args {
		n += len(a) + 3 // a space and quotes
	}
	if n > maxArgv {
		return nil, fmt.Errorf("acli %s: the command line is too long (%d characters, the limit is %d); use a keychain or env token for long text",
			strings.Join(args[:min(3, len(args))], " "), n, maxArgv)
	}
	bin, err := acliPath()
	if err != nil {
		return nil, err
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
//
// The hook cannot see the manifest, so it does not know the workspace's site: the adapter checks
// that acli's site is tracker.scope.org before its first acli call (verifyACLISite).
func cliHook(ctx context.Context, env []string) (cred.Token, error) {
	if _, err := acliPath(); err != nil {
		return cred.Token{}, fmt.Errorf("%w: %v", cred.ErrNoCredential, err)
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

// parseStatus reads acli jira auth status: the account (its Email line, else User or Account)
// and the site it is logged in to.
func parseStatus(out []byte) (account, site string) {
	var other string
	for _, l := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "email":
			if account == "" {
				account = v
			}
		case "user", "account":
			if other == "" {
				other = v
			}
		case "site":
			if site == "" {
				site = v
			}
		}
	}
	if account == "" {
		account = other
	}
	return account, site
}

// siteHost is the lower-cased host of a site URL or bare host ("https://ACME.atlassian.net/" ->
// "acme.atlassian.net").
func siteHost(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		return u.Hostname()
	}
	s, _, _ = strings.Cut(s, "/")
	return s
}

// acliStatus runs acli jira auth status.
func (a *Adapter) acliStatus(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	return a.acli(ctx, "jira", "auth", "status")
}

// verifyACLISite refuses an acli logged in to any site but the workspace's. acli keeps one login
// per OS user, so in a multi-client workspace it may be another client's site; tq would then
// mirror that site's items and write this workspace's results there.
func (a *Adapter) verifyACLISite(ctx context.Context) error {
	out, err := a.acliStatus(ctx)
	if err != nil {
		return err
	}
	want := siteHost(a.base)
	_, site := parseStatus(out)
	fix := fmt.Sprintf("run acli jira auth login for %s, or set tracker.auth to keychain or env for this workspace", want)
	if site == "" {
		return &cred.SourceError{Source: cred.SourceCLI, Provider: Provider,
			Err: fmt.Errorf("acli jira auth status names no site, so tq cannot confirm it is %s; %s", want, fix)}
	}
	if got := siteHost(site); got != want {
		return &cred.SourceError{Source: cred.SourceCLI, Provider: Provider,
			Err: fmt.Errorf("acli is logged in to %s, but this workspace's site (tracker.scope.org) is %s; %s", got, want, fix)}
	}
	return nil
}

// cliCheck reads the account from acli jira auth status (its Email line, else its Site line).
func (a *Adapter) cliCheck(ctx context.Context) (string, error) {
	out, err := a.acliStatus(ctx)
	if err != nil {
		return "", err
	}
	switch account, site := parseStatus(out); {
	case account != "":
		return account, nil
	case site != "":
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
