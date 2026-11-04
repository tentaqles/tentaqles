package policy

import "testing"

// TestReviewForms is the coverage review's table for fix/hook-heredoc, kept
// for tq/env-dump: the forms it must still deny (real dumps, in every
// wrapper that runs a heredoc) and the false positives it must allow.
func TestReviewForms(t *testing.T) {
	s := builtinSet(t)
	const ed = "tq/env-dump"
	cases := []struct {
		name, tool, cmd string
		want            Action
		rule            string
	}{
		{"5 tab terminator then printenv", "Bash", "cat > n.md <<EOF\n\tEOF\nprintenv\nEOF", Deny, ed},
		{"7 set", "Bash", "set", Deny, ed},
		{"7 set piped", "Bash", "set | more", Deny, ed},
		{"7 env", "Bash", "env", Deny, ed},
		{"7 printenv", "Bash", "printenv", Deny, ed},
		{"7 export -p", "Bash", "export -p", Deny, ed},
		{"7 declare -p", "Bash", "declare -p", Deny, ed},
		{"7 cmd /c set", "PowerShell", "cmd /c set", Deny, ed},
		{"7 Get-ChildItem env", "PowerShell", "Get-ChildItem env:", Deny, ed},
		{"7 gci env", "PowerShell", "gci env:", Deny, ed},
		{"7 dir env", "PowerShell", "dir env:", Deny, ed},
		{"7 ls env", "PowerShell", "ls env:", Deny, ed},
		{"7 Environment variables", "PowerShell", "[Environment]::GetEnvironmentVariables()", Deny, ed},
		{"8b here-string env dump to iex", "PowerShell", "@'\nGet-ChildItem env:\n'@ | iex", Deny, ed},
		{"8b double here-string env dump", "PowerShell", "@\"\nGet-ChildItem env:\n\"@ | Invoke-Expression", Deny, ed},
		{"8b here-string cmd set", "PowerShell", "@'\ncmd /c set\n'@ | iex", Deny, ed},
		{"10 commit -F - set the flag", "Bash", "git commit -F - <<EOF\nfix: set the flag\n\nset\nEOF", Allow, ""},
		{"11 commit -m heredoc set up", "Bash", "git commit -m \"$(cat <<'EOF'\nfix: set up the parser\n\nset\nEOF\n)\"", Allow, ""},
		{"X1b quoted << argument then printenv", "Bash", "cat '<<X' > n.md\nprintenv\nX", Deny, ed},
		{"X6 apostrophe in a comment", "Bash", "ls # it's fine\nset", Deny, ed},
		{"X6b apostrophe in a comment then printenv", "Bash", "ls # don't\nprintenv", Deny, ed},
		{"X7 powershell backslash", "PowerShell", `Write-Output "a\"; Get-ChildItem env:`, Deny, ed},
		{"X7b powershell backslash cmd set", "PowerShell", `Write-Output "C:\"; cmd /c set`, Deny, ed},
		{"X8 note with set", "Bash", "cat > n.md <<EOF\nset\nEOF", Allow, ""},
		{"X12 bash -c multi-line", "Bash", "bash -c 'ls\nset'", Deny, ed},
		{"X13 commit -m set substitution", "Bash", `git commit -m "$(set)"`, Deny, ed},
		{"X14 backtick printenv", "Bash", "echo \"`printenv`\"", Deny, ed},
		{"X15 env in a file then sh", "Bash", "cat > x <<EOF\nenv\nEOF\nsh x", Deny, ed},

		// re-review (round 2)
		{"E4c gh pr create then bash -c set", "Bash", "gh pr create --body \"$(cat <<'EOF'\nx\nEOF\n)\" && bash -c 'set'", Deny, ed},
		{"E5e commit substitution then bash -c set", "Bash", "git commit -m \"$(cat <<'EOF'\nx\nEOF\n)\"; bash -c 'set'", Deny, ed},
		{"N3b ansi-c quote then printenv", "Bash", "cat $'a\\' <<X ''\nprintenv\nX", Deny, ed},
		{"R10 bash -c set", "Bash", "bash -c 'set'", Deny, ed},
		{"R10 sh -c printenv", "Bash", `sh -c "printenv"`, Deny, ed},
		{"R10b bash -c padded env", "Bash", "bash -c ' env '", Deny, ed},
		{"R10c sh -c padded printenv", "Bash", `sh -c " printenv"`, Deny, ed},
		{"R10d bash -c padded set -e stays allowed", "Bash", "bash -c ' set -e; ls'", Allow, ""},
		{"R10 bash -c env", "Bash", "bash -c 'env'", Deny, ed},
		{"R10 bash -lc set piped", "Bash", "bash -lc 'set | grep KEY'", Deny, ed},

		// the original false positives stay allowed
		{"orig commit -F - set the flag", "Bash", "git commit -F - <<EOF\nset the flag\nEOF", Allow, ""},
		{"orig commit -m substitution", "Bash", "git commit -m \"$(cat <<'EOF'\ndocs: notes\n\nset\nEOF\n)\"", Allow, ""},
		{"orig gh pr body", "Bash", "gh pr create --title t --body \"$(cat <<'EOF'\nTouches the env\nset\nEOF\n)\"", Allow, ""},
		{"bash -c one variable", "Bash", "bash -c 'printenv PATH'", Allow, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := s.Evaluate(ToolCall{Tool: tc.tool, Command: tc.cmd})
			if d.Action != tc.want {
				t.Fatalf("action = %q, want %q (%s)", d.Action, tc.want, d.Reason())
			}
			if tc.rule == "" {
				return
			}
			for _, r := range d.Matched {
				if r.ID == tc.rule {
					return
				}
			}
			t.Errorf("rules = %s, want %s", d.Reason(), tc.rule)
		})
	}
}
