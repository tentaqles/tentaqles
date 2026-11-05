package humanonly

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Parent is one step of the process tree: the parent of a pid, as the per-OS
// walker reads it.
type Parent struct {
	PID     int
	Name    string // executable name, with or without a directory and .exe
	Cmdline string // the parent's command line; "" when the OS will not say
	// CmdlineKnown is false when the command line could not be read for a
	// process that could be this user's (same session or uid). A process of
	// another user or session is reported known with an empty command line:
	// it is never this user's harness.
	CmdlineKnown bool
	// Reused is set on Windows when the parent was created after its child:
	// the child's real parent exited and the pid now belongs to someone else.
	Reused bool
}

// ParentFunc reads one step of the process tree. ok is false when pid has no
// readable parent: the root, a parent that exited, or an OS without a walker.
// The per-OS implementations live in proctree_*.go; tests feed a fake tree.
type ParentFunc func(pid int) (Parent, bool)

// Process is one process found in the parent chain.
type Process struct {
	PID     int
	Name    string
	Cmdline string
}

// Verdict is the outcome of a walk.
type Verdict int

const (
	// Human: the chain passes through an anchor, a process only a human
	// session starts (a terminal emulator, a login shell, the desktop shell,
	// an interactive ssh session, a session manager, an editor's integrated
	// terminal), and no harness.
	Human Verdict = iota
	// Harness: a harness or subagent is in the chain, by name or by the
	// command line of an interpreter.
	Harness
	// Unverified: the chain cannot be followed to an anchor: a parent that
	// exited, a reused pid, a loop or an unreadable command line before any
	// anchor, a chain that reaches the root with no anchor, or an OS without
	// a walker. A command refuses on it: an orphaned child of a dead harness
	// looks exactly like this.
	Unverified
)

// Kind classifies one process of the chain.
type Kind int

const (
	KindNeutral Kind = iota
	KindHarness
	KindUnknown
	KindAnchor
)

// maxDepth bounds the walk: no real chain is this deep.
const maxDepth = 64

// harnessNames are the executables (without directory or .exe) that are a
// harness or a subagent wherever they appear in the chain.
var harnessNames = set("claude", "codex", "opencode", "kimi")

// interpreters are the shells and runtimes a harness runs through (an
// npm-installed Claude Code is node with cli.js on its command line; a
// subagent may be `bash -c "claude -p ..."`). Their command line decides,
// and an unreadable or empty one is unknown. A shell is an anchor only as a
// login shell, never by name.
var interpreters = set("bun", "deno", "npx", "node", "cmd", "pwsh", "powershell", "sh", "bash", "zsh", "fish", "dash")

// cmdlineNeedles are the substrings of an interpreter's command line that
// name a harness: the npm packages, their scripts and the CLIs.
var cmdlineNeedles = []string{"claude-code", "@anthropic-ai/claude-code", "codex.js", "@openai/codex", "opencode", "kimi-code"}

// bareTokens are the command words that name a harness when they stand on
// their own in an interpreter's command line.
var bareTokens = set("claude", "codex")

// anchorNames are the processes only a human session starts: terminal
// emulators, multiplexers, the desktop shell and session managers,
// interactive ssh, mosh and console logins, and editors with an integrated
// terminal. An editor that hosts agents (Cursor, Windsurf) is not one. A
// chain that reaches one of them (and no harness) is a human's; the
// developer adds an unlisted terminal with `tq settings anchors add`.
var anchorNames = set(
	// Windows: terminals, the desktop shell, the shell's helpers, WSL
	"explorer", "windowsterminal", "wt", "openconsole", "conhost", "mintty",
	"conemu", "conemu64", "conemuc", "conemuc64",
	"startmenuexperiencehost", "sihost", "wslhost",
	// macOS and Linux terminals
	"terminal", "iterm2", "warp", "ghostty", "kitty", "alacritty", "wezterm", "wezterm-gui", "foot",
	"gnome-terminal", "gnome-terminal-server", "kgx", "konsole", "xfce4-terminal", "mate-terminal",
	"lxterminal", "qterminal", "sakura", "guake", "yakuake", "tilda", "blackbox", "contour",
	"terminator", "tilix", "urxvt", "rxvt", "xterm", "st", "hyper", "tabby", "ptyxis", "cosmic-term",
	// session managers
	"gnome-session-binary", "plasma_session", "ksmserver", "loginwindow",
	// multiplexers, remote and console logins
	"tmux", "tmux: server", "screen", "zellij", "sshd", "sshd-session", "mosh-server",
	"login", "getty", "agetty",
	// editors with an integrated terminal
	"code", "code-insiders", "codium", "zed", "fleet",
	"idea", "idea64", "pycharm", "pycharm64", "goland", "goland64", "webstorm", "webstorm64",
	"phpstorm", "phpstorm64", "rubymine", "rubymine64", "datagrip", "datagrip64",
	"rustrover", "rustrover64", "rider", "rider64", "clion", "clion64", "studio", "studio64",
)

