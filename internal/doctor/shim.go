package doctor

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/envplan"
)

// scriptExts are the extensions a PATH entry can have and still be a script
// that runs arbitrary code before the real CLI. A real binary (.exe, or an
// extensionless ELF/Mach-O) cannot rewrite its own environment this way.
var scriptExts = map[string]bool{
	".cmd": true, ".bat": true, ".ps1": true, ".sh": true, ".bash": true,
}

// tqEnvRe matches a script asking tq to recompute the identity for the current
// directory -- `tq env --shell cmd`, `"%LOCALAPPDATA%\tentaqles\bin\tq.exe" env`.
var tqEnvRe = regexp.MustCompile(`(?i)tq(\.exe)?"?\s+env\b`)

// shimMaxBytes caps how much of a script is read. Shims are tiny; anything
// larger is almost certainly a real program that happens to ship as a script.
const shimMaxBytes = 64 << 10

// shimShadow reports whether resolved -- the file a CLI name resolves to on
// PATH -- is a script that assigns the identity variables itself.
//
// This matters because tq exports the identity into the environment and then
// execs the CLI by name (`tq run <ws> -- gh ...`, `tq login <ws> claude`). A
// script earlier on PATH that recomputes the identity from the *current
// directory* silently discards the workspace the user asked for, so the
// command runs against the wrong account and nothing reports it. Folder-based
// resolution looks right in everyday use, which is exactly why this is worth
// a finding rather than being left to be noticed.
//
// It returns the reason to show the user and whether a shim was found.
func shimShadow(resolved string, vars []string, readFile func(string) ([]byte, error)) (string, bool) {
	if resolved == "" {
		return "", false
	}
	if !scriptExts[strings.ToLower(filepath.Ext(resolved))] {
		return "", false
	}
	if readFile == nil {
		readFile = os.ReadFile
	}
	raw, err := readFile(resolved)
	if err != nil {
		return "", false
	}
	if len(raw) > shimMaxBytes {
		raw = raw[:shimMaxBytes]
	}
	body := string(raw)

	var assigns []string
	for _, v := range vars {
		if v == "" {
			continue
		}
		// An assignment, not a read: `SET "CLAUDE_CONFIG_DIR=..."` and
		// `$env:CLAUDE_CONFIG_DIR = "..."` match; `%CLAUDE_CONFIG_DIR%` and
		// `$CLAUDE_CONFIG_DIR` do not.
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(v) + `\s*=`).MatchString(body) {
			assigns = append(assigns, v)
		}
	}
	sort.Strings(assigns)

	switch {
	case len(assigns) > 0:
		return "it sets " + strings.Join(assigns, ", ") + " itself", true
	case tqEnvRe.MatchString(body):
		return "it re-runs `tq env` for the current directory", true
	}
	return "", false
}

// identityVars is the set of environment variables a provider uses to point a
// CLI at a workspace's private config home.
func identityVars(p envplan.Provider) []string {
	if p.Vars == nil {
		return nil
	}
	m := p.Vars("{dir}")
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
