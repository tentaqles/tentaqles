package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/mcpscope"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// L-02: connect warns, on a line of its own, when a Claude session looks open with the
// config home; the success line stays as it is.
func TestConnect_WarnsWhenClaudeIsOpen(t *testing.T) {
	root, _ := setupScoped(t, "acme", ghBody)
	chdir(t, root)
	run := func(provider string) (stdout, stderr string) {
		t.Helper()
		var out, errb bytes.Buffer
		r := NewRoot()
		r.SetOut(&out)
		r.SetErr(&errb)
		r.SetArgs([]string{"connect", provider})
		if err := r.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String(), errb.String()
	}
	out, errOut := run("gh")
	if out != "gh: connected as github\n" || errOut != "" {
		t.Fatalf("an idle config home: stdout=%q stderr=%q", out, errOut)
	}
	sessions := filepath.Join(paths.IdentityDir("acme", "claude"), "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "s1.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut = run("linear")
	if out != "linear: connected as linear\n" {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "warning: Claude Code looks open") || !strings.Contains(errOut, "tentaqles status --json") {
		t.Fatalf("no warning with an open session: %q", errOut)
	}
}

// L-01: every copy of the MCP entries in the identity's Claude home is a protected path: the
// list render keeps of the entries it removed, its damaged-list copies, and Claude Code's
// backups of .claude.json. The harness's own working data stays readable.
func TestPreToolUse_MCPEntryCopiesProtected(t *testing.T) {
	root, _ := setupScoped(t, "company-x", companyXBody)
	claude := paths.IdentityDir("company-x", "claude")
	// The real route: render moves an out-of-scope entry, values included, into the list.
	writeClaudeJSON(t, "company-x", `{"mcpServers":{"aws-api":{"command":"uvx","args":["awslabs.aws-api-mcp-server@latest"],"env":{"X":"v"}}}}`)
	runEnv(t, "settings", "render", "company-x")
	if _, err := os.Stat(filepath.Join(claude, mcpscope.RemovedFile)); err != nil {
		t.Fatalf("render did not write the removed list: %v", err)
	}
	backup := filepath.Join(claude, "backups", ".claude.json.backup.1762424100000")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{backup, filepath.Join(claude, mcpscope.RemovedFile+".corrupt-20261106T101500Z")} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{filepath.Join(claude, mcpscope.RemovedFile), filepath.Join(claude, mcpscope.RemovedFile+".corrupt-20261106T101500Z"), backup} {
		for _, c := range []struct{ name, tool, input string }{
			{"Read", "Read", pathIn("file_path", p)},
			{"Write", "Write", `{"file_path":` + jsonPathLit(p) + `,"content":"{}"}`},
			{"cat", "Bash", cmdIn("cat " + filepath.ToSlash(p))},
		} {
			code, category, rule, errOut := hookDecision(t, root, c.tool, c.input)
			if code != 2 || category != policy.CategorySelfProtection || rule != policy.ProtectedPathID ||
				!strings.Contains(errOut, "BLOCKED [self-protection/tq/protected-path]") {
				t.Errorf("%s %s: code=%d %s/%s %q", c.name, filepath.Base(p), code, category, rule, errOut)
			}
		}
	}
	// What the harness keeps for itself stays readable and writable.
	mem := filepath.Join(claude, "projects", "C--repos-x", "memory", "MEMORY.md")
	if err := os.MkdirAll(filepath.Dir(mem), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mem, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, tool, input string }{
		{"Read memory", "Read", pathIn("file_path", mem)},
		{"Write memory", "Write", `{"file_path":` + jsonPathLit(mem) + `,"content":"y"}`},
		{"Read a todo", "Read", pathIn("file_path", filepath.Join(claude, "todos", "t.json"))},
	} {
		if code, _, rule, errOut := hookDecision(t, root, c.tool, c.input); code != 0 {
			t.Errorf("%s denied (%s): %s", c.name, rule, errOut)
		}
	}
}
