package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/guard"
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

// denialSubject is what the log records for a call (PRD v2 §7.3, ADR-0005):
// no arguments, only the verb plus the one path or token the denial named.
// A shell tool logs its verb (the first word, after sudo/env/assignments)
// and the subject; a file tool logs its path; an MCP call or a patch logs
// the tool name. Never the rest of the command line, so a secret in an
// argument, a here-doc body, an assignment value or a flag never reaches the
// log.
func denialSubject(call policy.ToolCall, subject string) string {
	switch {
	case isApplyPatch(call):
		return joinVerbSubject(call.Tool, subject) // apply_patch <file>
	case call.IsShell() && call.Command != "":
		return joinVerbSubject(verbOf(call.Command), subject)
	case call.Path != "":
		return call.Path
	}
	return call.Tool
}

// joinVerbSubject combines a verb and a subject for the log, dropping an
// empty part and never repeating the verb (a human-only subject such as
// "tq allow" already begins with it).
func joinVerbSubject(verb, subject string) string {
	switch {
	case verb == "":
		return subject
	case subject == "" || subject == verb || strings.HasPrefix(subject, verb+" "):
		if subject != "" {
			return subject
		}
		return verb
	}
	return verb + " " + subject
}

// verbOf returns the verb of a shell command line for the log: the first
// command's verb (guard.Verb strips a leading sudo/env wrapper and bash
// NAME=value assignments), skipping a PowerShell `$env:X=` / `$x=` assignment
// and moving to the next command, so an assignment holding a token is never
// the logged word.
func verbOf(command string) string {
	for _, c := range guard.Commands(command) {
		v, _, ok := guard.Verb(c.Words)
		if !ok {
			continue
		}
		if strings.HasPrefix(v, "$") || strings.Contains(v, "=") {
			continue // a shell variable assignment, not a verb
		}
		return guard.BaseName(v)
	}
	return ""
}

// denyKind is the category a policy deny belongs to, for the log.
func denyKind(d policy.Decision) string {
	if len(d.Matched) > 0 && d.Matched[0].Category != "" {
		return d.Matched[0].Category
	}
	return guardErrorID
}

// logCommand is what the denial log records as the command of a call (PRD
// v2 §7.3, ADR-0006): a shell line through audit.RedactCommand (here-doc
// and quoted bodies, assignment and credential-flag values, secret-shaped
// spans and long tokens masked, cut); a file tool's path; for an MCP call or
// a patch the tool name, never its input or content.
func logCommand(call policy.ToolCall) string {
	switch {
	case isApplyPatch(call):
		return call.Tool
	case call.IsShell() && call.Command != "":
		return audit.RedactCommand(call.Command)
	case call.Path != "":
		return call.Path
	}
	return call.Tool
}
