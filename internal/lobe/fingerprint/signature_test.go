package fingerprint

import (
	"strings"
	"testing"
)

// The error texts of S-08's tester brief, rows 1 to 5, and the changes the brief makes to them.
const (
	row1Text   = "Exit code 127\n/usr/bin/bash: line 1: Get-ChildItem: command not found"
	row1Change = "/bin/bash: line 12: Get-ChildItem: command not found"
	row2Text   = "[PARSE_SYNTAX_ERROR] Syntax error at or near '('. SQLSTATE: 42601 (line 3, pos 27)"
	row2Change = "[PARSE_SYNTAX_ERROR] Syntax error at or near 'SELECT'. SQLSTATE: 42601 (line 1, pos 41)"
	row3Text   = `HTTP 429 Too Many Requests: {"error":{"code":"rate_limited","message":"Rate limit exceeded for key ending 4f2a. Retry after 17 seconds.","request_id":"req_8f3a9c21d0b4"}}`
	row3Change = `HTTP 429 Too Many Requests: {"error":{"code":"rate_limited","message":"Rate limit exceeded for key ending 91cc. Retry after 3 seconds.","request_id":"req_02b7e6a955f1"}}`
	row5Text   = `HTTP 404 Not Found: {"error":{"code":"rate_limited","message":"Rate limit exceeded for key ending 4f2a. Retry after 17 seconds.","request_id":"req_8f3a9c21d0b4"}}`
	row3Sig    = "HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending 4f2a. Retry after 17 seconds."
	row3ChgSig = "HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending 91cc. Retry after 3 seconds."
	row5Sig    = "HTTP 404 Not Found code=rate_limited Rate limit exceeded for key ending 4f2a. Retry after 17 seconds."
	row2Sig    = "[PARSE_SYNTAX_ERROR] Syntax error at or near '('. SQLSTATE: 42601 (line 3, pos 27)"
	row2ChgSig = "[PARSE_SYNTAX_ERROR] Syntax error at or near 'SELECT'. SQLSTATE: 42601 (line 1, pos 41)"
	row1Sig    = "/usr/bin/bash: line 1: Get-ChildItem: command not found"
	row1ChgSig = "/bin/bash: line 12: Get-ChildItem: command not found"
)

