package humanonly

import "testing"

// fakeTree is a parent chain keyed by pid: each entry names a process and its
// parent. It feeds Walk through the ParentFunc seam, so no real process is read.
type fakeProc struct {
	ppid    int
	name    string
	cmdline string
}

func fakeTree(procs map[int]fakeProc) ParentFunc {
	return func(pid int) (int, string, string, bool) {
		p, ok := procs[pid]
		if !ok || p.ppid <= 0 {
			return 0, "", "", false
		}
		parent, ok := procs[p.ppid]
		if !ok {
			return 0, "", "", false
		}
		return p.ppid, parent.name, parent.cmdline, true
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
		{"node", `node C:\Users\dev\AppData\Roaming\npm\node_modules\@anthropic-ai\claude-code\cli.js`, true},
		{"node.exe", `"C:\Program Files\nodejs\node.exe" /opt/codex/bin/codex.js`, true},
		{"node", "node server.js", false},
		{"node.exe", "", false},
		{"pwsh.exe", "", false},
		{"WindowsTerminal.exe", "", false},
		{"bash", "", false},
		{"claudex", "", false},
		{"my-claude-wrapper", "", false},
		{"", "claude", false},
	}
	for _, c := range cases {
		if got := IsHarness(c.name, c.cmdline); got != c.want {
			t.Errorf("IsHarness(%q, %q) = %v, want %v", c.name, c.cmdline, got, c.want)
		}
	}
}

func TestWalkFindsAHarnessParent(t *testing.T) {
	// tq (100) <- bash (90) <- claude.exe (80) <- WindowsTerminal.exe (70)
	tree := fakeTree(map[int]fakeProc{
		100: {90, "tq.exe", ""},
		90:  {80, "bash.exe", ""},
		80:  {70, "claude.exe", ""},
		70:  {1, "WindowsTerminal.exe", ""},
		1:   {0, "wininit.exe", ""},
	})
	p, found := Walk(100, tree)
	if !found || p.PID != 80 || p.Name != "claude.exe" {
		t.Fatalf("want claude.exe at 80, got %+v found=%v", p, found)
	}
}

func TestWalkPassesAHumanTerminal(t *testing.T) {
	// tq (100) <- pwsh (90) <- WindowsTerminal.exe (80) <- explorer (70)
	tree := fakeTree(map[int]fakeProc{
		100: {90, "tq.exe", ""},
		90:  {80, "pwsh.exe", ""},
		80:  {70, "WindowsTerminal.exe", ""},
		70:  {1, "explorer.exe", ""},
		1:   {0, "wininit.exe", ""},
	})
	if p, found := Walk(100, tree); found {
		t.Fatalf("a human terminal chain must pass, got %+v", p)
	}
}

func TestWalkMatchesNodeByCommandLine(t *testing.T) {
	// A Claude Code installed through npm runs as node with cli.js on its
	// command line; the same node running a web server is not a harness.
	harness := fakeTree(map[int]fakeProc{
		100: {90, "tq", ""},
		90:  {80, "sh", ""},
		80:  {70, "node", "node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js"},
		70:  {1, "zsh", ""},
		1:   {0, "launchd", ""},
	})
	if p, found := Walk(100, harness); !found || p.PID != 80 {
		t.Fatalf("node running claude must be found, got %+v found=%v", p, found)
	}
	server := fakeTree(map[int]fakeProc{
		100: {90, "tq", ""},
		90:  {80, "sh", ""},
		80:  {70, "node", "node server.js --port 3000"},
		70:  {1, "zsh", ""},
		1:   {0, "launchd", ""},
	})
	if p, found := Walk(100, server); found {
		t.Fatalf("node running a server must pass, got %+v", p)
	}
}

func TestWalkStopsOnCyclesAndUnknownParents(t *testing.T) {
	cycle := fakeTree(map[int]fakeProc{
		100: {90, "tq", ""},
		90:  {100, "sh", ""},
	})
	if _, found := Walk(100, cycle); found {
		t.Fatal("a cycle must end the walk without a match")
	}
	orphan := fakeTree(map[int]fakeProc{100: {12345, "tq", ""}})
	if _, found := Walk(100, orphan); found {
		t.Fatal("an unreadable parent ends the walk without a match")
	}
	if _, found := Walk(100, func(int) (int, string, string, bool) { return 0, "", "", false }); found {
		t.Fatal("a stubbed walker never matches")
	}
}

// The real walker reads this test's own parent chain. It must not fail, and
// it must at least find the go test runner that started this process; a
// harness above it (go test under Claude Code) is what tq is built to see.
func TestRealParentOfReadsThisProcess(t *testing.T) {
	ppid, name, _, ok := parentOf(selfPID())
	if method := processTreeMethod; method == "" {
		if ok {
			t.Fatalf("a stubbed walker must report no parent, got %d %q", ppid, name)
		}
		return
	}
	if !ok || ppid <= 0 || name == "" {
		t.Fatalf("parentOf(self) = %d %q ok=%v", ppid, name, ok)
	}
}
