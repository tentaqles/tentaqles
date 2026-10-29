package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Owned by WS2 (spec E §8): checks about the claude/codex launcher.

func init() {
	Register(Check{Name: "launcher", Workspace: permissionModeOverridden, Global: claudeAliasShadow})
}

// shellAliasRe matches an alias of claude or codex in a shell profile and
// captures its value (posix and fish), or any PowerShell alias of them.
var shellAliasRe = map[string]*regexp.Regexp{
	"posix":      regexp.MustCompile(`(?m)^[ \t]*alias[ \t]+(claude|codex)=['"]?([^'"\n]*)`),
	"fish":       regexp.MustCompile(`(?m)^[ \t]*alias[ \t]+(claude|codex)(?:[ \t]+|=)['"]?([^'"\n]*)`),
	"powershell": regexp.MustCompile(`(?mi)^[ \t]*(?:set-alias|new-alias|sal|nal)[ \t]+(?:-name[ \t]+)?["']?(claude|codex)\b`),
}

// shellFuncRe matches a function named claude or codex (not claude-off and
// the like): defined after tq's activation, it replaces tq's launcher.
var shellFuncRe = map[string]*regexp.Regexp{
	"posix":      regexp.MustCompile(`(?m)^[ \t]*(?:function[ \t]+(claude|codex)(?:[ \t({]|$)|(claude|codex)[ \t]*\(\))`),
	"fish":       regexp.MustCompile(`(?m)^[ \t]*function[ \t]+(claude|codex)(?:[ \t;]|$)`),
	"powershell": regexp.MustCompile(`(?mi)^[ \t]*function[ \t]+(?:global:|script:)?(claude|codex)(?:[ \t({]|$)`),
}

// activationRe finds the line that runs tq activate (or tq's managed block).
var activationRe = regexp.MustCompile(`(?m)^.*(?:tq(?:\.exe)?["']?[ \t]+activate\b|# <<< tq <<<).*$`)

// legacyBlockRe is the preserved pre-tq setup tq migrate keeps; it runs only
// under TQ_ENABLED=0, so its functions do not shadow the launcher.
var legacyBlockRe = regexp.MustCompile(`(?s)# >>> tq-legacy >>>.*?# <<< tq-legacy <<<`)

// profileFiles are the shell profiles under home that the check reads.
func profileFiles(home string) [][2]string {
	out := [][2]string{}
	for _, f := range []string{".bashrc", ".bash_profile", ".bash_aliases", ".profile", ".zshrc"} {
		out = append(out, [2]string{filepath.Join(home, f), "posix"})
	}
	out = append(out, [2]string{filepath.Join(home, ".config", "fish", "config.fish"), "fish"})
	for _, d := range []string{"PowerShell", "WindowsPowerShell"} {
		out = append(out, [2]string{filepath.Join(home, "Documents", d, "Microsoft.PowerShell_profile.ps1"), "powershell"})
	}
	return out
}

// homes are the home folders to scan: HOME and USERPROFILE (they differ under
// Git Bash on some boxes), else the OS home.
func homes(d Deps) []string {
	env := d.Env
	if env == nil {
		env = os.LookupEnv
	}
	var out []string
	seen := map[string]bool{}
	for _, k := range []string{"HOME", "USERPROFILE"} {
		if v, ok := env(k); ok && v != "" && !seen[filepath.Clean(v)] {
			seen[filepath.Clean(v)] = true
			out = append(out, v)
		}
	}
	if len(out) == 0 && d.Env == nil {
		if h, err := os.UserHomeDir(); err == nil {
			out = append(out, h)
		}
	}
	return out
}

// claudeAliasShadow reports what in a shell profile stands in for the
// claude and codex launchers `tq activate` defines (spec E §8.2):
//   - a PowerShell alias (aliases resolve before functions): warn;
//   - a function named claude or codex defined after tq's activation, such
//     as the wrapper an older tq migrate carried over: warn;
//   - a posix or fish alias that runs something else (an absolute path, a
//     different command): warn;
//   - a posix or fish alias whose value starts with the same name (alias
//     claude='claude --flag'): it calls tq's launcher with extra flags, so
//     it is only a note.
func claudeAliasShadow(d Deps, add AddFunc) {
	read := d.ReadFile
	if read == nil {
		read = os.ReadFile
	}
	for _, home := range homes(d) {
		for _, pf := range profileFiles(home) {
			raw, err := read(pf[0])
			if err != nil {
				continue
			}
			text := legacyBlockRe.ReplaceAllStringFunc(string(raw), func(b string) string {
				return strings.Repeat("\n", strings.Count(b, "\n"))
			})
			for _, m := range shellAliasRe[pf[1]].FindAllStringSubmatch(text, -1) {
				name := m[1]
				if pf[1] == "powershell" {
					add("warn", "claude-alias-shadow", "",
						fmt.Sprintf("alias %s replaces tq's %s launcher: PowerShell resolves aliases before functions (%s)", name, name, pf[0]),
						fmt.Sprintf("delete the %s alias from %s; tq activate defines %s", name, pf[0], name))
					continue
				}
				if f := strings.Fields(m[2]); len(f) > 0 && f[0] == name {
					add("info", "claude-alias-shadow", "",
						fmt.Sprintf("alias %s adds flags to tq's %s launcher, which it calls (%s)", name, name, pf[0]),
						fmt.Sprintf("the launcher already starts %s in bypass; delete the alias from %s if its flags were for the old setup", name, pf[0]))
					continue
				}
				add("warn", "claude-alias-shadow", "",
					fmt.Sprintf("alias %s runs %q instead of tq's %s launcher (%s)", name, m[2], name, pf[0]),
					fmt.Sprintf("delete the %s alias from %s; tq activate defines %s", name, pf[0], name))
			}
			after := -1
			if loc := activationRe.FindAllStringIndex(text, -1); len(loc) > 0 {
				after = loc[len(loc)-1][1]
			}
			if after < 0 {
				continue
			}
			for _, loc := range shellFuncRe[pf[1]].FindAllStringSubmatchIndex(text, -1) {
				if loc[0] < after {
					continue
				}
				name := ""
				for i := 2; i+1 < len(loc); i += 2 {
					if loc[i] >= 0 {
						name = text[loc[i]:loc[i+1]]
						break
					}
				}
				add("warn", "claude-alias-shadow", "",
					fmt.Sprintf("function %s, defined after tq's activation, replaces tq's %s launcher (%s)", name, name, pf[0]),
					fmt.Sprintf("delete function %s from %s (tq migrate removes the copy it carried over, if unchanged); tq activate defines %s", name, pf[0], name))
			}
		}
	}
}

// permissionModeOverridden notes a claude.permission_mode other than bypass
// (or bypassPermissions, which ModeFor passes through as bypass):
// the launcher starts claude in bypass anyway (spec E §8.1, §19 Q3), and the
// value only sets defaultMode for sessions started outside it.
func permissionModeOverridden(w *resolve.Workspace, _ Deps, add AddFunc) {
	if w == nil || w.Manifest == nil {
		return
	}
	pm := w.Manifest.Claude.PermissionMode
	if pm == "" || pm == "bypass" || pm == "bypassPermissions" {
		return
	}
	add("info", "permission-mode-overridden", w.Name,
		fmt.Sprintf("claude.permission_mode is %s; the claude launcher starts in bypass anyway", pm),
		"it still sets permissions.defaultMode for IDE, desktop and hub sessions; tq's guard mode is the policy layer for launched sessions")
}
