package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// unattendedSkip reports whether an ask decision is skipped because the
// workspace runs this folder unattended in a bypass or auto session, and
// records the skip. Deny decisions never reach it.
func unattendedSkip(ws *resolve.Workspace, cwd, mode string, call policy.ToolCall, d policy.Decision) bool {
	if ws == nil || ws.Manifest == nil || d.Action != policy.Ask || !trust.IsTrusted(ws.Hash) {
		return false
	}
	on, scope := guardcfg.Unattended(ws.Manifest, ws.Root, cwd, mode, trust.IsBypassAllowed(ws.Hash))
	if !on {
		return false
	}
	// A file tool may reach outside the covered folder: its target must be
	// covered too. Bash and MCP calls stay judged by the session cwd.
	if call.Path != "" {
		abs := call.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(cwd, abs)
		}
		if ok, _ := guardcfg.Covers(ws.Manifest, ws.Root, filepath.Dir(abs)); !ok {
			return false
		}
	}
	ids := make([]string, 0, len(d.Matched))
	for _, r := range d.Matched {
		ids = append(ids, r.ID)
	}
	if !guardcfg.Skippable(ws.Manifest, ids) {
		return false
	}
	subject := call.Command
	if subject == "" {
		subject = call.Path
	}
	if subject == "" {
		subject = call.Content // MCP/Agent input: masked and truncated by Append
	}
	guardlog.Append(guardlog.Record{WS: ws.Name, Scope: scope, Mode: mode, Rules: ids, Tool: call.Tool, Command: subject}, ws.Manifest.Data.Classes)
	return true
}

// unattendedLine is the SessionStart context line for a workspace with a
// guard.unattended block; "" when the block is not configured.
func unattendedLine(ws *resolve.Workspace, cwd string, now time.Time) string {
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Guard.Unattended.Configured() {
		return ""
	}
	u := ws.Manifest.Guard.Unattended
	if !trust.IsBypassAllowed(ws.Hash) {
		return "Unattended: configured, not effective (needs tq allow --bypass " + ws.Name + ")"
	}
	on, scope := guardcfg.Covers(ws.Manifest, ws.Root, cwd)
	if !on {
		what := strings.Join(u.Projects, ", ")
		if u.Workspace {
			what = "the workspace"
		}
		return "Unattended: on for " + what + " (not this folder)"
	}
	n := len(guardlog.Read(ws.Name, now.Add(-24*time.Hour), 1<<20))
	noun := "confirms"
	if n == 1 {
		noun = "confirm"
	}
	return fmt.Sprintf("Unattended: on (%s) · %d %s skipped in 24h · applies in bypass and auto sessions", scope, n, noun)
}
