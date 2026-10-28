package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/resolve"
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
	return trackerJob{w: w, env: environFor(d, w)}, true
}

// runTrackerChecks runs the jobs in parallel, each under trackerBudget, in workspace order.
func runTrackerChecks(d Deps, jobs []trackerJob) []Finding {
	if len(jobs) == 0 {
		return nil
	}
	out := make([][]Finding, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), trackerBudget)
			defer cancel()
			acct, err := d.TrackerCheck(ctx, j.w, j.env)
			if err != nil && ctx.Err() != nil {
				err = fmt.Errorf("timed out after %s", trackerBudget)
			}
			out[i] = trackerFindings(j.w, acct, err)
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
			trackerFix(w.Name, t, secret)}}
	}
	return []Finding{{"warn", "tracker-auth-failed", w.Name, label + ": " + err.Error(), trackerFix(w.Name, t, secret)}}
}

// trackerFix is the login step for the tracker's preferred source.
func trackerFix(ws string, t *manifest.Tracker, secret string) string {
	switch t.AuthOrDefault() {
	case "cli":
		switch t.Provider {
		case "github":
			return "tq login " + ws + " gh"
		case "azure-devops":
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
	file := filepath.Join(paths.IdentityDir(w.Name, "claude"), ".claude.json")
	servers := readMCPServers(file)
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
			fmt.Sprintf("%s %s: no MCP server for it in %s (ignore this if you connected it on claude.ai)", s.kind, s.svc.Provider, file),
			fmt.Sprintf("add %q to claude.bundle.mcp and the tq bundle catalog, run tq bundle sync %s, then /mcp in a Claude session", want, w.Name))
	}
}

// readMCPServers is the mcpServers key set of a .claude.json; a missing or unreadable file is
// empty.
func readMCPServers(file string) []string {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var root struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	out := make([]string, 0, len(root.MCPServers))
	for k := range root.MCPServers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// matchMCP finds the server for a provider: the catalog's mcp name exactly, else a key that
// contains the provider id (case-insensitive), as in "slack-mcp".
func matchMCP(servers []string, want, provider string) (string, bool) {
	for _, s := range servers {
		if strings.EqualFold(s, want) {
			return s, true
		}
	}
	for _, s := range servers {
		if strings.Contains(strings.ToLower(s), strings.ToLower(provider)) {
			return s, true
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
