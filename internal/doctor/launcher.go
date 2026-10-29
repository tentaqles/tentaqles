package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Owned by WS2 (spec E §8): checks about the claude/codex launcher.

func init() {
	Register(Check{Name: "launcher", Workspace: permissionModeOverridden, Global: claudeAliasShadow})
}

// shellAliasRe matches an alias of claude or codex in a shell profile, which
// wins over the activate block's functions (bash and zsh expand aliases
// before functions, PowerShell resolves aliases before functions).
var shellAliasRe = map[string]*regexp.Regexp{
	"posix":      regexp.MustCompile(`(?m)^[ \t]*alias[ \t]+(claude|codex)=`),
	"fish":       regexp.MustCompile(`(?m)^[ \t]*alias[ \t]+(claude|codex)([ \t=])`),
	"powershell": regexp.MustCompile(`(?mi)^[ \t]*(?:set-alias|new-alias|sal|nal)[ \t]+(?:-name[ \t]+)?["']?(claude|codex)\b`),
}

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

// claudeAliasShadow warns about a claude or codex alias in a shell profile:
// it bypasses the launcher functions `tq activate` defines (spec E §8.2), so
// that claude starts without tq's mode and bypass handling.
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
			for _, m := range shellAliasRe[pf[1]].FindAllStringSubmatch(string(raw), -1) {
				add("warn", "claude-alias-shadow", "",
					fmt.Sprintf("alias %s bypasses tq's launcher; remove it (%s)", m[1], pf[0]),
					fmt.Sprintf("delete the %s alias from %s; tq activate defines %s", m[1], pf[0], m[1]))
			}
		}
	}
}

// permissionModeOverridden notes a claude.permission_mode other than bypass:
// the launcher starts claude in bypass anyway (spec E §8.1, §19 Q3), and the
// value only sets defaultMode for sessions started outside it.
func permissionModeOverridden(w *resolve.Workspace, _ Deps, add AddFunc) {
	if w == nil || w.Manifest == nil {
		return
	}
	pm := w.Manifest.Claude.PermissionMode
	if pm == "" || pm == "bypass" {
		return
	}
	add("info", "permission-mode-overridden", w.Name,
		fmt.Sprintf("claude.permission_mode is %s; the claude launcher starts in bypass anyway", pm),
		"it still sets permissions.defaultMode for IDE, desktop and hub sessions; tq's guard mode is the policy layer for launched sessions")
}
