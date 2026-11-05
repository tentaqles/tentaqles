package cmd

import (
	"os"
	"path/filepath"

	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/protect"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// The hook's side of the self-protection category (PRD v2 §7.1, §8.2;
// ADR-0005): the protected path set is computed from what the hook already
// resolved, and every deny of every category lands in the denial log.

// hookExecutable is os.Executable; tests replace it.
var hookExecutable = os.Executable

// protectedSet computes the protected path set of this hook run: tq's
// state folder, the markers of the registered workspaces, the resolved
// identity's harness config homes (the hook registration and the installed
// plugin), the session's $CLAUDE_CONFIG_DIR and $CODEX_HOME, the plugin in
// the default ~/.claude, and the running tq binary. ws may be nil (a
// neutral or untrusted cwd): the state folder and the markers are
// protected all the same.
func protectedSet(ws *resolve.Workspace, cfg *registry.Config) *protect.Set {
	in := protect.Inputs{Home: paths.Home(), Env: os.Getenv}
	if u, err := os.UserHomeDir(); err == nil {
		in.UserHome = u
		// The plugin may be installed in the default config home too.
		in.ConfigDirs = append(in.ConfigDirs, filepath.Join(u, ".claude"))
	}
	if cfg != nil {
		in.Bases = cfg.Bases
	}
	if ws != nil {
		in.Roots = []string{ws.Root}
		in.ConfigDirs = append(in.ConfigDirs, paths.IdentityDir(ws.Name, "claude"))
		in.CodexDirs = append(in.CodexDirs, paths.IdentityDir(ws.Name, "codex"))
	}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		in.ConfigDirs = append(in.ConfigDirs, d)
	}
	if d := os.Getenv("CODEX_HOME"); d != "" {
		in.CodexDirs = append(in.CodexDirs, d)
	}
	if exe, err := hookExecutable(); err == nil {
		in.Executable = exe
	}
	return protect.New(in)
}

// hookWSName names a workspace for the log, "" for none.
func hookWSName(ws *resolve.Workspace) string {
	if ws == nil {
		return ""
	}
	return ws.Name
}

// denialSubject is what the log records for a call: the command line of a
// shell tool, the path of a file tool, and the tool name of anything else
// (an MCP input or a patch may carry content, which is never logged).
func denialSubject(call policy.ToolCall) string {
	switch {
	case isApplyPatch(call):
		return call.Tool
	case call.IsShell() && call.Command != "":
		return call.Command
	case call.Path != "":
		return call.Path
	}
	return call.Tool
}

// denyKind is the category a policy deny belongs to, for the log.
func denyKind(d policy.Decision) string {
	if len(d.Matched) > 0 && d.Matched[0].Category != "" {
		return d.Matched[0].Category
	}
	return guardErrorID
}

// logHookDenial appends one line to the denial log (PRD v2 §7.3): the
// category, the layer, the tool, the command or path, the rule, the reason
// the model saw and the workspace; never the environment, never file
// content. A log that cannot be written changes nothing: the deny stands.
func logHookDenial(kind, tool, subject, rule, reason, ws, cwd string) {
	_ = audit.AppendDenial(audit.Denial{
		Kind: kind, Layer: audit.LayerHook, Tool: tool, Command: subject,
		Rule: rule, Reason: reason, Workspace: ws, PID: os.Getpid(), Cwd: cwd,
	})
}
