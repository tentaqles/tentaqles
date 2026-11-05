package humanonly

import (
	"strings"
	"testing"
)

// fakeProc is one process of a fake tree: its parent and what the walker
// would read about it. known defaults to true through fakeTree.
type fakeProc struct {
	ppid    int
	name    string
	cmdline string
	unknown bool // command line unreadable
	reused  bool // created after its child (Windows pid reuse)
}

// fakeTree feeds Walk through the ParentFunc seam: no real process is read.
// A ppid that is absent from the map, or 0, is a parent that exited.
func fakeTree(procs map[int]fakeProc) ParentFunc {
	return func(pid int) (Parent, bool) {
		p, ok := procs[pid]
		if !ok || p.ppid <= 0 {
			return Parent{}, false
		}
		parent, ok := procs[p.ppid]
		if !ok {
			return Parent{}, false
		}
		return Parent{PID: p.ppid, Name: parent.name, Cmdline: parent.cmdline, CmdlineKnown: !parent.unknown, Reused: parent.reused}, true
	}
}

// humanChain is a Windows developer's chain: tq (100) <- pwsh <- WindowsTerminal.exe <- explorer.exe <- (exited).
func humanChain() map[int]fakeProc {
	return map[int]fakeProc{
		100: {90, "tq.exe", `tq allow acme`, false, false},
		90:  {80, "pwsh.exe", `"C:\Program Files\PowerShell\7\pwsh.exe" -NoLogo`, false, false},
		80:  {70, "WindowsTerminal.exe", `"C:\Program Files\WindowsApps\WindowsTerminal.exe"`, false, false},
		70:  {7777, "explorer.exe", `C:\WINDOWS\Explorer.EXE`, false, false},
	}
}

// walk runs Walk from pid 100 over procs and fails the test unless the
// verdict is want.
func walk(t *testing.T, label string, procs map[int]fakeProc, want Verdict) (Process, string) {
	t.Helper()
	v, p, why := Walk(100, fakeTree(procs))
	if v != want {
		t.Fatalf("%s: verdict %v, want %v (%+v %s)", label, v, want, p, why)
	}
	return p, why
}