// TestSignatureTesterRows checks the signature line of each tester row and each change the brief makes.
// Row 4 is row 2's text under another tool, so its signature is row 2's.
func TestSignatureTesterRows(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"row 1", row1Text, row1Sig},
		{"row 1 change", row1Change, row1ChgSig},
		{"row 1 change with the exit line kept", "Exit code 127\n" + row1Change, row1ChgSig},
		{"row 2", row2Text, row2Sig},
		{"row 2 change", row2Change, row2ChgSig},
		{"row 3", row3Text, row3Sig},
		{"row 3 change", row3Change, row3ChgSig},
		{"row 4 (row 2 text)", row2Text, row2Sig},
		{"row 5", row5Text, row5Sig},
	}
	for _, c := range cases {
		if got := Signature(c.in); got != c.want {
			t.Errorf("%s: Signature = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestSignatureRuleA: the JSON object after the first "{" (S-08 step 2, rule (a)).
func TestSignatureRuleA(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"prefix, code and message", `Request failed: {"code":"x","message":"m"}`, "Request failed code=x m"},
		{"colon before the brace is removed", `HTTP 418: {"message":"teapot"}`, "HTTP 418 teapot"},
		{"status number and msg", `{"status":500,"msg":"boom"}`, "status=500 boom"},
		{"errors[0] is looked up", `{"errors":[{"message":"bad field","code":"E1"}]}`, "code=E1 bad field"},
		{"top level wins over error", `{"message":"top","error":{"message":"inner","code":"c2"}}`, "code=c2 top"},
		{"an object under error is not a value", `{"error":{"code":"x"}}`, "code=x"},
		{"type and error text in order", `{"error":"boom","type":"ValueError"}`, "type=ValueError error=boom"},
		{"detail before title", `{"title":"t","detail":"d"}`, "d"},
		{"title alone", `{"title":"only title"}`, "only title"},
		{"a JSON object with no part falls through to rule (c)", "{}\nerror: boom", "error: boom"},
		{"a text that does not parse falls through", "Failed: {not json", "Failed: {not json"},
	}
	for _, c := range cases {
		if got := Signature(c.in); got != c.want {
			t.Errorf("%s: Signature(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestSignatureRuleB: after a Traceback line, the last line that starts in column 0 (S-08 step 2, rule (b)).
func TestSignatureRuleB(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"last column-0 line of a Python traceback",
			"Traceback (most recent call last):\n  File \"x.py\", line 3, in <module>\n    main()\nKeyError: 'orders'\n",
			"KeyError: 'orders'",
		},
		{
			"a column-0 line before the Traceback is not taken",
			"warning: stale\nTraceback (most recent call last):\n  File \"x.py\", line 3, in <module>\nValueError: bad value\n",
			"ValueError: bad value",
		},
		{
			"no column-0 line after the Traceback falls through to rule (d)",
			"Traceback (most recent call last):\n  File \"x.py\", line 1, in <module>",
			"Traceback (most recent call last):",
		},
	}
	for _, c := range cases {
		if got := Signature(c.in); got != c.want {
			t.Errorf("%s: Signature = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestSignatureRuleC: the first line with a failure word, any case; stack frames and a bare Exit line skipped
// (S-08 step 2, rule (c)).
func TestSignatureRuleC(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"first failure word", "npm WARN fine\nerror: cannot find module 'x'\n", "error: cannot find module 'x'"},
		{"a stack frame is skipped", "    at handleError (x.js:1:2)\nfailed to connect", "failed to connect"},
		{"the keyword is in any case", "ERROR: boom", "ERROR: boom"},
		{"denied in capitals", "Access DENIED for user", "Access DENIED for user"},
		{"a bare Exit line is skipped", "Exit code 127\n/usr/bin/bash: line 1: Get-ChildItem: command not found", "/usr/bin/bash: line 1: Get-ChildItem: command not found"},
		{"a Python File frame is skipped", "  File \"x.py\", line 1, in f\nunable to open", "unable to open"},
	}
	for _, c := range cases {
		if got := Signature(c.in); got != c.want {
			t.Errorf("%s: Signature = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestSignatureRuleD: the first non-empty line that is not a bare Exit line; a bare Exit line only if it is
// the only line; <empty> if there is none (S-08 step 2, rule (d)).
func TestSignatureRuleD(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"first non-empty line", "hello world\nmore text", "hello world"},
		{"a lone bare Exit line is the fallback", "Exit code 3", "Exit code 3"},
		{"a bare Exit line loses to a later line", "Exit code 3\nhello there", "hello there"},
		{"blank lines are skipped and the line is trimmed", "   \n  shell output  ", "shell output"},
	}
	for _, c := range cases {
		if got := Signature(c.in); got != c.want {
			t.Errorf("%s: Signature(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestSignatureEmpty: an empty or blank text gives <empty>; so does a text that is only color codes.
func TestSignatureEmpty(t *testing.T) {
	for _, in := range []string{"", " ", "   \n\t \r\n", "\x1b[31m\x1b[0m", "\n\n"} {
		if got := Signature(in); got != "<empty>" {
			t.Errorf("Signature(%q) = %q, want <empty>", in, got)
		}
	}
}

// TestSignatureColorCodes: ANSI color codes are removed before any rule reads the text.
func TestSignatureColorCodes(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"colored failure line", "\x1b[31mError: boom\x1b[0m", "Error: boom"},
		{"colored plain line", "\x1b[1;33mwarning\x1b[0m", "warning"},
		{"colored JSON", "\x1b[31mHTTP 429 Too Many Requests: {\"error\":{\"code\":\"x\",\"message\":\"m\"}}\x1b[0m", "HTTP 429 Too Many Requests code=x m"},
	}
	for _, c := range cases {
		if got := Signature(c.in); got != c.want {
			t.Errorf("%s: Signature = %q, want %q", c.name, got, c.want)
		}
	}
	if got := stripColorCodes("a\x1b[31"); got != "a\x1b[31" {
		t.Errorf("an unclosed sequence must stay: got %q", got)
	}
}

// TestSignatureCRLF: a CRLF text reads the same as its LF form, and the result has no line break.
func TestSignatureCRLF(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"failure line", "first line\r\nerror: boom\r\n", "error: boom"},
		{"traceback", "Traceback (most recent call last):\r\n  File \"x.py\"\r\nKeyError: 'a'\r\n", "KeyError: 'a'"},
		{"first line", "hello\r\nworld\r\n", "hello"},
	}
	for _, c := range cases {
		if got := Signature(c.in); got != c.want {
			t.Errorf("%s: Signature = %q, want %q", c.name, got, c.want)
		}
	}
	for _, in := range []string{row3Text, row1Text, "a\r\nerror b\r\n"} {
		if strings.ContainsAny(Signature(in), "\r\n") {
			t.Errorf("Signature(%q) holds a line break", in)
		}
	}
}
