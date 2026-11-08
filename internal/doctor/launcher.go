package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Owned by WS2 (spec E §8): checks about the claude/codex launcher.

func init() {
	Register(Check{Name: "launcher", Global: claudeAliasShadow})
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

// legacyBlockRe is a preserved pre-tq setup an older tq left in a profile; it runs only
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
//     as a wrapper an older tq carried over: warn;
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
				first := strings.Count(text[:loc[0]], "\n") + 1
				fix := fmt.Sprintf("delete lines %d-%d of %s (function %s) by hand; tq activate defines %s", first, funcEndLine(text, loc[0], pf[1]), pf[0], name, name)
				add("warn", "claude-alias-shadow", "",
					fmt.Sprintf("function %s, defined after tq's activation, replaces tq's %s launcher (%s)", name, name, pf[0]),
					fix)
			}
		}
	}
}

// funcEndLine is the 1-based line where the function starting at off ends:
// the line of its closing brace (fish: its `end` line), or the start line
// when that cannot be found.
func funcEndLine(text string, off int, kind string) int {
	start := strings.Count(text[:off], "\n") + 1
	if kind == "fish" {
		depth := 0
		for i, ln := range strings.Split(text[off:], "\n") {
			f := strings.Fields(ln)
			if len(f) == 0 {
				continue
			}
			switch f[0] {
			case "function", "if", "for", "while", "switch", "begin":
				depth++
			case "end":
				depth--
				if depth == 0 {
					return start + i
				}
			}
		}
		return start
	}
	open := strings.IndexByte(text[off:], '{')
	if open < 0 {
		return start
	}
	depth := 0
	for i := off + open; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.Count(text[:i], "\n") + 1
			}
		}
	}
	return start
}
