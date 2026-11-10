package fingerprint

import (
	"strings"
	"testing"
)

// TestLobeFingerprintVectors pins the fingerprints of S-08's tester table (rows 1 to 5, docs/prd/lobe-sense.md)
// and of the texts of docs/features/006-error-fingerprints.md, checks 1 to 5. Rows 1 to 3 each have the
// change the table names: 6 texts give 3 values. Rows 4 and 5 give their own: 8 texts give 5 values.
func TestLobeFingerprintVectors(t *testing.T) {
	const (
		row2Text = "[PARSE_SYNTAX_ERROR] Syntax error at or near '('. SQLSTATE: 42601 (line 3, pos 27)"
		row3Text = `HTTP 429 Too Many Requests: {"error":{"code":"rate_limited","message":"Rate limit exceeded for key ending 4f2a. Retry after 17 seconds.","request_id":"req_8f3a9c21d0b4"}}`
	)
	rows := []struct {
		name, tool, text, wantFP, wantTemplate string
	}{
		{
			"row 1 as written", "Bash",
			"Exit code 127\n/usr/bin/bash: line 1: Get-ChildItem: command not found",
			"fp_b4f3e92d90787db6", "<path>: line <n>: Get-ChildItem: command not found",
		},
		{
			"row 1 with the change", "Bash",
			"/bin/bash: line 12: Get-ChildItem: command not found",
			"fp_b4f3e92d90787db6", "<path>: line <n>: Get-ChildItem: command not found",
		},
		{
			"row 2 as written", "mcp__warehouse__query", row2Text,
			"fp_f44eaf6b555b0cd2", "[PARSE_SYNTAX_ERROR] Syntax error at or near <q>. SQLSTATE: 42601 (line <n>, pos <n>)",
		},
		{
			"row 2 with the change", "mcp__warehouse__query",
			"[PARSE_SYNTAX_ERROR] Syntax error at or near 'SELECT'. SQLSTATE: 42601 (line 1, pos 41)",
			"fp_f44eaf6b555b0cd2", "[PARSE_SYNTAX_ERROR] Syntax error at or near <q>. SQLSTATE: 42601 (line <n>, pos <n>)",
		},
		{
			"row 3 as written", "mcp__tracker__create_issue", row3Text,
			"fp_c9f4aa346d1ec77f", "HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds.",
		},
		{
			"row 3 with the change", "mcp__tracker__create_issue",
			`HTTP 429 Too Many Requests: {"error":{"code":"rate_limited","message":"Rate limit exceeded for key ending 91cc. Retry after 3 seconds.","request_id":"req_02b7e6a955f1"}}`,
			"fp_c9f4aa346d1ec77f", "HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds.",
		},
		{
			"row 4 (row 2's text, tool Bash)", "Bash", row2Text,
			"fp_366015c0d74849a2", "[PARSE_SYNTAX_ERROR] Syntax error at or near <q>. SQLSTATE: 42601 (line <n>, pos <n>)",
		},
		{
			"row 5 (row 3 with 404 Not Found)", "mcp__tracker__create_issue",
			"HTTP 404 Not Found: {\"error\":{\"code\":\"rate_limited\",\"message\":\"Rate limit exceeded for key ending 4f2a. Retry after 17 seconds.\",\"request_id\":\"req_8f3a9c21d0b4\"}}",
			"fp_8c90d679b4fe1c9f", "HTTP 404 Not Found code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds.",
		},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			fp, tpl := Of(r.tool, r.text)
			if fp != r.wantFP {
				t.Errorf("fp = %s, want %s (template %q)", fp, r.wantFP, tpl)
			}
			if tpl != r.wantTemplate {
				t.Errorf("template = %q, want %q", tpl, r.wantTemplate)
			}
		})
	}
}

