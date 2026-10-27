package policy

import "testing"

const gf = "~/.tentaqles/guard.yaml"

// Heredoc bodies that are data (a note, a fixture, a commit message) are not
// scanned by tq/guard-file-write and tq/env-dump; the command line, code
// bodies and real dumps or writes still are.
func TestHeredocDataRules(t *testing.T) {
	s := builtinSet(t)
	cases := []struct {
		name string
		tool string
		cmd  string
		want Action
		rule string
	}{
		// harmless: the guard file or `set` only mentioned in data
		{"note mentions guard file", "Bash", "cat > docs/notes.md <<'EOF'\nThe global guard file is " + gf + ".\nEOF", Allow, ""},
		{"fixture mentions guard file", "Bash", "mkdir -p testdata && cat <<EOF > testdata/acme.md\npath: " + gf + "\nEOF", Allow, ""},
		{"tee fixture mentions guard file", "Bash", "tee testdata/acme.txt >/dev/null <<'EOF'\n" + gf + "\nEOF", Allow, ""},
		{"commit -F - mentions guard file", "Bash", "git commit -F - <<'EOF'\ndocs: explain " + gf + "\nEOF", Allow, ""},
		{"commit -m heredoc mentions guard file", "Bash", "git commit -m \"$(cat <<'EOF'\ndocs: explain " + gf + "\nEOF\n)\"", Allow, ""},
		{"pr body mentions guard file", "Bash", "gh pr create --title t --body \"$(cat <<'EOF'\nTouches " + gf + " handling.\nEOF\n)\"", Allow, ""},
		{"commit message with bare set line", "Bash", "git commit -F - <<'EOF'\nfix: parse the env\n\nset\n(set)\n| set | yes |\nEOF", Allow, ""},
		{"commit -m heredoc with set", "Bash", "git add a && git commit -m \"$(cat <<'EOF'\nfix: tester notes\n\nset\nEOF\n)\"", Allow, ""},
		{"doc with set table", "Bash", "cat > docs/acme.md <<'EOF'\n| cmd | dumps |\n|---|---|\n| set | yes |\n(set)\nEOF", Allow, ""},
		{"quoted message starting with set", "Bash", `git commit -m "set the flag"`, Allow, ""},
		{"quoted message with set up", "Bash", `git commit -m "fix: set up the parser"`, Allow, ""},
		{"declare one var", "Bash", "declare -p PATH", Allow, ""},
		{"one environment variable", "PowerShell", "[Environment]::GetEnvironmentVariable('PATH')", Allow, ""},
		// PowerShell is scanned as it is: no heredoc is data there
		{"powershell text naming the guard file", "PowerShell", "cat > notes.md <<'EOF'\n" + gf + "\nEOF", Deny, "tq/guard-file-write"},
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
		{"two data heredocs", "Bash", "cat > a.md <<A\n" + gf + "\nA\ncat > b.md <<B\nset\nB", Allow, ""},

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

		// real writes of the guard file
		{"redirect", "Bash", "echo 'disable: [x]' > " + gf, Deny, "tq/guard-file-write"},
		{"heredoc redirect", "Bash", "cat > " + gf + " <<'EOF'\ndisable: [x]\nEOF", Deny, "tq/guard-file-write"},
		{"heredoc redirect after marker", "Bash", "cat <<'EOF' > " + gf + "\ndisable: [x]\nEOF", Deny, "tq/guard-file-write"},
		{"tee", "Bash", "echo x | tee " + gf, Deny, "tq/guard-file-write"},
		{"tee heredoc", "Bash", "tee " + gf + " <<EOF\nx\nEOF", Deny, "tq/guard-file-write"},
		{"Set-Content", "PowerShell", `Set-Content $HOME\.tentaqles\guard.yaml 'x'`, Deny, "tq/guard-file-write"},
		{"Out-File", "PowerShell", `'x' | Out-File -FilePath $HOME\.tentaqles\guard.yaml`, Deny, "tq/guard-file-write"},
		{"cp", "Bash", "cp acme.yaml " + gf, Deny, "tq/guard-file-write"},
		{"bash heredoc writes", "Bash", "bash <<'EOF'\necho 'disable: [x]' > " + gf + "\nEOF", Deny, "tq/guard-file-write"},
		{"heredoc piped to bash writes", "Bash", "cat <<'EOF' | bash\necho x > " + gf + "\nEOF", Deny, "tq/guard-file-write"},
		{"python heredoc writes", "Bash", "python3 - <<'EOF'\nopen('" + gf + "', 'w').write('x')\nEOF", Deny, "tq/guard-file-write"},
		{"script written then run", "Bash", "cat > x.sh <<'EOF'\necho x > " + gf + "\nEOF\nbash x.sh", Deny, "tq/guard-file-write"},
		{"unterminated heredoc", "Bash", "cat > a.md <<EOF\n" + gf, Deny, "tq/guard-file-write"},
		{"eval substitution", "Bash", "eval \"$(cat <<'EOF'\necho x > " + gf + "\nEOF\n)\"", Deny, "tq/guard-file-write"},
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
