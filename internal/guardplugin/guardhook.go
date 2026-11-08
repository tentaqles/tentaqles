package guardplugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// The guard of a session is the tentaqles plugin's PreToolUse hook (the plugin's hooks/hooks.json
// runs tq_hook.sh pre-tool-use). A Claude config dir without it starts a session with no guard
// at all, so tq refuses to start one. The check reads the same plugin folder the
// tq reads (PluginRoot), so the two cannot disagree about which plugin is loaded.

// GuardMarketplace and GuardPlugin are the constant names of the install (`claude plugin
// marketplace add tentaqles/tentaqles`, `claude plugin install tentaqles@tentaqles`). GuardPlugin
// is also the enabledPlugins key of the guard: a plugin of the same name from another marketplace
// is not the guard.
const (
	GuardMarketplace = "tentaqles/tentaqles"
	GuardPlugin      = "tentaqles@tentaqles"
)

// GuardHook is the answer to whether a session starts with the guard.
type GuardHook struct {
	// OK is true when the guard runs before every tool call of a session.
	OK bool
	// Why says what is missing, in one line; "" when OK.
	Why string
	// Setting is the switch in a settings file that turns the guard off: "disableAllHooks", or
	// "enabledPlugins" for the tentaqles entry set off in a project's settings. "" otherwise.
	Setting string
	// File is the settings file that holds Setting.
	File string
	// NoConfig is set when the client has no Claude config at all (no claude identity).
	NoConfig bool
	// Missing is set when the tentaqles plugin is not installed for the client at all.
	Missing bool
	// Disabled is set when the plugin is installed but not enabled in the identity's settings.json.
	Disabled bool
	// Unresolved is set when the folder the session starts in cannot be resolved, so the project
	// settings it loads cannot be read.
	Unresolved bool
	// Reinstall is set when the plugin is installed and enabled but its hooks file is not the guard
	// (unreadable, or no PreToolUse hook that runs tq_hook.sh): only a fresh install replaces it.
	Reinstall bool
}

// CheckGuardHook reads the Claude config dir claudeDir (an identity's own) and says whether a
// session that loads only it runs tq's guard: settings.json does not set disableAllHooks, the
// tentaqles plugin is installed and enabled (enabledPlugins), and the plugin's hooks/hooks.json
// has a PreToolUse command hook that runs tq_hook.sh pre-tool-use, for Bash and for MCP tools.
// Anything it cannot read counts as missing: the check fails closed. It reads a few small files
// and runs no process. CheckSessionGuard adds the project's settings.
func CheckGuardHook(claudeDir string) GuardHook {
	if claudeDir == "" {
		// A client with no claude identity has no Claude config for a session to run with, so no guard.
		return GuardHook{Why: "this client has no Claude config (no claude identity in its manifest)", NoConfig: true}
	}
	settings := filepath.Join(claudeDir, "settings.json")
	doc := map[string]any{}
	raw, err := os.ReadFile(settings)
	switch {
	case err == nil:
		if json.Unmarshal(raw, &doc) != nil {
			return GuardHook{Why: fmt.Sprintf("%s can't be read as JSON, so the tentaqles plugin's state is unknown", settings),
				File: settings}
		}
	case !os.IsNotExist(err):
		return GuardHook{Why: fmt.Sprintf("%s can't be read: %v", settings, err), File: settings}
	}
	if v, ok := doc["disableAllHooks"]; ok && v != false {
		return GuardHook{Why: fmt.Sprintf("disableAllHooks is set in %s, which turns off every hook, the guard's too", settings),
			Setting: "disableAllHooks", File: settings}
	}
	root := PluginRoot(claudeDir)
	if root == "" {
		return GuardHook{Why: "the tentaqles plugin is not installed for this client", Missing: true}
	}
	plugins, _ := doc["enabledPlugins"].(map[string]any)
	if on, _ := plugins[GuardPlugin].(bool); !on {
		return GuardHook{Why: fmt.Sprintf("the tentaqles plugin is not enabled (enabledPlugins in %s)", settings), Disabled: true}
	}
	hooks := filepath.Join(root, "hooks", "hooks.json")
	raw, err = os.ReadFile(hooks)
	if err != nil {
		return GuardHook{Why: fmt.Sprintf("the tentaqles plugin's hooks file can't be read: %v", err), Reinstall: true}
	}
	if !preToolUseRunsGuard(raw) {
		return GuardHook{Why: "the tentaqles plugin has no PreToolUse hook that runs tq's guard (tq_hook.sh pre-tool-use for Bash and PowerShell)", Reinstall: true}
	}
	return GuardHook{OK: true}
}

