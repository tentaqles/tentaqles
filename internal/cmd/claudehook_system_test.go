package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/policy"
)

func TestShellWriteTargets(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		want []string
	}{
		{"rm -rf /abs/x", []string{"/abs/x"}},
		{"rm -r a b", []string{"a", "b"}},
		{"rm /abs/file", nil}, // only recursive deletes (spec E §5.3)
		{"rm --recursive -f /x", []string{"/x"}},
		{"echo hi > /abs/out.txt", []string{"/abs/out.txt"}},
		{"echo hi >>/abs/out.txt", []string{"/abs/out.txt"}},
		{"cmd 2>/abs/err.log", []string{"/abs/err.log"}},
		{"cmd 2>&1 | tee -a /abs/t.log", []string{"/abs/t.log"}},
		{"cmd > /dev/null 2>&1", []string{"/dev/null"}},
		{"cp a b /abs/dest/", []string{"/abs/dest/"}},
		{"mv -f a /abs/dest", []string{"/abs/dest"}},
		{"cp /abs/src ./here", []string{"./here"}},
		{"sed -i 's/a/b/' /abs/f1 /abs/f2", []string{"/abs/f1", "/abs/f2"}},
		{"sed -i -e s/a/b/ /abs/f", []string{"/abs/f"}},
		{"sudo rm -rf /abs/x && ls", []string{"/abs/x"}},
		{"X=1 tee /abs/y", []string{"/abs/y"}},
		{`echo "a > b" ; echo 'c>d'`, nil},
		{`Remove-Item -Recurse -Force C:\abs\x`, []string{`C:\abs\x`}},
		{`Remove-Item C:\abs\x`, nil},
		{`Set-Content -Path C:\abs\x.txt -Value 1`, []string{`C:\abs\x.txt`}},
		{`"x" | Out-File -FilePath C:\abs\o.txt`, []string{`C:\abs\o.txt`}},
		{`Add-Content C:\abs\a.txt "x"`, []string{`C:\abs\a.txt`}},
		{`Copy-Item a -Destination C:\abs\d`, []string{`C:\abs\d`}},
		{`rmdir /s /q C:\abs\x`, []string{`C:\abs\x`}},
		{`cat < /abs/in.txt`, nil},
		{"ls /abs", nil},
	} {
		got := shellWriteTargets(c.cmd)
		if !slices.Equal(got, c.want) {
			t.Errorf("%q: got %q, want %q", c.cmd, got, c.want)
		}
	}
}