func TestIsHarness(t *testing.T) {
	cases := []struct {
		name, cmdline string
		want          bool
	}{
		{"claude", "", true},
		{"claude.exe", "", true},
		{"CLAUDE.EXE", "", true},
		{`C:\Users\dev\AppData\Local\Programs\claude\claude.exe`, "", true},
		{"/usr/local/bin/codex", "", true},
		{"codex.exe", "", true},
		{"opencode", "", true},
		{"kimi", "", true},
		// interpreters: the command line decides
		{"node", `node C:\Users\dev\AppData\Roaming\npm\node_modules\@anthropic-ai\claude-code\cli.js`, true},
		{"node.exe", `"C:\Program Files\nodejs\node.exe" /opt/codex/bin/codex.js`, true},
		{"node", "node server.js", false},
		{"node.exe", "", true}, // unknown: an interpreter with no readable command line refuses
		{"bun", "bun run /home/dev/.bun/install/global/node_modules/@anthropic-ai/claude-code/cli.js", true},
		{"bun", "bun run dev", false},
		{"bun", "", true},
		{"deno", "deno run -A npm:@openai/codex", true},
		{"deno", "deno run main.ts", false},
		{"npx", "npx @anthropic-ai/claude-code", true},
		{"npx", "npx opencode", true},
		{"npx", "npx kimi-code", true},
		{"npx", "npx prettier --check .", false},
		{"bash", `bash -c "claude -p 'summarise'"`, true},
		{"bash", `bash -c "codex exec ls"`, true},
		{"sh", "sh -c 'CLAUDE=1 ls'", false},
		{"bash", "/usr/bin/bash", false},
		{"cmd.exe", `cmd.exe /c claude.cmd --version`, true},
		{"cmd.exe", `cmd.exe /c dir`, false},
		{"pwsh.exe", `pwsh -Command "& codex --help"`, true},
		{"pwsh.exe", `pwsh -NoLogo`, false},
		{"powershell.exe", `powershell -File C:\scripts\run-claude-code.ps1`, true},
		{"zsh", `zsh -c "claude"`, true},
		{"fish", `fish -c "ls"`, false},
		// non-interpreters are not matched on their command line
		{"WindowsTerminal.exe", `wt.exe -p claude`, false},
		{"pwsh.exe", "pwsh.exe", false},
		{"bash", "-bash", false},
		{"claudex", "claudex", false},
		{"my-claude-wrapper", "my-claude-wrapper", false},
		{"", "claude", false},
	}
	for _, c := range cases {
		if got := IsHarness(c.name, c.cmdline); got != c.want {
			t.Errorf("IsHarness(%q, %q) = %v, want %v", c.name, c.cmdline, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name, cmdline string
		known         bool
		want          Kind
	}{
		{"claude.exe", "", true, KindHarness},
		{"node", "node cli.js", false, KindUnknown},
		{"svchost.exe", "", false, KindUnknown},
		{"svchost.exe", "", true, KindNeutral}, // another session's process: known, empty
		{"explorer.exe", "C:\\WINDOWS\\Explorer.EXE", true, KindAnchor},
		{"explorer.exe", "", false, KindAnchor}, // an anchor is known by its name alone
		{"WindowsTerminal.exe", "", true, KindAnchor},
		{"ConEmuC64.exe", "", true, KindAnchor},
		{"sihost.exe", "", true, KindAnchor},
		{"StartMenuExperienceHost.exe", "", true, KindAnchor},
		{"wslhost.exe", "", true, KindAnchor},
		{"Terminal", "/System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal", true, KindAnchor},
		{"loginwindow", "/System/Library/CoreServices/loginwindow.app/Contents/MacOS/loginwindow console", true, KindAnchor},
		{"gnome-terminal-server", "/usr/libexec/gnome-terminal-server", true, KindAnchor},
		{"kgx", "/usr/bin/kgx", true, KindAnchor},
		{"gnome-session-binary", "/usr/libexec/gnome-session-binary --session=gnome", true, KindAnchor},
		{"plasma_session", "/usr/bin/plasma_session", true, KindAnchor},
		{"sshd-session", "", true, KindAnchor},
		{"mosh-server", "mosh-server new -s -c 256 -l LANG=en_US.UTF-8", true, KindAnchor},
		{"tmux: server", "tmux", true, KindAnchor},
		{"agetty", "/sbin/agetty -o -p -- \\u --noclear tty1 linux", true, KindAnchor},
		{"Code.exe", `"C:\Users\dev\AppData\Local\Programs\Microsoft VS Code\Code.exe"`, true, KindAnchor},
		{"fleet", "/opt/fleet/bin/fleet", true, KindAnchor},
		{"rustrover64.exe", `C:\Program Files\JetBrains\RustRover\bin\rustrover64.exe`, true, KindAnchor},
		{"Cursor.exe", `"C:\Users\dev\AppData\Local\Programs\cursor\Cursor.exe"`, true, KindNeutral}, // hosts agents
		{"windsurf", "/opt/windsurf/windsurf", true, KindNeutral},
		// java is an anchor only as a JetBrains IDE
		{"java", "/opt/idea/jbr/bin/java -Didea.paths.selector=IntelliJIdea2025.1 com.intellij.idea.Main", true, KindAnchor},
		{"java", "/opt/pycharm/jbr/bin/java -Didea.platform.prefix=PyCharm", true, KindAnchor},
		{"java", "java -jar app.jar", true, KindNeutral},
		// systemd is an anchor only as the user instance; init only as WSL's
		{"systemd", "/usr/lib/systemd/systemd --user", true, KindAnchor},
		{"systemd", "/sbin/init splash", true, KindNeutral},
		{"init", "/init", true, KindAnchor},
		{"init", "SessionLeader", true, KindAnchor},
		{"init", "Relay(64)", true, KindAnchor},
		{"init", "/sbin/init", true, KindNeutral},
		// login shells
		{"bash", "-bash", true, KindAnchor},
		{"zsh", "/bin/zsh -l", true, KindAnchor},
		{"zsh", "zsh --login", true, KindAnchor},
		{"-zsh", "-zsh", true, KindAnchor},
		{"bash", "bash", true, KindNeutral},
		{"go.exe", "go test ./...", true, KindNeutral},
	}
	for _, c := range cases {
		if got := Classify(c.name, c.cmdline, c.known); got != c.want {
			t.Errorf("Classify(%q, %q, %v) = %v, want %v", c.name, c.cmdline, c.known, got, c.want)
		}
	}
}

// The chains the critic walked on real boxes. Each one is a human's.
func TestWalkPassesRealTerminals(t *testing.T) {
	walk(t, "Windows Terminal + pwsh", humanChain(), Human)
	// Git Bash profile in Windows Terminal: bash under OpenConsole.
	walk(t, "Git Bash under WT", map[int]fakeProc{
		100: {90, "tq.exe", "tq allow acme", false, false},
		90:  {80, "bash.exe", `C:\Program Files\Git\usr\bin\bash.exe --login -i`, false, false},
		80:  {70, "OpenConsole.exe", `OpenConsole.exe --headless`, false, false},
		70:  {60, "WindowsTerminal.exe", "wt", false, false},
		60:  {4242, "explorer.exe", "explorer", false, false},
	}, Human)
	// Git Bash app: bash under mintty, started by git-bash.exe from explorer.
	walk(t, "Git Bash under mintty", map[int]fakeProc{
		100: {90, "tq.exe", "tq allow acme", false, false},
		90:  {80, "bash.exe", `/usr/bin/bash --login -i`, false, false},
		80:  {70, "mintty.exe", `/usr/bin/mintty --nodaemon -o AppID=GitForWindows.Bash`, false, false},
		70:  {60, "git-bash.exe", `"C:\Program Files\Git\git-bash.exe"`, false, false},
		60:  {4242, "explorer.exe", "explorer", false, false},
	}, Human)
	// ConEmu: the console server under the GUI.
	walk(t, "ConEmu", map[int]fakeProc{
		100: {90, "tq.exe", "tq allow acme", false, false},
		90:  {80, "pwsh.exe", "pwsh", false, false},
		80:  {70, "ConEmuC64.exe", `ConEmuC64.exe /ATTACH`, false, false},
		70:  {60, "ConEmu64.exe", `"C:\tools\ConEmu\ConEmu64.exe"`, false, false},
		60:  {4242, "explorer.exe", "explorer", false, false},
	}, Human)
	// Windows Terminal started from the Start menu: session-0 services above it.
	walk(t, "WT from the Start menu", map[int]fakeProc{
		100: {90, "tq.exe", "tq", false, false},
		90:  {80, "pwsh.exe", "pwsh", false, false},
		80:  {70, "WindowsTerminal.exe", "wt", false, false},
		70:  {60, "svchost.exe", "", false, false},
		60:  {50, "services.exe", "", false, false},
		50:  {4242, "wininit.exe", "", false, false},
	}, Human)
	// VS Code's integrated terminal on Windows.
	walk(t, "VS Code on Windows", map[int]fakeProc{
		100: {90, "tq.exe", "tq", false, false},
		90:  {80, "powershell.exe", "powershell -NoLogo", false, false},
		80:  {70, "Code.exe", `Code.exe --type=utility`, false, false},
		70:  {60, "Code.exe", "Code.exe", false, false},
		60:  {4242, "explorer.exe", "explorer", false, false},
	}, Human)
	// macOS: tq <- -zsh (login shell) <- Terminal <- launchd (1).
	walk(t, "Terminal.app", map[int]fakeProc{
		100: {90, "tq", "tq allow acme", false, false},
		90:  {80, "zsh", "-zsh", false, false},
		80:  {1, "Terminal", "/System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal", false, false},
		1:   {0, "launchd", "", false, false},
	}, Human)
	// Linux desktop: tq <- bash <- gnome-terminal-server <- systemd --user <- systemd (1).
	walk(t, "GNOME", map[int]fakeProc{
		100: {90, "tq", "tq allow acme", false, false},
		90:  {80, "bash", "bash", false, false},
		80:  {70, "gnome-terminal-server", "/usr/libexec/gnome-terminal-server", false, false},
		70:  {1, "systemd", "/usr/lib/systemd/systemd --user", false, false},
		1:   {0, "systemd", "", false, false},
	}, Human)
	// JetBrains on Linux: the IDE runs as java.
	walk(t, "JetBrains as java", map[int]fakeProc{
		100: {90, "tq", "tq", false, false},
		90:  {80, "bash", "/bin/bash --rcfile /opt/goland/plugins/terminal/shell-integrations/bash/bash-integration.bash -i", false, false},
		80:  {70, "java", "/opt/goland/jbr/bin/java -Didea.paths.selector=GoLand2025.1 com.intellij.idea.Main", false, false},
		70:  {1, "systemd", "/usr/lib/systemd/systemd --user", false, false},
		1:   {0, "systemd", "", false, false},
	}, Human)
	// ssh: tq <- -bash <- sshd-session (root: known, empty) <- sshd <- systemd (1).
	walk(t, "ssh", map[int]fakeProc{
		100: {90, "tq", "tq allow acme", false, false},
		90:  {80, "bash", "-bash", false, false},
		80:  {70, "sshd-session", "", false, false},
		70:  {1, "sshd", "", false, false},
		1:   {0, "systemd", "", false, false},
	}, Human)
	// VS Code Remote-SSH: the server runs as node under sshd.
	walk(t, "VS Code Remote-SSH", map[int]fakeProc{
		100: {90, "tq", "tq", false, false},
		90:  {80, "bash", "/usr/bin/bash --init-file /home/dev/.vscode-server/bin/abc/out/vs/workbench/contrib/terminal/common/scripts/shellIntegration-bash.sh", false, false},
		80:  {70, "node", "/home/dev/.vscode-server/bin/abc/node /home/dev/.vscode-server/bin/abc/out/server-main.js --host=127.0.0.1", false, false},
		70:  {60, "sh", "sh /home/dev/.vscode-server/bin/abc/bin/code-server --start-server", false, false},
		60:  {50, "sshd-session", "", false, false},
		50:  {1, "sshd", "", false, false},
		1:   {0, "systemd", "", false, false},
	}, Human)
	// tmux: the server is the session root.
	walk(t, "tmux", map[int]fakeProc{
		100: {90, "tq", "tq", false, false},
		90:  {80, "bash", "-bash", false, false},
		80:  {1, "tmux: server", "tmux", false, false},
		1:   {0, "systemd", "", false, false},
	}, Human)
	// A console login: login under agetty.
	walk(t, "getty", map[int]fakeProc{
		100: {90, "tq", "tq", false, false},
		90:  {80, "bash", "-bash", false, false},
		80:  {70, "login", "login -- dev", false, false},
		70:  {1, "agetty", "/sbin/agetty -o -p -- \\u --noclear tty1 linux", false, false},
		1:   {0, "systemd", "", false, false},
	}, Human)
	// WSL login shell: the distro's init processes are the session root.
	walk(t, "WSL login shell", map[int]fakeProc{
		100: {90, "tq", "tq", false, false},
		90:  {80, "bash", "-bash", false, false},
		80:  {70, "init", "SessionLeader", false, false},
		70:  {1, "init", "Relay(64)", false, false},
		1:   {0, "init", "/init", false, false},
	}, Human)
	// WSL `wsl -e bash`: the same init processes, a plain bash. It passes:
	// from inside the distro the Windows-side parent (a terminal, or a
	// harness running `wsl -e`) is invisible, so the verdict is the session
	// root's. A harness driving WSL this way is the documented limit (ADR-0003).
	walk(t, "wsl -e bash", map[int]fakeProc{
		100: {90, "tq", "tq", false, false},
		90:  {80, "bash", "bash", false, false},
		80:  {70, "init", "SessionLeader", false, false},
		70:  {1, "init", "Relay(72)", false, false},
		1:   {0, "init", "/init", false, false},
	}, Human)
}

func TestWalkFindsAHarnessParent(t *testing.T) {
	// tq (100) <- bash (90) <- claude.exe (80) <- WindowsTerminal.exe (70)
	p, _ := walk(t, "claude.exe", map[int]fakeProc{
		100: {90, "tq.exe", "tq allow acme", false, false},
		90:  {80, "bash.exe", `bash -c "tq allow acme"`, false, false},
		80:  {70, "claude.exe", "claude --dangerously-skip-permissions", false, false},
		70:  {60, "WindowsTerminal.exe", "", false, false},
		60:  {0, "explorer.exe", "", false, false},
	}, Harness)
	if p.PID != 80 || p.Name != "claude.exe" {
		t.Fatalf("want claude.exe at 80, got %+v", p)
	}
	// A harness above the terminal it opened is still found.
	if p, _ := walk(t, "harness above the terminal", map[int]fakeProc{
		100: {90, "tq.exe", "", false, false},
		90:  {80, "pwsh.exe", "pwsh", false, false},
		80:  {70, "WindowsTerminal.exe", "wt", false, false},
		70:  {0, "claude.exe", "claude", false, false},
	}, Harness); p.PID != 70 {
		t.Fatalf("a harness above the terminal is found: %+v", p)
	}
}

func TestWalkMatchesInterpretersByCommandLine(t *testing.T) {
	// npm-installed Claude Code: node with cli.js on its command line.
	if p, _ := walk(t, "node claude", map[int]fakeProc{
		100: {90, "tq", "", false, false},
		90:  {80, "sh", "sh -c tq", false, false},
		80:  {70, "node", "node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js", false, false},
		70:  {1, "zsh", "-zsh", false, false},
		1:   {0, "launchd", "", false, false},
	}, Harness); p.PID != 80 {
		t.Fatalf("node running claude must be found, got %+v", p)
	}
	// A subagent through a shell: bash -c "codex exec ...".
	if p, _ := walk(t, "bash codex", map[int]fakeProc{
		100: {90, "tq", "", false, false},
		90:  {80, "bash", `bash -c "codex exec 'tq allow acme'"`, false, false},
		80:  {1, "zsh", "-zsh", false, false},
		1:   {0, "launchd", "", false, false},
	}, Harness); p.PID != 90 {
		t.Fatalf("bash running codex must be found, got %+v", p)
	}
	// The same node running a web server, under a terminal, is a human's.
	walk(t, "node server", map[int]fakeProc{
		100: {90, "tq", "", false, false},
		90:  {80, "sh", "sh -c tq", false, false},
		80:  {70, "node", "node server.js --port 3000", false, false},
		70:  {1, "zsh", "-zsh", false, false},
		1:   {0, "launchd", "", false, false},
	}, Human)
}

// An orphaned chain refuses: a parent that exited before any terminal or
// login shell was seen cannot prove a human ancestry (the child of a dead
// harness looks exactly like this).
func TestWalkRefusesAnOrphanedChain(t *testing.T) {
	p, why := walk(t, "orphan", map[int]fakeProc{
		100: {90, "tq", "", false, false},
		90:  {12345, "bash", "bash", false, false}, // bash's parent (a harness) is gone
	}, Unverified)
	if p.PID != 90 || !strings.Contains(why, "exited or cannot be read") {
		t.Fatalf("orphan: %+v %q", p, why)
	}
	// Unix: re-parented to pid 1 with no anchor below it.
	if _, why := walk(t, "re-parented", map[int]fakeProc{
		100: {90, "tq", "", false, false},
		90:  {80, "bash", "bash", false, false},
		80:  {1, "node", "node server.js", false, false},
		1:   {0, "launchd", "", false, false},
	}, Unverified); !strings.Contains(why, "no terminal or login shell") {
		t.Fatalf("re-parented to pid 1: %q", why)
	}
	// A service or cron: the root reached through system processes only.
	walk(t, "scheduled task", map[int]fakeProc{
		100: {90, "tq.exe", "", false, false},
		90:  {80, "pwsh.exe", "pwsh -File task.ps1", false, false},
		80:  {70, "svchost.exe", "", false, false},
		70:  {0, "services.exe", "", false, false},
	}, Unverified)
	// A stubbed walker never proves anything.
	if v, _, _ := Walk(100, func(int) (Parent, bool) { return Parent{}, false }); v != Unverified {
		t.Fatalf("a stubbed walker must refuse: %v", v)
	}
}

// A dead, reused or looping link above an anchor ends the chain as a human's:
// explorer.exe's and wininit.exe's parents are dead pids on every Windows box.
func TestWalkAcceptsDeadLinksAboveAnAnchor(t *testing.T) {
	// explorer.exe's parent pid reused by a process younger than explorer.
	reused := humanChain()
	reused[7777] = fakeProc{4242, "RuntimeBroker.exe", "RuntimeBroker.exe -Embedding", false, true}
	walk(t, "pid reused above explorer", reused, Human)
	// The reused pid is a harness name: still above the anchor, still not
	// the real parent, still a human's chain.
	reused[7777] = fakeProc{4242, "claude.exe", "claude", false, true}
	walk(t, "reused pid wearing a harness name", reused, Human)
	// A loop above the anchor.
	loop := humanChain()
	loop[7777] = fakeProc{80, "wininit.exe", "", false, false}
	walk(t, "loop above the anchor", loop, Human)
	// An unreadable command line above the anchor is not consulted.
	unread := humanChain()
	unread[7777] = fakeProc{4242, "userinit.exe", "", true, false}
	walk(t, "unreadable above the anchor", unread, Human)
}

func TestWalkRefusesAReusedPidBelowAnAnchor(t *testing.T) {
	tree := humanChain()
	p := tree[80]
	p.reused = true // WindowsTerminal.exe's pid was handed to a process created after pwsh
	tree[80] = p
	proc, why := walk(t, "reused below the anchor", tree, Unverified)
	if proc.PID != 80 || !strings.Contains(why, "reused") {
		t.Fatalf("pid reuse: %+v %q", proc, why)
	}
}

func TestWalkRefusesAnUnreadableCommandLineBelowAnAnchor(t *testing.T) {
	tree := humanChain()
	p := tree[90]
	p.unknown = true // PROCESS_VM_READ refused on a same-session pwsh
	tree[90] = p
	proc, why := walk(t, "unreadable pwsh", tree, Unverified)
	if proc.PID != 90 || !strings.Contains(why, "command line") {
		t.Fatalf("unreadable command line: %+v %q", proc, why)
	}
}

func TestWalkKeepsTheCycleGuard(t *testing.T) {
	// A loop below any anchor is a broken chain, not a proven one.
	_, why := walk(t, "loop", map[int]fakeProc{
		100: {90, "tq", "", false, false},
		90:  {100, "sh", "sh", false, false},
	}, Unverified)
	if !strings.Contains(why, "loops") {
		t.Fatalf("a cycle ends the walk as unverified: %q", why)
	}
	// The guard also bounds the walk: a loop above the anchor ends it as a
	// human's instead of running to maxDepth.
	calls := 0
	loop := humanChain()
	loop[7777] = fakeProc{80, "wininit.exe", "", false, false}
	f := fakeTree(loop)
	v, _, _ := Walk(100, func(pid int) (Parent, bool) { calls++; return f(pid) })
	if v != Human || calls > 6 {
		t.Fatalf("loop above the anchor: %v after %d reads", v, calls)
	}
}

// A refusal names what the walk saw and the exact command that would accept
// the top-most of this user's processes as a terminal.
func TestWalkRefusalNamesTheTopProcessAndTheAnchorsCommand(t *testing.T) {
	_, why := walk(t, "unlisted terminal", map[int]fakeProc{
		100: {90, "tq.exe", "tq", false, false},
		90:  {80, "pwsh.exe", "pwsh", false, false},
		80:  {70, "Foo-Term.exe", `"C:\Program Files\FooTerm\Foo-Term.exe"`, false, false},
		70:  {60, "svchost.exe", "", false, false},
		60:  {4242, "services.exe", "", false, false},
	}, Unverified)
	for _, want := range []string{
		"processes seen: pwsh.exe (pid 90) <- Foo-Term.exe (pid 80) <- svchost.exe (pid 70) <- services.exe (pid 60)",
		"if Foo-Term.exe is your terminal, allow it once with: tq settings anchors add foo-term",
	} {
		if !strings.Contains(why, want) {
			t.Fatalf("want %q in %q", want, why)
		}
	}
	// Once added, the same chain is a human's.
	prev := userAnchors
	userAnchors = set("foo-term")
	t.Cleanup(func() { userAnchors = prev })
	walk(t, "unlisted terminal, now an anchor", map[int]fakeProc{
		100: {90, "tq.exe", "tq", false, false},
		90:  {80, "pwsh.exe", "pwsh", false, false},
		80:  {70, "Foo-Term.exe", `"C:\Program Files\FooTerm\Foo-Term.exe"`, false, false},
		70:  {60, "svchost.exe", "", false, false},
		60:  {4242, "services.exe", "", false, false},
	}, Human)
}

// The real walker reads this test's own parent chain. On an OS with a walker
// it must find the go test runner that started this process, with a readable
// command line; on one without, it must report no parent.
func TestRealParentOfReadsThisProcess(t *testing.T) {
	p, ok := parentOf(selfPID())
	if processTreeMethod == "" {
		if ok {
			t.Fatalf("a stubbed walker must report no parent, got %+v", p)
		}
		return
	}
	if !ok || p.PID <= 0 || p.Name == "" || !p.CmdlineKnown || p.Reused {
		t.Fatalf("parentOf(self) = %+v ok=%v", p, ok)
	}
}
