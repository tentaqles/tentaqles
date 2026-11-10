package sense

import "testing"

// TestLobeHiddenPipes is the ten cases of feature 009: hidesExit on eight commands, firstProgram on two.
func TestLobeHiddenPipes(t *testing.T) {
	hides := []struct {
		command string
		want    bool
	}{
		{"npm test | tail -5", true},
		{"make 2>&1 | tee build.log", true},
		{"pytest || true", true},
		{"go test ./... | grep -v ok", true},
		{"npm test", false},
		{`echo "a | tail"`, false},
		{"cat <<EOF | tail -1", false},
		{"npm test | tail -5 && echo done", false},
	}
	for _, tc := range hides {
		if got := hidesExit(tc.command); got != tc.want {
			t.Errorf("hidesExit(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
	programs := []struct {
		command, program, next string
	}{
		{"cat deploy.log | tail -3", "cat", "deploy.log"},
		{"git log | head", "git", "log"},
	}
	for _, tc := range programs {
		if p, n := firstProgram(tc.command); p != tc.program || n != tc.next {
			t.Errorf("firstProgram(%q) = (%q, %q), want (%q, %q)", tc.command, p, n, tc.program, tc.next)
		}
	}
}

// TestLobeHiddenReadsOnly checks the read-only list that rule C skips.
func TestLobeHiddenReadsOnly(t *testing.T) {
	cases := []struct {
		program, next string
		want          bool
	}{
		{"cat", "x", true},
		{"jq", "", true},
		{"git", "log", true},
		{"git", "push", false},
		{"npm", "test", false},
	}
	for _, tc := range cases {
		if got := readsOnly(tc.program, tc.next); got != tc.want {
			t.Errorf("readsOnly(%q, %q) = %v, want %v", tc.program, tc.next, got, tc.want)
		}
	}
}

// TestLobeHiddenReadOnlyRuleB: a command whose first program only reads is not judged by its output,
// by rule B or by rule C (owner, 2026-11-10), and a leading cd is skipped to find that program.
func TestLobeHiddenReadOnlyRuleB(t *testing.T) {
	cases := []struct {
		name, tool, command, stdout string
		want                        bool
	}{
		{"git log title with a count", "Bash", "git log --oneline", "abc123 fix 2 failing tests\n", false},
		{"cat of an old log", "Bash", "cat old.log", "Tests: 1 failed\n", false},
		{"cd then a reader", "Bash", "cd app && cat x.log", "3 failed\n", false},
		{"piped test run still judged", "Bash", "npm test 2>&1 | tail -5", "FAIL src/a.test.ts\nTests: 1 failed, 3 passed\n", true},
	}
	for _, tc := range cases {
		_, got := Hidden(tc.tool, commandInput(tc.command), shellResponse(tc.stdout, ""))
		if got != tc.want {
			t.Errorf("%s: Hidden(%q) = %v, want %v", tc.name, tc.command, got, tc.want)
		}
	}
	if p, n := firstProgram("cd app && cat x.log"); p != "cat" || n != "x.log" {
		t.Errorf("firstProgram past a cd = (%q, %q), want (cat, x.log)", p, n)
	}
	if p, n := firstProgram("cd app"); p != "" || n != "" {
		t.Errorf("firstProgram of a lone cd = (%q, %q), want empty", p, n)
	}
}