// projectSettingsFiles are the settings files of one project folder that a session loads on top of
// the identity's: .claude/settings.json and .claude/settings.local.json.
var projectSettingsFiles = []string{filepath.Join(".claude", "settings.json"), filepath.Join(".claude", "settings.local.json")}

// CheckSessionGuard is CheckGuardHook for a session that runs in cwd inside the client folder root:
// Claude Code merges the project settings of each folder from cwd up to root into the identity's,
// so a disableAllHooks or a tentaqles entry set off there turns the guard off for that session
// (REQ-8). A project file that cannot be read as JSON counts as off, and is named. A cwd that cannot
// be resolved is refused: its project settings cannot be read.
func CheckSessionGuard(claudeDir, cwd, root string) GuardHook {
	g := CheckGuardHook(claudeDir)
	if !g.OK {
		return g
	}
	dirs, err := SessionDirs(cwd, root)
	if err != nil {
		return GuardHook{Why: "the folder the session starts in can't be resolved, so its project settings can't be checked: " + err.Error(), Unresolved: true}
	}
	for _, d := range dirs {
		for _, rel := range projectSettingsFiles {
			if p := checkProjectSettings(filepath.Join(d, rel)); !p.OK {
				return p
			}
		}
	}
	return GuardHook{OK: true}
}

// checkProjectSettings reads one project settings file: a missing file is fine; one that cannot be
// read, or sets disableAllHooks or the tentaqles entry of enabledPlugins to anything but the
// switch on, is not.
func checkProjectSettings(file string) GuardHook {
	raw, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return GuardHook{OK: true}
		}
		return GuardHook{Why: fmt.Sprintf("%s can't be read: %v", file, err), File: file}
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return GuardHook{Why: fmt.Sprintf("%s can't be read as JSON, so the guard's state in this project is unknown", file), File: file}
	}
	if v, ok := doc["disableAllHooks"]; ok && v != false {
		return GuardHook{Why: fmt.Sprintf("disableAllHooks is set in %s, which turns off every hook, the guard's too", file),
			Setting: "disableAllHooks", File: file}
	}
	if raw, ok := doc["enabledPlugins"]; ok {
		plugins, isMap := raw.(map[string]any)
		if !isMap {
			return GuardHook{Why: fmt.Sprintf("enabledPlugins in %s is not an object, so the guard's state is unknown", file), File: file}
		}
		if v, ok := plugins[GuardPlugin]; ok && v != true {
			return GuardHook{Why: fmt.Sprintf("the tentaqles plugin is turned off in %s (enabledPlugins)", file),
				Setting: "enabledPlugins", File: file}
		}
	}
	return GuardHook{OK: true}
}

