package cmd

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runFingerprint runs tentaqles lobe fingerprint with args and stdin text, and returns stdout and the error.
func runFingerprint(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"lobe", "fingerprint"}, args...))
	err := root.Execute()
	return out.String(), err
}

// snapshotTree lists every entry under dir with its mode and size. A missing dir is an empty list.
func snapshotTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, fmt.Sprintf("%s %s %d", p, info.Mode(), info.Size()))
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}

// TestLobeFingerprintVerb: the verb prints two lines for each vector of the feature's evaluation, the
// secret is scrubbed out of the template, it works with an empty TQ_HOME in a neutral folder and inside a
// Claude Code session without writing anything, and without --tool it refuses with exit 1.
func TestLobeFingerprintVerb(t *testing.T) {
	isolateHome(t)
	vectors := []struct {
		name, tool, in, fp, template string
	}{
		{"bash path and line", "Bash", "Exit code 127\n/usr/bin/bash: line 1: Get-ChildItem: command not found",
			"fp_b4f3e92d90787db6", "<path>: line <n>: Get-ChildItem: command not found"},
		{"bash path and line, other path", "Bash", "/bin/bash: line 12: Get-ChildItem: command not found",
			"fp_b4f3e92d90787db6", "<path>: line <n>: Get-ChildItem: command not found"},
		{"sql syntax error", "mcp__warehouse__query", "[PARSE_SYNTAX_ERROR] Syntax error at or near '('. SQLSTATE: 42601 (line 3, pos 27)",
			"fp_f44eaf6b555b0cd2", "[PARSE_SYNTAX_ERROR] Syntax error at or near <q>. SQLSTATE: 42601 (line <n>, pos <n>)"},
		{"sql syntax error, other quote", "mcp__warehouse__query", "[PARSE_SYNTAX_ERROR] Syntax error at or near 'SELECT'. SQLSTATE: 42601 (line 1, pos 41)",
			"fp_f44eaf6b555b0cd2", "[PARSE_SYNTAX_ERROR] Syntax error at or near <q>. SQLSTATE: 42601 (line <n>, pos <n>)"},
		{"rate limit", "mcp__tracker__create_issue", `HTTP 429 Too Many Requests: {"error":{"code":"rate_limited","message":"Rate limit exceeded for key ending 4f2a. Retry after 17 seconds.","request_id":"req_8f3a9c21d0b4"}}`,
			"fp_c9f4aa346d1ec77f", "HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds."},
		{"rate limit, other key", "mcp__tracker__create_issue", `HTTP 429 Too Many Requests: {"error":{"code":"rate_limited","message":"Rate limit exceeded for key ending 91cc. Retry after 3 seconds.","request_id":"req_02b7e6a955f1"}}`,
			"fp_c9f4aa346d1ec77f", "HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds."},
		{"sql text under Bash", "Bash", "[PARSE_SYNTAX_ERROR] Syntax error at or near '('. SQLSTATE: 42601 (line 3, pos 27)",
			"fp_366015c0d74849a2", "[PARSE_SYNTAX_ERROR] Syntax error at or near <q>. SQLSTATE: 42601 (line <n>, pos <n>)"},
		{"rate limit changed to 404", "mcp__tracker__create_issue", `HTTP 404 Not Found: {"error":{"code":"rate_limited","message":"Rate limit exceeded for key ending 4f2a. Retry after 17 seconds.","request_id":"req_8f3a9c21d0b4"}}`,
			"fp_8c90d679b4fe1c9f", "HTTP 404 Not Found code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds."},
	}
	for _, v := range vectors {
		out, err := runFingerprint(t, v.in, "--tool", v.tool)
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		want := v.fp + "\n" + v.template + "\n"
		if out != want {
			t.Errorf("%s: output = %q, want %q", v.name, out, want)
		}
	}

	// A secret in the text: the template holds <secret>, and neither the secret nor a marker word.
	out, err := runFingerprint(t, "login failed: password=hunter2 rejected", "--tool", "Bash")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("output has %d lines, want 2: %q", len(lines), out)
	}
	if !strings.Contains(lines[1], "<secret>") || strings.Contains(lines[1], "hunter2") || strings.Contains(lines[1], "redacted") {
		t.Errorf("template = %q, want <secret> and no hunter2 or redacted", lines[1])
	}

	// No store, no client, no login: an empty TQ_HOME that the process does not create, a neutral folder,
	// and a Claude Code session whose login points at a folder of another machine. The verb writes nothing.
	tq := filepath.Join(t.TempDir(), "home", ".tentaqles")
	t.Setenv("TQ_HOME", tq)
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "elsewhere"))
	t.Chdir(t.TempDir())
	before := snapshotTree(t, filepath.Dir(tq))
	out, err = runFingerprint(t, "login failed: password=hunter2 rejected", "--tool", "Bash")
	if err != nil {
		t.Fatalf("the verb did not work in a neutral folder with an empty TQ_HOME: %v", err)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("output = %q, want two lines", out)
	}
	after := snapshotTree(t, filepath.Dir(tq))
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("the verb changed the home folder:\nbefore %v\nafter  %v", before, after)
	}
	if _, err := os.Stat(tq); !os.IsNotExist(err) {
		t.Errorf("the verb created %s: %v", tq, err)
	}
	unsetenv(t, "CLAUDECODE")
	unsetenv(t, "CLAUDE_CONFIG_DIR")

	// Without --tool: exit 1 through the command line, one line on stderr, nothing on stdout.
	var stdout, stderr bytes.Buffer
	if code := RunMain([]string{"lobe", "fingerprint"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if stdout.Len() != 0 || strings.Count(stderr.String(), "\n") != 1 || !strings.Contains(stderr.String(), "--tool is required") {
		t.Errorf("stdout %q, stderr %q: want no stdout and one line that says --tool is required", stdout.String(), stderr.String())
	}
}