// TestLobeFingerprintDistinct: 20 pairs whose texts have different causes give 20 pairs of different
// fingerprints. Each pair is one tool, or two tools where the tool is the point.
func TestLobeFingerprintDistinct(t *testing.T) {
	pairs := []struct {
		name, toolA, textA, toolB, textB string
	}{
		{"HTTP 404 vs 429", "mcp__tracker__create_issue", "HTTP 404 Not Found: no such issue", "mcp__tracker__create_issue", "HTTP 429 Too Many Requests: slow down"},
		{"HTTP 401 vs 403", "mcp__tracker__create_issue", "HTTP 401 Unauthorized: bad token", "mcp__tracker__create_issue", "HTTP 403 Forbidden: no access"},
		{"SQLSTATE 42601 vs 42P01", "mcp__warehouse__query", "[PARSE_SYNTAX_ERROR] Syntax error at or near 'SELECT'. SQLSTATE: 42601 (line 1, pos 41)", "mcp__warehouse__query", "[TABLE_OR_VIEW_NOT_FOUND] The table or view orders cannot be found. SQLSTATE: 42P01 (line 1, pos 15)"},
		{"ENOENT vs EACCES", "Read", "Error: ENOENT: no such file or directory, open 'a.txt'", "Read", "Error: EACCES: permission denied, open 'a.txt'"},
		{"command not found vs permission denied", "Bash", "bash: foo: command not found", "Bash", "bash: ./run.sh: Permission denied"},
		{"missing script build vs test", "Bash", "npm ERR! missing script: build", "Bash", "npm ERR! missing script: test"},
		{"replace not found vs not read", "Edit", "String to replace not found in file", "Edit", "File has not been read yet. Read it first"},
		{"HTTP 403 vs 500 through WebFetch", "WebFetch", "HTTP 403 Forbidden", "WebFetch", "HTTP 500 Internal Server Error"},
		{"no such file vs permission denied in grep", "Grep", "grep: input.txt: No such file or directory", "Grep", "grep: input.txt: Permission denied"},
		{"exit code 1 vs 2 in a linker error", "Bash", "error: linker command failed with exit code 1", "Bash", "error: linker command failed with exit code 2"},
		{"PowerShell not found vs denied", "PowerShell", "Get-Item : Cannot find path 'C:\\x' because it does not exist.", "PowerShell", "Access to the path is denied"},
		{"push rejected by remote vs protected branch", "Bash", "git push rejected (non-fast-forward)", "Bash", "git push rejected (protected branch)"},
		{"validation head vs base", "mcp__github__create_pull_request", "Validation Failed: head already exists", "mcp__github__create_pull_request", "Validation Failed: base branch does not exist"},
		{"TypeError vs RangeError", "Bash", "TypeError: cannot read properties of undefined (reading 'map')", "Bash", "RangeError: Maximum call stack size exceeded"},
		{"KeyError vs ValueError in a traceback", "Bash", "Traceback (most recent call last):\n  File \"x.py\", line 3, in <module>\nKeyError: 'name'", "Bash", "Traceback (most recent call last):\n  File \"x.py\", line 3, in <module>\nValueError: bad input"},
		{"ORA-00942 vs ORA-01017", "mcp__warehouse__query", "ORA-00942: table or view does not exist", "mcp__warehouse__query", "ORA-01017: invalid username/password"},
		{"TS2345 vs TS2304", "Bash", "TS2345: Argument of type 'string' is not assignable to parameter", "Bash", "TS2304: Cannot find name 'foo'"},
		{"docker vs kubectl not found", "Bash", "sh: 1: docker: not found", "Bash", "sh: 1: kubectl: not found"},
		{"relation does not exist vs syntax at end of input", "mcp__warehouse__query", "relation \"orders\" does not exist. SQLSTATE: 42P01", "mcp__warehouse__query", "syntax error at end of input. SQLSTATE: 42601"},
		{"not a repo vs unable to access remote", "Bash", "fatal: not a git repository", "Bash", "fatal: unable to access remote"},
	}
	if len(pairs) != 20 {
		t.Fatalf("%d pairs, want 20", len(pairs))
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			a, _ := Of(p.toolA, p.textA)
			b, _ := Of(p.toolB, p.textB)
			if a == b {
				t.Errorf("both give %s", a)
			}
		})
	}
}

// TestLobeFingerprintKeywordBeforeCapitalWord: a status code after a keyword and a capital word is kept,
// so `Error: HTTP 404` and `Error: HTTP 429` are different errors (review of 006).
func TestLobeFingerprintKeywordBeforeCapitalWord(t *testing.T) {
	for _, pair := range [][2]string{
		{"Error: HTTP 404 Not Found", "Error: HTTP 429 Not Found"},
		{"status HTTP 404", "status HTTP 429"},
	} {
		a, ta := Of("WebFetch", pair[0])
		b, tb := Of("WebFetch", pair[1])
		if a == b {
			t.Errorf("%q and %q share %s (templates %q, %q)", pair[0], pair[1], a, ta, tb)
		}
	}
}

// TestLobeFingerprintJSONMustBeTheWholeRest: rule (a) reads JSON only when the text from its first `{` is
// one JSON object. A `{` inside a traceback frame does not hide the error line (review of 006).
func TestLobeFingerprintJSONMustBeTheWholeRest(t *testing.T) {
	tb := "Traceback (most recent call last):\n  File \"a.py\", line 3, in <module>\n    f({})\n"
	a, ta := Of("Bash", tb+"ValueError: bad")
	b, tbb := Of("Bash", tb+"KeyError: other")
	if a == b {
		t.Errorf("two tracebacks with different errors share %s (templates %q, %q)", a, ta, tbb)
	}
	if got := Signature(`Error {"code":5} then more words`); strings.Contains(got, "code=5") {
		t.Errorf("text after the object was read as JSON: %q", got)
	}
}