// SessionDirs are the folders whose project settings a session started in cwd loads: cwd, and each
// parent up to the client folder root when cwd is inside it. Both are resolved through their links
// (junctions included) before they are compared, and the folders returned are the resolved ones, so
// a cwd reached through a link still reaches the root's settings. A cwd outside root is checked
// alone; root "" gives cwd alone. It is an error when cwd, or root, cannot be resolved: a session
// whose project settings cannot be read is refused, not passed.
func SessionDirs(cwd, root string) ([]string, error) {
	c, err := resolveFolder(cwd)
	if err != nil {
		return nil, fmt.Errorf("the working folder %q: %w", cwd, err)
	}
	if root == "" {
		return []string{c}, nil
	}
	r, err := resolveFolder(root)
	if err != nil {
		return nil, fmt.Errorf("the client folder %q: %w", root, err)
	}
	if rel, err := filepath.Rel(r, c); err != nil || isOutside(rel) {
		return []string{c}, nil
	}
	dirs := []string{c}
	for cur := c; ; {
		if rel, err := filepath.Rel(r, cur); err != nil || rel == "." {
			return dirs, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return dirs, nil
		}
		cur = parent
		dirs = append(dirs, cur)
	}
}

// resolveFolder is the absolute path of an existing folder with every link on the way resolved. An
// error when the folder is empty, does not exist, or cannot be resolved. It walks the path itself:
// filepath.EvalSymlinks does not follow a junction on Windows (Go 1.23 on), and a junction is the
// link that needs no privilege, so a walk that stopped at one would miss the client's own folder.
func resolveFolder(p string) (string, error) {
	if p == "" {
		return "", errors.New("it is not known")
	}
	a, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return resolveLinks(a, 0)
}

// maxLinks bounds the links resolveLinks follows, so a loop of links cannot stall the hook.
const maxLinks = 40

// lstatPath is os.Lstat; a test replaces it to present a reparse point of another kind (a cloud
// placeholder folder cannot be made in CI).
var lstatPath = os.Lstat

// isLink reports a symlink, or a reparse point that is not a folder (a junction or a mount point).
// On Windows a folder with another reparse tag, such as a OneDrive placeholder, reports as a folder
// and a reparse point (ModeDir|ModeIrregular): it is a real folder, and it is walked as one, not
// handed to os.Readlink, which fails for any tag but symlink and mount point.
func isLink(fi os.FileInfo) bool {
	m := fi.Mode()
	return m&os.ModeSymlink != 0 || (m&os.ModeIrregular != 0 && !fi.IsDir())
}