// ideJava matches the command line of a JetBrains IDE running as java.
var ideJava = regexp.MustCompile(`idea|phpstorm|pycharm|webstorm|goland|rubymine|datagrip|clion|rider|rustrover|studio`)

// userAnchors are the names the developer added with `tq settings anchors
// add` (anchors.yaml). Detect loads them before each walk; tests set them.
var userAnchors = map[string]bool{}

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// BuiltinAnchors lists the built-in anchor names, sorted.
func BuiltinAnchors() []string {
	out := make([]string, 0, len(anchorNames))
	for n := range anchorNames {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// baseName lowers name, drops its directory and a .exe suffix.
func baseName(name string) string {
	n := strings.ToLower(filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/")))
	return strings.TrimSuffix(n, ".exe")
}

// argv0 is the first word of a command line, unquoted.
func argv0(cmdline string) string {
	f := strings.FieldsFunc(cmdline, func(r rune) bool { return r == ' ' || r == '\t' || r == '"' || r == '\'' })
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// loginShell reports a login shell: argv0 starts with "-" (-bash, -zsh) or
// the arguments carry -l / --login.
func loginShell(name, cmdline string) bool {
	if strings.HasPrefix(strings.TrimSpace(name), "-") || strings.HasPrefix(argv0(cmdline), "-") {
		return true
	}
	for _, f := range strings.Fields(cmdline)[min(1, len(strings.Fields(cmdline))):] {
		if f == "-l" || f == "--login" {
			return true
		}
	}
	return false
}

// isAnchor reports a session root: a listed or developer-added name, a login
// shell, the user's systemd instance, WSL's init processes, or a JetBrains
// IDE running as java.
func isAnchor(n, name, cmdline string) bool {
	if anchorNames[n] || userAnchors[n] || loginShell(name, cmdline) {
		return true
	}
	switch n {
	case "systemd":
		return strings.Contains(cmdline, "--user")
	case "init":
		return argv0(cmdline) == "/init" || strings.Contains(cmdline, "SessionLeader") || strings.Contains(cmdline, "Relay(")
	case "java":
		return ideJava.MatchString(strings.ToLower(cmdline))
	}
	return false
}

// mentionsHarness reports whether an interpreter's command line names a
// harness: a needle substring, or a bare claude / codex word.
func mentionsHarness(cmdline string) bool {
	c := strings.ToLower(cmdline)
	for _, n := range cmdlineNeedles {
		if strings.Contains(c, n) {
			return true
		}
	}
	// Words are split on whitespace, quotes, path separators and shell
	// joiners; an assignment such as CLAUDE=1 stays one word and is no
	// command.
	words := strings.FieldsFunc(c, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '"' || r == '\'' || r == '/' || r == '\\' || r == ';' || r == '&' || r == '|'
	})
	for _, w := range words {
		for _, ext := range []string{".exe", ".cmd", ".ps1"} {
			w = strings.TrimSuffix(w, ext)
		}
		if bareTokens[w] {
			return true
		}
	}
	return false
}

// Classify says what one process of the chain is. known is false when its
// command line could not be read: such a process is unknown unless its name
// alone makes it an anchor (a terminal is identified by its name).
func Classify(name, cmdline string, known bool) Kind {
	n := strings.TrimPrefix(baseName(name), "-")
	if harnessNames[n] {
		return KindHarness
	}
	if interpreters[n] {
		if !known || strings.TrimSpace(cmdline) == "" {
			return KindUnknown
		}
		if mentionsHarness(cmdline) {
			return KindHarness
		}
		if loginShell(name, cmdline) {
			return KindAnchor
		}
		return KindNeutral
	}
	if !known {
		if anchorNames[n] || userAnchors[n] {
			return KindAnchor
		}
		return KindUnknown
	}
	if isAnchor(n, name, cmdline) {
		return KindAnchor
	}
	return KindNeutral
}

// IsHarness reports whether a process refuses a human-only command on its
// own: a harness by name, an interpreter whose command line names one, or an
// interpreter whose command line is empty (unknown).
func IsHarness(name, cmdline string) bool {
	k := Classify(name, cmdline, true)
	return k == KindHarness || k == KindUnknown
}

// Walk follows parentOf from pid up to the root. It returns Harness with the
// process found; Human once the chain has passed through an anchor, whatever
// ends it above (the root, a parent that exited, a reused pid, a loop: the
// desktop shell's own parent is a dead pid on every Windows box); and
// Unverified with the reason otherwise. The reason names the processes seen
// and the top-most one that is this user's, with the `tq settings anchors
// add` line that would accept it. The process at pid itself is not examined.
func Walk(pid int, parentOf ParentFunc) (Verdict, Process, string) {
	seen := map[int]bool{pid: true}
	anchored := false
	var chain []Process
	var top Process
	cur := pid
	unverified := func(p Process, why string) (Verdict, Process, string) {
		return Unverified, p, why + anchorHint(chain, top)
	}
	for i := 0; i < maxDepth; i++ {
		p, ok := parentOf(cur)
		if !ok {
			if anchored {
				return Human, Process{}, ""
			}
			return unverified(Process{PID: cur}, fmt.Sprintf("the parent of pid %d exited or cannot be read, and no terminal or login shell was seen below it", cur))
		}
		proc := Process{PID: p.PID, Name: p.Name, Cmdline: p.Cmdline}
		if p.Reused || seen[p.PID] {
			// A dead or reused link above an anchor is the normal end of a
			// chain; below one it is an orphan.
			if anchored {
				return Human, Process{}, ""
			}
			if p.Reused {
				return unverified(proc, fmt.Sprintf("pid %d was reused: %s started after its child", p.PID, p.Name))
			}
			return unverified(proc, fmt.Sprintf("the process tree loops at pid %d", p.PID))
		}
		seen[p.PID] = true
		chain = append(chain, proc)
		if p.CmdlineKnown && p.Cmdline != "" {
			top = proc // readable: this user's own process
		}
		switch Classify(p.Name, p.Cmdline, p.CmdlineKnown) {
		case KindHarness:
			return Harness, proc, ""
		case KindUnknown:
			// Above an anchor an unreadable link, like a dead one, ends a
			// human's chain; below one it hides what started the shell.
			if anchored {
				return Human, Process{}, ""
			}
			top = proc
			return unverified(proc, fmt.Sprintf("the command line of %s (pid %d) cannot be read", p.Name, p.PID))
		case KindAnchor:
			anchored = true
		}
		cur = p.PID
	}
	return unverified(Process{}, fmt.Sprintf("more than %d ancestors", maxDepth))
}

// anchorHint names the processes a failed walk saw and the exact command
// that accepts the top-most one as a terminal.
func anchorHint(chain []Process, top Process) string {
	if len(chain) == 0 {
		return ""
	}
	names := make([]string, 0, len(chain))
	for _, p := range chain {
		names = append(names, fmt.Sprintf("%s (pid %d)", p.Name, p.PID))
	}
	s := "; processes seen: " + strings.Join(names, " <- ")
	if top.Name != "" {
		s += fmt.Sprintf("; if %s is your terminal, allow it once with: tq settings anchors add %s", top.Name, NormalizeAnchor(top.Name))
	}
	return s
}