func TestOutsidePathWrite(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "app")
	repos := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	roots := []string{root, cwd, repos}
	outside := filepath.Join(home, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	slash := filepath.ToSlash
	for _, c := range []struct {
		call policy.ToolCall
		hit  bool
	}{
		{policy.ToolCall{Tool: "Bash", Command: "rm -rf " + slash(filepath.Join(outside, "x"))}, true},
		{policy.ToolCall{Tool: "Bash", Command: "echo x > " + slash(filepath.Join(outside, "o.txt"))}, true},
		{policy.ToolCall{Tool: "Bash", Command: "rm -rf ~/outside/x"}, true},
		{policy.ToolCall{Tool: "Bash", Command: "echo x > $HOME/outside/o"}, true},
		{policy.ToolCall{Tool: "Write", Path: filepath.Join(outside, "w.txt"), Content: "x"}, true},
		{policy.ToolCall{Tool: "Edit", Path: filepath.Join(outside, "w.txt"), Content: "x"}, true},
		{policy.ToolCall{Tool: "Bash", Command: "rm -rf " + slash(filepath.Join(root, "build"))}, false},
		{policy.ToolCall{Tool: "Bash", Command: "rm -rf " + slash(filepath.Join(repos, "other-client", "x"))}, false},
		{policy.ToolCall{Tool: "Bash", Command: "go test ./... > /dev/null 2>&1"}, false},
		{policy.ToolCall{Tool: "Bash", Command: "rm -rf build"}, false}, // relative
		{policy.ToolCall{Tool: "Bash", Command: "echo x > $SOMEVAR/o"}, false},
		{policy.ToolCall{Tool: "Read", Path: filepath.Join(outside, "r.txt")}, false},
		{policy.ToolCall{Tool: "Write", Path: filepath.Join(cwd, "x.go"), Content: "x"}, false},
		{policy.ToolCall{Tool: "Write", Path: "rel/x.go", Content: "x"}, false},
		{policy.ToolCall{Tool: "PowerShell", Command: `"x" | Out-File $null`}, false},
	} {
		got := outsideWriteTargets(c.call, roots)
		if (len(got) > 0) != c.hit {
			t.Errorf("%s %q %q: got %q, want hit=%v", c.call.Tool, c.call.Path, c.call.Command, got, c.hit)
		}
	}
	tmp := writeRoots(nil, nil, cwd)
	for _, want := range []string{os.TempDir(), "/tmp", "/var/tmp", cwd} {
		if !slices.Contains(tmp, want) {
			t.Errorf("writeRoots must include %s: %q", want, tmp)
		}
	}
	if got := outsideWriteTargets(policy.ToolCall{Tool: "Bash", Command: "echo x > /tmp/t"}, tmp); len(got) != 0 {
		t.Errorf("/tmp is a temp root: %q", got)
	}
	if got := outsideWriteTargets(policy.ToolCall{Tool: "Bash", Command: "echo x > " + slash(filepath.Join(os.TempDir(), "t"))}, tmp); len(got) != 0 {
		t.Errorf("the temp dir is a root: %q", got)
	}
	if runtime.GOOS == "windows" {
		drive := strings.ToUpper(outside[:1])
		gitBash := "/" + strings.ToLower(drive) + slash(outside[2:])
		if got := outsideWriteTargets(policy.ToolCall{Tool: "Bash", Command: "rm -rf " + gitBash + "/x"}, roots); len(got) == 0 {
			t.Errorf("a Git Bash /c/ path is absolute: %q", gitBash)
		}
		inRoot := "/" + strings.ToLower(root[:1]) + slash(root[2:]) + "/build"
		if got := outsideWriteTargets(policy.ToolCall{Tool: "Bash", Command: "rm -rf " + inRoot}, roots); len(got) != 0 {
			t.Errorf("a Git Bash path inside the root: %q", got)
		}
	}
}

// Claude's auto-memory, plans and todos under the session's
// CLAUDE_CONFIG_DIR (else ~/.claude), and Codex's session folders under
// CODEX_HOME (else ~/.codex), are write roots: Claude writes them with
// Write/Edit all the time (final review M3). The config dir itself is not,
// so its settings files still get the confirm (and the locked settings and
// identity-file rules).
func TestWriteRootsIncludeAgentWorkingDirs(t *testing.T) {
	isolateHome(t)
	// Outside every temp folder: those are write roots already.
	base := string(filepath.Separator) + "tq-nonexistent"
	if runtime.GOOS == "windows" {
		base = `C:\tq-nonexistent`
	}
	cfgDir := filepath.Join(base, "identities", "acme", "claude")
	codexDir := filepath.Join(base, "identities", "acme", "codex")
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)
	t.Setenv("CODEX_HOME", codexDir)
	cwd := t.TempDir()
	roots := writeRoots(nil, nil, cwd)
	for _, p := range []string{
		filepath.Join(cfgDir, "projects", "C--repos-x", "memory", "MEMORY.md"),
		filepath.Join(cfgDir, "plans", "p.md"),
		filepath.Join(cfgDir, "todos", "t.json"),
		filepath.Join(codexDir, "sessions", "2026", "s.jsonl"),
	} {
		if got := outsideWriteTargets(policy.ToolCall{Tool: "Write", Path: p, Content: "x"}, roots); len(got) != 0 {
			t.Errorf("%s must be a write root: %v", p, got)
		}
	}
	for _, r := range roots {
		if r == cfgDir || r == codexDir {
			t.Errorf("the config dir itself is not a root (its settings files stay guarded): %s", r)
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", base)
	t.Setenv("USERPROFILE", base)
	home, _ := os.UserHomeDir()
	roots = writeRoots(nil, nil, cwd)
	if got := outsideWriteTargets(policy.ToolCall{Tool: "Edit", Path: filepath.Join(home, ".claude", "projects", "x", "memory", "a.md"), Content: "x"}, roots); len(got) != 0 {
		t.Errorf("~/.claude/projects is a write root when CLAUDE_CONFIG_DIR is unset: %v", got)
	}
}
