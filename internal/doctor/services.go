package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/redact"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/verify"
)

// Company services (spec B §8): the tracker check (Adapter.Check through the registry) and the
// chat/email checks (the MCP entry in the identity's .claude.json).

// TrackerCheckFunc verifies a workspace's tracker credential and returns the account it resolves
// to. cmd wires the adapter registry and the credential resolver; nil (tests, the hook path)
// skips the check.
type TrackerCheckFunc func(ctx context.Context, w *resolve.Workspace, env []string) (account string, err error)

// trackerBudget bounds one tracker check: it can make a network call or run a CLI, and doctor
// must never stall.
var trackerBudget = 10 * time.Second

type trackerJob struct {
	w   *resolve.Workspace
	env []string
}

// trackerJobFor is the tracker check for w, when it has a tracker block and a checker is wired.
func trackerJobFor(d Deps, w *resolve.Workspace) (trackerJob, bool) {
	// --verify off runs no account checks at all, the tracker's included.
	if d.TrackerCheck == nil || d.VerifyMode == VerifyOff || !w.Manifest.HasTracker() {
		return trackerJob{}, false
	}
	// Under auto (the default) a subprocess or network call is spent only where the answer
	// matters: the tracker of the workspace the user is in. --verify all checks every tracker.
	if d.VerifyMode != VerifyAll && (d.Cwd == "" || !withinDir(w.Root, d.Cwd)) {
		return trackerJob{}, false
	}
	return trackerJob{w: w, env: environFor(d, w)}, true
}

// runTrackerChecks runs the jobs in parallel, each under trackerBudget, in workspace order.
func runTrackerChecks(d Deps, jobs []trackerJob) []Finding {
	if len(jobs) == 0 {
		return nil
	}
	// The budget is enforced around the call, not only through ctx: a checker that ignores ctx (a
	// keychain prompt, a CLI run without CommandContext) is reported as timed out and its
	// goroutine is left behind, so doctor never waits on it.
	type result struct {
		acct string
		err  error
	}
	out := make([][]Finding, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), trackerBudget)
			defer cancel()
			done := make(chan result, 1) // buffered: a late checker never blocks on its send
			go func() {
				acct, err := d.TrackerCheck(ctx, j.w, j.env)
				done <- result{acct, err}
			}()
			timedOut := fmt.Errorf("timed out after %s", trackerBudget)
			select {
			case r := <-done:
				if r.err != nil && ctx.Err() != nil {
					r.err = timedOut
				}
				out[i] = trackerFindings(j.w, r.acct, r.err)
			case <-ctx.Done():
				out[i] = trackerFindings(j.w, "", timedOut)
			}
		}()
	}
	wg.Wait()
	idx := make([]int, len(jobs))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return jobs[idx[a]].w.Name < jobs[idx[b]].w.Name })
	var fs []Finding
	for _, i := range idx {
		fs = append(fs, out[i]...)
	}
	return fs
}

func trackerFindings(w *resolve.Workspace, account string, err error) []Finding {
	t := w.Manifest.Tracker
	scope := t.ScopeKey()
	if scope == "" {
		scope = "no scope"
	}
	label := fmt.Sprintf("tracker %s (%s, auth %s)", t.Provider, scope, t.AuthOrDefault())
	secret := "tq secret set " + w.Name + " " + t.Provider
	switch {
	case err == nil:
		who := account
		if who == "" {
			who = "an account tq could not name"
		}
		return []Finding{{"ok", "tracker-ok", w.Name, label + ": signed in as " + who, ""}}
	case errors.Is(err, hubsync.ErrNoAdapter):
		return []Finding{{"info", "tracker-no-adapter", w.Name, label + ": this tq build has no sync adapter for " + t.Provider,
			"use /tentaqles:sync from a Claude session, or update tq"}}
	case errors.Is(err, cred.ErrNeedsSession):
		return []Finding{{"info", "tracker-needs-session", w.Name, label + ": no background credential, so only a Claude session can sync",
			trackerFix(w.Name, w.Manifest, secret)}}
	}
	return []Finding{{"warn", "tracker-auth-failed", w.Name, label + ": " + trackerErrText(w, err), trackerFix(w.Name, w.Manifest, secret)}}
}

// trackerErrText renders a tracker error for a finding. The error comes from the cred chain and
// the adapters (CLI stderr, HTTP bodies) and the finding goes to the terminal, --json and the
// onboard skill's prompt, so it is redacted with the workspace's exit classes, then cut to one
// bounded line with the verify token sweep (spec §12: never in prompts).
func trackerErrText(w *resolve.Workspace, err error) string {
	classes := []string{"all"}
	if m := w.Manifest; m != nil && m.Data.Enabled() {
		classes = append(append([]string(nil), m.Data.Classes...), "secrets")
	}
	return verify.SafeError(errors.New(redact.Redact(err.Error(), classes)))
}