// resolveLinks walks the absolute path p one element at a time. Each element must exist; a symlink
// or junction is replaced by its target (resolved in turn), and anything else must be a folder.
func resolveLinks(p string, depth int) (string, error) {
	if depth > maxLinks {
		return "", fmt.Errorf("%s: too many links", p)
	}
	vol := filepath.VolumeName(p)
	cur := vol + string(filepath.Separator)
	rest := strings.Trim(p[len(vol):], string(filepath.Separator))
	if rest == "" {
		return cur, nil
	}
	for _, part := range strings.Split(rest, string(filepath.Separator)) {
		next := filepath.Join(cur, part)
		fi, err := lstatPath(next)
		if err != nil {
			return "", err
		}
		if isLink(fi) {
			tgt, err := os.Readlink(next)
			if err != nil {
				return "", err
			}
			tgt = strings.TrimPrefix(tgt, `\??\`)
			if !filepath.IsAbs(tgt) {
				tgt = filepath.Join(cur, tgt)
			}
			if cur, err = resolveLinks(tgt, depth+1); err != nil {
				return "", err
			}
			continue
		}
		if !fi.IsDir() {
			return "", fmt.Errorf("%s is not a folder", next)
		}
		cur = next
	}
	return filepath.Clean(cur), nil
}

// preToolUseRunsGuard reports whether a hooks.json document has a PreToolUse entry whose matcher
// covers Bash and an MCP tool and whose command hook runs tq_hook.sh pre-tool-use.
func preToolUseRunsGuard(raw []byte) bool {
	var doc struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return false
	}
	for _, e := range doc.Hooks.PreToolUse {
		if !matcherCovers(e.Matcher, "Bash") || !matcherCovers(e.Matcher, "PowerShell") {
			continue
		}
		for _, h := range e.Hooks {
			if h.Type == "command" && runsGuardCommand(h.Command) {
				return true
			}
		}
	}
	return false
}

// matcherCovers reports whether a PreToolUse matcher selects tool. An empty matcher or "*" selects
// every tool; any other is a regular expression, which must compile.
func matcherCovers(matcher, tool string) bool {
	m := strings.TrimSpace(matcher)
	if m == "" || m == "*" {
		return true
	}
	re, err := regexp.Compile(m)
	return err == nil && re.MatchString(tool)
}

// runsGuardCommand reports whether command is the plugin's guard call: bash, then a path ending in
// tq_hook.sh, then pre-tool-use, with the quotes around the path read away.
func runsGuardCommand(command string) bool {
	fields := strings.Fields(strings.NewReplacer(`"`, " ", "'", " ").Replace(command))
	if len(fields) != 3 || (fields[0] != "bash" && fields[0] != "sh") {
		return false
	}
	return path.Base(strings.ReplaceAll(fields[1], `\`, "/")) == "tq_hook.sh" && fields[2] == "pre-tool-use"
}

// GuardHookFix is the sequence of lines that gets a developer a guarded session for workspace ws,
// each one copyable and run in order: the plugin install when it is missing (with its marketplace
// and the record's re-sign), the enable when it is off, a fresh install (uninstall, re-sign, install)
// when its hooks file is not the guard, or, for a settings file, the switch to clear or the file to
// repair, and what follows the edit (a re-sign for the identity's settings, which the record covers;
// a new session for a project's). The plugin install line is exact (REQ-5).
func GuardHookFix(ws string, g GuardHook) []string {
	switch {
	case g.NoConfig:
		return []string{fmt.Sprintf("add claude to the identities in %s's manifest, then: tq settings render %s", ws, ws)}
	case g.Unresolved:
		return []string{"run it from a folder whose path resolves, then: tq run " + ws + " -- claude"}
	case g.Setting == "disableAllHooks":
		next := "tq run " + ws + " -- claude"
		return []string{fmt.Sprintf("remove %q (or set it to false) in %s, then: %s", g.Setting, g.File, next)}
	case g.Setting == "enabledPlugins":
		return []string{fmt.Sprintf("set %q to true under enabledPlugins in %s, then: tq run %s -- claude", GuardPlugin, g.File, ws)}
	case g.File != "" && g.Setting == "":
		// A file that cannot be read or is not the JSON it must be: repairing it is the fix, and an
		// install cannot repair it.
		next := "tq run " + ws + " -- claude"
		return []string{fmt.Sprintf("repair %s (it can't be read as the JSON it must be), then: %s", g.File, next)}
	case g.Disabled:
		return []string{"tq run " + ws + " -- claude plugin enable " + GuardPlugin}
	case g.Missing:
		return GuardInstallLines(ws)
	case g.Reinstall:
		// The plugin's hooks file is not the guard, so an install over the installed plugin may not
		// replace it: uninstall first, then install afresh.
		return append([]string{"tq run " + ws + " -- claude plugin uninstall " + GuardPlugin},
			GuardInstallLines(ws)...)
	}
	return GuardInstallLines(ws)
}

// GuardInstallLines are the lines that install the tentaqles plugin for workspace ws, in the order
// to run them: the marketplace (which may be there already, and then the line says so), then the
// install.
func GuardInstallLines(ws string) []string {
	return []string{
		"tq run " + ws + " -- claude plugin marketplace add " + GuardMarketplace,
		GuardPluginLine(ws),
	}
}

// GuardPluginLine is the command that installs the tentaqles plugin for workspace ws, run through
// tq so it uses the client's own Claude config.
func GuardPluginLine(ws string) string {
	return "tq run " + ws + " -- claude plugin install " + GuardPlugin
}

// isOutside reports a filepath.Rel result that leaves its base.
func isOutside(rel string) bool {
	return filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
