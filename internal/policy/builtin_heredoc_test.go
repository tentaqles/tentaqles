package policy

import "testing"

// Heredoc bodies that are data (a note, a fixture, a commit message) are not
// scanned by tq/env-dump; the command line, code bodies and real dumps still
// are.
func TestHeredocDataRules(t *testing.T) {
	s := builtinSet(t)
	cases := []struct {
		name string
		tool string
		cmd  string
		want Action
		rule string
	}{
		// harmless: `set` only mentioned in data
		{"commit message with bare set line", "Bash", "git commit -F - <<'EOF'\nfix: parse the env\n\nset\n(set)\n| set | yes |\nEOF", Allow, ""},
		{"commit -m heredoc with set", "Bash", "git commit -m \"$(cat <<'EOF'\nfix: tester notes\n\nset\nEOF\n)\"", Allow, ""},
		{"chained commit -m heredoc with set is scanned", "Bash", "git add a && git commit -m \"$(cat <<'EOF'\nfix: tester notes\n\nset\nEOF\n)\"", Deny, "tq/env-dump"},
		{"doc with set table", "Bash", "cat > docs/acme.md <<'EOF'\n| cmd | dumps |\n|---|---|\n| set | yes |\n(set)\nEOF", Allow, ""},
		{"quoted message starting with set", "Bash", `git commit -m "set the flag"`, Allow, ""},
		{"quoted message with set up", "Bash", `git commit -m "fix: set up the parser"`, Allow, ""},
		{"declare one var", "Bash", "declare -p PATH", Allow, ""},
		{"one environment variable", "PowerShell", "[Environment]::GetEnvironmentVariable('PATH')", Allow, ""},
		{"declare -p", "Bash", "declare -p", Deny, "tq/env-dump"},
		{"typeset -p", "Bash", "typeset -p | grep KEY", Deny, "tq/env-dump"},
		{"environment variables", "PowerShell", "[Environment]::GetEnvironmentVariables()", Deny, "tq/env-dump"},
		{"system environment variables", "PowerShell", "[System.Environment]::GetEnvironmentVariables('Process')", Deny, "tq/env-dump"},
		{"backtick printenv", "Bash", "echo \"`printenv`\"", Deny, "tq/env-dump"},
		{"set word in args", "Bash", "echo set && grep -rn set src", Allow, ""},
		{"set with options", "Bash", "set -euo pipefail; make test", Allow, ""},
		{"set -x", "Bash", "set -x", Allow, ""},
		{"Set-Content elsewhere", "PowerShell", "Set-Content notes.txt 'x'", Allow, ""},
		{"Set-Location", "PowerShell", "Set-Location src; dir", Allow, ""},
		{"two data heredocs are scanned", "Bash", "cat > a.md <<A\nx\nA\ncat > b.md <<B\nset\nB", Deny, "tq/env-dump"},

		// real dumps
		{"set alone", "Bash", "set", Deny, "tq/env-dump"},
		{"set piped", "Bash", "set | grep AWS", Deny, "tq/env-dump"},
		{"set after and", "Bash", "cd x && set", Deny, "tq/env-dump"},
		{"set in subshell", "Bash", "(set)", Deny, "tq/env-dump"},
		{"set in quoted substitution", "Bash", `echo "$(set)"`, Deny, "tq/env-dump"},
		{"set crlf", "Bash", "ls\r\nset\r\n", Deny, "tq/env-dump"},
		{"cmd set", "PowerShell", "cmd /c set", Deny, "tq/env-dump"},
		{"bash -c set", "Bash", `bash -c "ls; set"`, Deny, "tq/env-dump"},
		{"gci env", "PowerShell", "Get-ChildItem env:", Deny, "tq/env-dump"},
		{"gci env after semicolon", "PowerShell", "cd x; Get-ChildItem Env:", Deny, "tq/env-dump"},
		{"printenv", "Bash", "printenv", Deny, "tq/env-dump"},
		{"printenv after data heredoc", "Bash", "cat > a <<EOF\nx\nEOF\nprintenv", Deny, "tq/env-dump"},
		{"env sort", "Bash", "env | sort", Deny, "tq/env-dump"},
		{"set in bash heredoc", "Bash", "bash <<'EOF'\nset\nEOF", Deny, "tq/env-dump"},
		{"set in heredoc piped to sh", "Bash", "cat <<'EOF' | sh\nset\nEOF", Deny, "tq/env-dump"},
		{"printenv in sh heredoc", "Bash", "sh <<'EOF'\nls\nprintenv\nEOF", Deny, "tq/env-dump"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := s.Evaluate(ToolCall{Tool: tc.tool, Command: tc.cmd})
			if d.Action != tc.want {
				t.Fatalf("action = %q, want %q (%s)", d.Action, tc.want, d.Reason())
			}
			if tc.rule != "" {
				found := false
				for _, r := range d.Matched {
					if r.ID == tc.rule {
						found = true
					}
				}
				if !found {
					t.Errorf("rules = %s, want %s", d.Reason(), tc.rule)
				}
			}
		})
	}
}