// trackerFix is the login step for the tracker's preferred source.
func trackerFix(ws string, m *manifest.Manifest, secret string) string {
	t := m.Tracker
	switch t.AuthOrDefault() {
	case "cli":
		switch t.Provider {
		case "github":
			return "tq login " + ws + " gh"
		case "azure-devops":
			// The az identity the manifest actually has: azure-devops when listed, else az (the
			// same CLI).
			ids := m.IdentityNames()
			if !slices.Contains(ids, "azure-devops") && slices.Contains(ids, "az") {
				return "tq login " + ws + " az"
			}
			return "tq login " + ws + " azure-devops"
		case "jira":
			return "acli jira auth login, or " + secret
		}
		return secret
	case "keychain":
		return secret
	case "env":
		return "set " + t.Env + " where tq dotenv resolves it"
	}
	return "/tentaqles:sync in a Claude session"
}

// mcpServiceChecks reports whether each enabled chat/email provider has its MCP entry in the
// workspace's Claude identity (<identity>/.claude.json mcpServers). Claude.ai connectors live in
// the account, not that file, so a missing entry is a warning, never an error.
func mcpServiceChecks(w *resolve.Workspace, add func(level, code, ws, msg, fix string)) {
	m := w.Manifest
	if !m.Chat.Enabled() && !m.Email.Enabled() {
		return
	}
	idDir := paths.IdentityDir(w.Name, "claude")
	file := filepath.Join(idDir, ".claude.json")
	projectMCP := filepath.Join(w.Root, ".mcp.json")
	servers := mcpServerNames(file, w.Root, filepath.Join(idDir, ".mcp.json"), projectMCP)
	cat := providers.MustLoad()
	for _, s := range []struct {
		kind string
		svc  *manifest.Service
	}{{"chat", m.Chat}, {"email", m.Email}} {
		if !s.svc.Enabled() {
			continue
		}
		want := s.svc.Provider
		if p, ok := cat.Get(want); ok {
			want = p.MCPName()
		}
		if name, ok := matchMCP(servers, want, s.svc.Provider); ok {
			add("ok", s.kind+"-ok", w.Name, fmt.Sprintf("%s %s: MCP server %q is configured", s.kind, s.svc.Provider, name), "")
			continue
		}
		add("warn", s.kind+"-mcp-missing", w.Name,
			fmt.Sprintf("%s %s: no MCP server for it in %s (global or this folder's project entry) or %s (ignore this if you connected it on claude.ai)", s.kind, s.svc.Provider, file, projectMCP),
			fmt.Sprintf("add %q to claude.bundle.mcp and the tq bundle catalog, run tq bundle sync %s, then /mcp in a Claude session", want, w.Name))
	}
}

// mcpServerNames is the union of MCP server names (keys only; values stay unparsed, so no env or
// header token is ever read) from the identity's .claude.json (top-level mcpServers and
// projects[<root or a folder under it>].mcpServers) and from each extra .mcp.json. A missing or
// unreadable file contributes nothing.
func mcpServerNames(claudeJSON, root string, mcpJSONs ...string) []string {
	set := map[string]bool{}
	add := func(m map[string]json.RawMessage) {
		for k := range m {
			set[k] = true
		}
	}
	type servers struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if raw, err := os.ReadFile(claudeJSON); err == nil {
		var cj struct {
			servers
			Projects map[string]servers `json:"projects"`
		}
		if json.Unmarshal(raw, &cj) == nil {
			add(cj.MCPServers)
			for p, s := range cj.Projects {
				if root != "" && withinDir(root, p) {
					add(s.MCPServers)
				}
			}
		}
	}
	for _, f := range mcpJSONs {
		if raw, err := os.ReadFile(f); err == nil {
			var s servers
			if json.Unmarshal(raw, &s) == nil {
				add(s.MCPServers)
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// withinDir reports whether p (a .claude.json project key, often with forward slashes) is root or
// a folder under it. filepath.Rel compares case-insensitively on Windows.
func withinDir(root, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(filepath.FromSlash(p)))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// matchMCP finds the server for a provider: the catalog's mcp name exactly, else a key that has
// the provider id as a whole token (split on - _ . and spaces, case-insensitive), as in
// "slack-mcp" or "ms-Teams-mcp", never "steamship".
func matchMCP(servers []string, want, provider string) (string, bool) {
	for _, s := range servers {
		if strings.EqualFold(s, want) {
			return s, true
		}
	}
	split := func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == ' ' }
	for _, s := range servers {
		for _, tok := range strings.FieldsFunc(s, split) {
			if strings.EqualFold(tok, provider) {
				return s, true
			}
		}
	}
	return "", false
}

// finish adds "all checks passed" when nothing but ok findings were reported.
func finish(fs []Finding) []Finding {
	for _, f := range fs {
		if f.Level != "ok" {
			return fs
		}
	}
	return append(fs, Finding{"ok", "ok", "", "all checks passed", ""})
}
