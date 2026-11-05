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
		{"WindowsTerminal.exe", "", true, KindAnchor},
		{"Terminal", "/System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal", true, KindAnchor},
		{"gnome-terminal-server", "/usr/libexec/gnome-terminal-server", true, KindAnchor},
		{"sshd-session", "", true, KindAnchor},
		{"tmux: server", "tmux", true, KindAnchor},
		{"Code.exe", `"C:\Users\dev\AppData\Local\Programs\Microsoft VS Code\Code.exe"`, true, KindAnchor},
		{"bash", "-bash", true, KindAnchor},
		{"zsh", "/bin/zsh -l", true, KindAnchor},
		{"zsh", "zsh --login", true, KindAnchor},
		{"-zsh", "-zsh", true, KindAnchor},
		{"bash", "bash", true, KindNeutral},
		{"systemd", "/usr/lib/systemd/systemd --user", true, KindNeutral},
		{"go.exe", "go test ./...", true, KindNeutral},
	}
	for _, c := range cases {
		if got := Classify(c.name, c.cmdline, c.known); got != c.want {
			t.Errorf("Classify(%q, %q, %v) = %v, want %v", c.name, c.cmdline, c.known, got, c.want)
		}
	}
}

func TestWalkPassesAHumanTerminal(t *testing.T) {
	v, p, why := Walk(100, fakeTree(humanChain()))
	if v != Human {
		t.Fatalf("a human terminal chain must pass: %v %+v %s", v, p, why)
	}
	// macOS: tq <- -zsh (login shell) <- Terminal <- launchd (1) <- root.
	mac := map[int]fakeProc{
		100: {90, "tq", "tq allow acme", false, false},
		90:  {80, "zsh", "-zsh", false, false},
		80:  {1, "Terminal", "/System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal", false, false},
		1:   {0, "launchd", "", false, false},
	}
	if v, p, why := Walk(100, fakeTree(mac)); v != Human {
		t.Fatalf("Terminal.app chain must pass: %v %+v %s", v, p, why)
	}
	// Linux desktop: tq <- bash <- gnome-terminal-server <- systemd --user <- systemd (1).
	linux := map[int]fakeProc{
		100: {90, "tq", "tq allow acme", false, false},
		90:  {80, "bash", "bash", false, false},
		80:  {70, "gnome-terminal-server", "/usr/libexec/gnome-terminal-server", false, false},
		70:  {1, "systemd", "/usr/lib/systemd/systemd --user", false, false},
		1:   {0, "systemd", "", false, false},
	}
	if v, p, why := Walk(100, fakeTree(linux)); v != Human {
		t.Fatalf("GNOME chain must pass: %v %+v %s", v, p, why)
	}
	// ssh: tq <- -bash <- sshd-session (root: known, empty) <- sshd <- systemd (1).
	ssh := map[int]fakeProc{
		100: {90, "tq", "tq allow acme", false, false},
		90:  {80, "bash", "-bash", false, false},
		80:  {70, "sshd-session", "", false, false},
		70:  {1, "sshd", "", false, false},
		1:   {0, "systemd", "", false, false},
	}
	if v, p, why := Walk(100, fakeTree(ssh)); v != Human {
		t.Fatalf("ssh chain must pass: %v %+v %s", v, p, why)
	}
}

func TestWalkFindsAHarnessParent(t *testing.T) {
	// tq (100) <- bash (90) <- claude.exe (80) <- WindowsTerminal.exe (70)
	tree := fakeTree(map[int]fakeProc{
		100: {90, "tq.exe", "tq allow acme", false, false},
		90:  {80, "bash.exe", `bash -c "tq allow acme"`, false, false},
		80:  {70, "claude.exe", "claude --dangerously-skip-permissions", false, false},
		70:  {60, "WindowsTerminal.exe", "", false, false},
		60:  {0, "explorer.exe", "", false, false},
	})
	v, p, _ := Walk(100, tree)
	if v != Harness || p.PID != 80 || p.Name != "claude.exe" {
		t.Fatalf("want claude.exe at 80, got %v %+v", v, p)
	}
	// A harness above the terminal it opened is still found.
	above := fakeTree(map[int]fakeProc{
		100: {90, "tq.exe", "", false, false},
		90:  {80, "pwsh.exe", "pwsh", false, false},
		80:  {70, "WindowsTerminal.exe", "wt", false, false},
		70:  {0, "claude.exe", "claude", false, false},
	})
	if v, p, _ := Walk(100, above); v != Harness || p.PID != 70 {
		t.Fatalf("a harness above the terminal is found: %v %+v", v, p)
	}
}

