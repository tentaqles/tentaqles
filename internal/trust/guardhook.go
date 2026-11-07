package trust

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// The guard of a session is the tentaqles plugin's PreToolUse hook (the plugin's hooks/hooks.json
// runs tq_hook.sh pre-tool-use). A Claude config dir without it starts a session with no guard
// at all, so tq refuses to start one (ADR-0018). The check reads the same plugin folder the
// integrity record covers (PluginRoot), so the two cannot disagree about which plugin is loaded.

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
	// Covered is true when File is the identity's settings.json, which the integrity record covers.
	Covered bool
	// NoConfig is set when the client has no Claude config at all (no claude identity).
	NoConfig bool
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
			return GuardHook{Why: fmt.Sprintf("%s can't be read as JSON, so the tentaqles plugin's state is unknown", settings)}
		}
	case !os.IsNotExist(err):
		return GuardHook{Why: fmt.Sprintf("%s can't be read: %v", settings, err)}
	}
	if v, ok := doc["disableAllHooks"]; ok && v != false {
		return GuardHook{Why: fmt.Sprintf("disableAllHooks is set in %s, which turns off every hook, the guard's too", settings),
			Setting: "disableAllHooks", File: settings, Covered: true}
	}
	root := PluginRoot(claudeDir)
	if root == "" {
		return GuardHook{Why: "the tentaqles plugin is not installed for this client"}
	}
	plugins, _ := doc["enabledPlugins"].(map[string]any)
	if on, _ := plugins[GuardPlugin].(bool); !on {
		return GuardHook{Why: fmt.Sprintf("the tentaqles plugin is not enabled (enabledPlugins in %s)", settings)}
	}
	hooks := filepath.Join(root, "hooks", "hooks.json")
	raw, err = os.ReadFile(hooks)
	if err != nil {
		return GuardHook{Why: fmt.Sprintf("the tentaqles plugin's hooks file can't be read: %v", err)}
	}
	if !preToolUseRunsGuard(raw) {
		return GuardHook{Why: "the tentaqles plugin has no PreToolUse hook that runs tq's guard (tq_hook.sh pre-tool-use for Bash and MCP tools)"}
	}
	return GuardHook{OK: true}
}

// projectSettingsFiles are the settings files of one project folder that a session loads on top of
// the identity's: .claude/settings.json and .claude/settings.local.json.
var projectSettingsFiles = []string{filepath.Join(".claude", "settings.json"), filepath.Join(".claude", "settings.local.json")}

// CheckSessionGuard is CheckGuardHook for a session that runs in cwd inside the client folder root:
// Claude Code merges the project settings of each folder from cwd up to root into the identity's,
// so a disableAllHooks or a tentaqles entry set off there turns the guard off for that session
// (REQ-8). A project file that cannot be read as JSON counts as off, and is named.
func CheckSessionGuard(claudeDir string, dirs []string) GuardHook {
	g := CheckGuardHook(claudeDir)
	if !g.OK {
		return g
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
// parent up to the client folder root when cwd is inside it. A cwd outside root is checked alone.
// root "" (no folder) gives cwd alone.
func SessionDirs(cwd, root string) []string {
	dirs := []string{cwd}
	if root == "" {
		return dirs
	}
	if rel, err := filepath.Rel(root, cwd); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return dirs
	}
	for cur := cwd; ; {
		if rel, err := filepath.Rel(root, cur); err != nil || rel == "." {
			return dirs
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return dirs
		}
		cur = parent
		dirs = append(dirs, cur)
	}
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
		if !matcherCovers(e.Matcher, "Bash") || !matcherCovers(e.Matcher, "mcp__tentaqles__probe") {
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

// GuardHookFix is the line that fixes a missing guard for workspace ws: the plugin install, or, when
// a settings file switches the guard off, the switch to clear and what follows the edit (a re-sign
// for the identity's settings, which the record covers; a new session for a project's).
func GuardHookFix(ws string, g GuardHook) string {
	switch {
	case g.NoConfig:
		return fmt.Sprintf("add claude to the identities in %s's manifest, then: tq allow %s", ws, ws)
	case g.Setting == "disableAllHooks":
		next := "tq run " + ws + " -- claude"
		if g.Covered {
			next = "tq hooks verify --accept " + ws
		}
		return fmt.Sprintf("remove %q (or set it to false) in %s, then: %s", g.Setting, g.File, next)
	case g.Setting == "enabledPlugins":
		return fmt.Sprintf("set %q to true under enabledPlugins in %s, then: tq run %s -- claude", GuardPlugin, g.File, ws)
	}
	return GuardPluginLine(ws)
}

// GuardPluginLine is the command that installs the tentaqles plugin for workspace ws, run through
// tq so it uses the client's own Claude config.
func GuardPluginLine(ws string) string {
	return "tq run " + ws + " -- claude plugin install " + GuardPlugin
}
