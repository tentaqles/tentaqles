package policy

import "testing"

// TestReviewForms is the coverage review's table for fix/hook-heredoc: the
// forms the narrowed rules must still deny (base denied them, or they are
// real dumps/writes) and the false positives they must now allow.
func TestReviewForms(t *testing.T) {
	s := builtinSet(t)
	w := "echo 'disable: [x]' > " + gf // a write of the guard file
	const gw, ed = "tq/guard-file-write", "tq/env-dump"
	cases := []struct {
		name, tool, cmd string
		want            Action
		rule            string
	}{
		{"1 cat piped to bash", "Bash", "cat <<EOF | bash\n" + w + "\nEOF", Deny, gw},
		{"2 bash", "Bash", "bash <<EOF\n" + w + "\nEOF", Deny, gw},
		{"2 bash -s", "Bash", "bash -s <<EOF\n" + w + "\nEOF", Deny, gw},
		{"2 sh", "Bash", "sh <<EOF\n" + w + "\nEOF", Deny, gw},
		{"2 env bash", "Bash", "/usr/bin/env bash <<EOF\n" + w + "\nEOF", Deny, gw},
		{"2 python -", "Bash", "python - <<EOF\nopen('" + gf + "', 'w').write('x')\nEOF", Deny, gw},
		{"3 eval substitution", "Bash", "eval \"$(cat <<'EOF'\n" + w + "\nEOF\n)\"", Deny, gw},
		{"3 sh -c substitution", "Bash", "sh -c \"$(cat <<'EOF'\n" + w + "\nEOF\n)\"", Deny, gw},
		{"4 source stdin", "Bash", "source /dev/stdin <<EOF\n" + w + "\nEOF", Deny, gw},
		{"4 dot stdin", "Bash", ". /dev/stdin <<EOF\n" + w + "\nEOF", Deny, gw},
		{"5 fake terminator", "Bash", "cat > n.md <<EOF\nEOF \n" + w + "\nEOF", Deny, gw},
		{"5 tab terminator then printenv", "Bash", "cat > n.md <<EOF\n\tEOF\nprintenv\nEOF", Deny, ed},
		{"6 heredoc redirect to the guard file", "Bash", "cat > " + gf + " <<EOF\ndisable: [x]\nEOF", Deny, gw},
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
		{"8 here-string to iex", "PowerShell", "@'\nSet-Content $HOME\\.tentaqles\\guard.yaml 'x'\n'@ | iex", Deny, gw},
		{"8 here-string to Invoke-Expression", "PowerShell", "@'\nSet-Content $HOME\\.tentaqles\\guard.yaml 'x'\n'@ | Invoke-Expression", Deny, gw},
		{"8b here-string env dump to iex", "PowerShell", "@'\nGet-ChildItem env:\n'@ | iex", Deny, ed},
		{"8b double here-string env dump", "PowerShell", "@\"\nGet-ChildItem env:\n\"@ | Invoke-Expression", Deny, ed},
		{"8b here-string cmd set", "PowerShell", "@'\ncmd /c set\n'@ | iex", Deny, ed},
		{"9 note naming the guard file", "Bash", "cat > notes.md <<'EOF'\nThe guard file is " + gf + ".\nEOF", Allow, ""},
		{"10 commit -F - set the flag", "Bash", "git commit -F - <<EOF\nfix: set the flag\n\nset\nEOF", Allow, ""},
		{"11 commit -m heredoc set up", "Bash", "git commit -m \"$(cat <<'EOF'\nfix: set up the parser\n\nset\nEOF\n)\"", Allow, ""},
		{"X1 quoted << then a write", "Bash", "git commit -m 'note <<X'\n" + w + "\nX", Deny, gw},
		{"X1b quoted << argument then printenv", "Bash", "cat '<<X' > n.md\nprintenv\nX", Deny, ed},
		{"X2 quote inside the delimiter", "Bash", "cat > n.md <<E\"O\"F\nEOF\n" + w + "\nE", Deny, gw},
		{"X3 find -exec sh -c substitution", "Bash", "find . -exec sh -c \"$(cat <<'EOF'\n" + w + "\nEOF\n)\" \\;", Deny, gw},
		{"X3b su -c substitution", "Bash", "su -c \"$(cat <<'EOF'\n" + w + "\nEOF\n)\"", Deny, gw},
		{"X3c wsl sh -c substitution", "Bash", "wsl sh -c \"$(cat <<'EOF'\necho x > /mnt/c/Users/tester/.tentaqles/guard.yaml\nEOF\n)\"", Deny, gw},
		{"X4 busybox sh", "Bash", "cat <<EOF | busybox sh\n" + w + "\nEOF", Deny, gw},
		{"X4b ash", "Bash", "cat <<EOF | ash\n" + w + "\nEOF", Deny, gw},
		{"X4c at now", "Bash", "cat <<EOF | at now\n" + w + "\nEOF", Deny, gw},
		{"X4d xargs sh", "Bash", "cat <<EOF | xargs -0 -I{} sh -c {}\n" + w + "\nEOF", Deny, gw},
		{"X11 sudo -u root bash", "Bash", "cat <<EOF | sudo -u root bash\n" + w + "\nEOF", Deny, gw},
		{"X5 makefile then make", "Bash", "cat > mk <<'EOF'\nall:\n\t" + w + "\nEOF\nmake -f mk", Deny, gw},
		{"X5b file then find -exec sh", "Bash", "cat > x.txt <<'EOF'\n" + w + "\nEOF\nfind x.txt -exec sh {} \\;", Deny, gw},
		{"X9 tee then sh", "Bash", "tee x <<EOF\n" + w + "\nEOF\nsh x", Deny, gw},
		{"X10 env -i bash", "Bash", "env -i bash <<EOF\n" + w + "\nEOF", Deny, gw},
		{"X16 git alias", "Bash", "git -c alias.c='!sh' c commit <<EOF\n" + w + "\nEOF", Deny, gw},
		{"X6 apostrophe in a comment", "Bash", "ls # it's fine\nset", Deny, ed},
		{"X6b apostrophe in a comment then printenv", "Bash", "ls # don't\nprintenv", Deny, ed},
		{"X7 powershell backslash", "PowerShell", `Write-Output "a\"; Get-ChildItem env:`, Deny, ed},
		{"X7b powershell backslash cmd set", "PowerShell", `Write-Output "C:\"; cmd /c set`, Deny, ed},
		{"X8 note with set", "Bash", "cat > n.md <<EOF\nset\nEOF", Allow, ""},
		{"X12 bash -c multi-line", "Bash", "bash -c 'ls\nset'", Deny, ed},
		{"X13 commit -m set substitution", "Bash", `git commit -m "$(set)"`, Deny, ed},
		{"X14 backtick printenv", "Bash", "echo \"`printenv`\"", Deny, ed},
		{"X15 env in a file then sh", "Bash", "cat > x <<EOF\nenv\nEOF\nsh x", Deny, ed},
		{"continued opener piped to bash", "Bash", "cat <<EOF \\\n | bash\n" + w + "\nEOF", Deny, gw},

		// re-review (round 2)
		{"E4c gh pr create then bash -c set", "Bash", "gh pr create --body \"$(cat <<'EOF'\nx\nEOF\n)\" && bash -c 'set'", Deny, ed},
		{"E5e commit substitution then bash -c set", "Bash", "git commit -m \"$(cat <<'EOF'\nx\nEOF\n)\"; bash -c 'set'", Deny, ed},
		{"N1 hook written then commit", "Bash", "cat > .git/hooks/pre-commit <<'EOF'\n#!/bin/sh\n" + w + "\nEOF\nchmod +x .git/hooks/pre-commit && git commit -m x", Deny, gw},
		{"N1b commit-msg hook then commit -F -", "Bash", "cat > .git/hooks/commit-msg <<'A'\n#!/bin/sh\n" + w + "\nA\nchmod +x .git/hooks/commit-msg\ngit commit -F - <<'B'\nmsg\nB", Deny, gw},
		{"N1c fsmonitor config then git status", "Bash", "cat >> .git/config <<'EOF'\n[core]\n\tfsmonitor = \"" + w + "\"\nEOF\ngit status", Deny, gw},
		{"N1d mv into hooks then commit", "Bash", "cat > h <<'EOF'\n#!/bin/sh\n" + w + "\nEOF\nmv h .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit && git commit -m x", Deny, gw},
		{"N1e cd hooks then commit", "Bash", "cd .git/hooks && cat > pre-commit <<'EOF'\n#!/bin/sh\n" + w + "\nEOF\nchmod +x pre-commit && cd ../.. && git commit -m x", Deny, gw},
		{"N1f hook written alone", "Bash", "cat > .git/hooks/pre-commit <<'EOF'\n#!/bin/sh\n" + w + "\nEOF", Deny, gw},
		{"N1g git config by tee", "Bash", "tee -a .git/config <<'EOF'\n[core]\n\tfsmonitor = \"" + w + "\"\nEOF", Deny, gw},
		{"N1h windows git dir", "Bash", "cat > .git\\hooks\\pre-push <<'EOF'\n" + w + "\nEOF", Deny, gw},
		{"N1i global gitconfig", "Bash", "cat >> ~/.gitconfig <<'EOF'\n[core]\n\thooksPath = h\n" + w + "\nEOF", Deny, gw},
		{"N1j hooks path dir", "Bash", "cat > hooks/pre-commit <<'EOF'\n" + w + "\nEOF", Deny, gw},
		{"chained cd then cat", "Bash", "cd docs && cat > n.md <<'EOF'\n" + w + "\nEOF", Deny, gw},
		{"chained cat then ls", "Bash", "cat > n.md <<'EOF'\n" + w + "\nEOF\nls", Deny, gw},
		{"N2 param expansion holds <<", "Bash", "cat ${x/<<X/}\n" + w + "\nX/}", Deny, gw},
		{"N3 ansi-c quote", "Bash", "cat $'a\\' <<X ''\n" + w + "\nX", Deny, gw},
		{"N3b ansi-c quote then printenv", "Bash", "cat $'a\\' <<X ''\nprintenv\nX", Deny, ed},
		{"R10 bash -c set", "Bash", "bash -c 'set'", Deny, ed},
		{"R10 sh -c printenv", "Bash", `sh -c "printenv"`, Deny, ed},
		{"R10b bash -c padded env", "Bash", "bash -c ' env '", Deny, ed},
		{"R10c sh -c padded printenv", "Bash", `sh -c " printenv"`, Deny, ed},
		{"R10d bash -c padded set -e stays allowed", "Bash", "bash -c ' set -e; ls'", Allow, ""},
		{"R10 bash -c env", "Bash", "bash -c 'env'", Deny, ed},
		{"R10 bash -lc set piped", "Bash", "bash -lc 'set | grep KEY'", Deny, ed},

		// the original false positives stay allowed
		{"orig note naming the guard file", "Bash", "cat > notes.md <<'EOF'\nThe guard file is " + gf + ".\nEOF", Allow, ""},
		{"orig commit -F - set the flag", "Bash", "git commit -F - <<EOF\nset the flag\nEOF", Allow, ""},
		{"orig commit -m substitution", "Bash", "git commit -m \"$(cat <<'EOF'\ndocs: " + gf + "\n\nset\nEOF\n)\"", Allow, ""},
		{"orig gh pr body", "Bash", "gh pr create --title t --body \"$(cat <<'EOF'\nTouches " + gf + "\nset\nEOF\n)\"", Allow, ""},
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