func TestWalkMatchesInterpretersByCommandLine(t *testing.T) {
	// npm-installed Claude Code: node with cli.js on its command line.
	harness := fakeTree(map[int]fakeProc{
		100: {90, "tq", "", false, false},
		90:  {80, "sh", "sh -c tq", false, false},
		80:  {70, "node", "node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js", false, false},
		70:  {1, "zsh", "-zsh", false, false},
		1:   {0, "launchd", "", false, false},
	})
	if v, p, _ := Walk(100, harness); v != Harness || p.PID != 80 {
		t.Fatalf("node running claude must be found, got %v %+v", v, p)
	}
	// A subagent through a shell: bash -c "codex exec ...".
	shell := fakeTree(map[int]fakeProc{
		100: {90, "tq", "", false, false},
		90:  {80, "bash", `bash -c "codex exec 'tq allow acme'"`, false, false},
		80:  {1, "zsh", "-zsh", false, false},
		1:   {0, "launchd", "", false, false},
	})
	if v, p, _ := Walk(100, shell); v != Harness || p.PID != 90 {
		t.Fatalf("bash running codex must be found, got %v %+v", v, p)
	}
	// The same node running a web server, under a terminal, is a human's.
	server := fakeTree(map[int]fakeProc{
		100: {90, "tq", "", false, false},
		90:  {80, "sh", "sh -c tq", false, false},
		80:  {70, "node", "node server.js --port 3000", false, false},
		70:  {1, "zsh", "-zsh", false, false},
		1:   {0, "launchd", "", false, false},
	})
	if v, p, why := Walk(100, server); v != Human {
		t.Fatalf("node running a server under a login shell must pass, got %v %+v %s", v, p, why)
	}
}

// An orphaned chain refuses: a parent that exited before any terminal or
// login shell was seen cannot prove a human ancestry (the child of a dead
// harness looks exactly like this).
func TestWalkRefusesAnOrphanedChain(t *testing.T) {
	orphan := fakeTree(map[int]fakeProc{
		100: {90, "tq", "", false, false},
		90:  {12345, "bash", "bash", false, false}, // bash's parent (a harness) is gone
	})
	v, p, why := Walk(100, orphan)
	if v != Unverified || p.PID != 90 || !strings.Contains(why, "exited or cannot be read") {
		t.Fatalf("orphan: %v %+v %q", v, p, why)
	}
	// Unix: re-parented to pid 1 with no anchor below it.
	reparented := fakeTree(map[int]fakeProc{
		100: {90, "tq", "", false, false},
		90:  {80, "bash", "bash", false, false},
		80:  {1, "node", "node server.js", false, false},
		1:   {0, "launchd", "", false, false},
	})
	if v, _, why := Walk(100, reparented); v != Unverified || !strings.Contains(why, "no terminal or login shell") {
		t.Fatalf("re-parented to pid 1: %v %q", v, why)
	}
	// A service or cron: the root reached through system processes only.
	service := fakeTree(map[int]fakeProc{
		100: {90, "tq.exe", "", false, false},
		90:  {80, "pwsh.exe", "pwsh -File task.ps1", false, false},
		80:  {70, "svchost.exe", "", false, false},
		70:  {0, "services.exe", "", false, false},
	})
	if v, _, _ := Walk(100, service); v != Unverified {
		t.Fatalf("a scheduled task is not a human: %v", v)
	}
	// A stubbed walker never proves anything.
	if v, _, _ := Walk(100, func(int) (Parent, bool) { return Parent{}, false }); v != Unverified {
		t.Fatalf("a stubbed walker must refuse: %v", v)
	}
}

func TestWalkRefusesAReusedPid(t *testing.T) {
	tree := humanChain()
	p := tree[80]
	p.reused = true // WindowsTerminal.exe's pid was handed to a process created after pwsh
	tree[80] = p
	v, proc, why := Walk(100, fakeTree(tree))
	if v != Unverified || proc.PID != 80 || !strings.Contains(why, "reused") {
		t.Fatalf("pid reuse: %v %+v %q", v, proc, why)
	}
}

func TestWalkRefusesAnUnreadableCommandLine(t *testing.T) {
	tree := humanChain()
	p := tree[90]
	p.unknown = true // PROCESS_VM_READ refused on a same-session pwsh
	tree[90] = p
	v, proc, why := Walk(100, fakeTree(tree))
	if v != Unverified || proc.PID != 90 || !strings.Contains(why, "command line") {
		t.Fatalf("unreadable command line: %v %+v %q", v, proc, why)
	}
	// Another session's process (a service) is reported known and empty, and passes.
	fromStart := map[int]fakeProc{
		100: {90, "tq.exe", "tq", false, false},
		90:  {80, "pwsh.exe", "pwsh", false, false},
		80:  {70, "WindowsTerminal.exe", "wt", false, false},
		70:  {60, "svchost.exe", "", false, false},
		60:  {50, "services.exe", "", false, false},
		50:  {4242, "wininit.exe", "", false, false},
	}
	if v, proc, why := Walk(100, fakeTree(fromStart)); v != Human {
		t.Fatalf("a terminal started from the Start menu passes: %v %+v %q", v, proc, why)
	}
}

func TestWalkKeepsTheCycleGuard(t *testing.T) {
	cycle := fakeTree(map[int]fakeProc{
		100: {90, "tq", "", false, false},
		90:  {100, "sh", "sh", false, false},
	})
	v, _, why := Walk(100, cycle)
	if v != Unverified || !strings.Contains(why, "loops") {
		t.Fatalf("a cycle ends the walk as unverified: %v %q", v, why)
	}
	// Even a cycle above an anchor: the chain is broken, not proven.
	above := humanChain()
	above[70] = fakeProc{80, "explorer.exe", "explorer", false, false}
	if v, _, _ := Walk(100, fakeTree(above)); v != Unverified {
		t.Fatalf("a loop above the anchor is still a broken chain: %v", v)
	}
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
