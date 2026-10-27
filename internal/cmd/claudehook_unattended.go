package cmd

import (
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
	if ws == nil || ws.Manifest == nil || d.Action != policy.Ask {
		return false
	}
	on, scope := guardcfg.Unattended(ws.Manifest, ws.Root, cwd, mode, trust.IsBypassAllowed(ws.Hash))
	if !on {
		return false
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
	guardlog.Append(guardlog.Record{WS: ws.Name, Scope: scope, Mode: mode, Rules: ids, Tool: call.Tool, Command: subject}, ws.Manifest.Data.Classes)
	return true
}
