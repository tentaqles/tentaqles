package policy

import "testing"

// tq/trust-store-write denies agent writes to tq's trust store and registry
// ($TQ_HOME/trust/**, $TQ_HOME/config.yaml) and leaves reads alone.
func TestTrustStoreWrite(t *testing.T) {
	s := builtinSet(t)
	const id = "tq/trust-store-write"
	deny := []ToolCall{
		{Tool: "Write", Path: `C:\Users\renato\.tentaqles\trust\abc123.bypass`, Content: "x"},
		{Tool: "Edit", Path: "/home/u/.tentaqles/config.yaml", Content: "bases: []"},
		{Tool: "NotebookEdit", Path: "/home/u/.tentaqles/trust/x.ipynb", Content: "x"},
		{Tool: "MultiEdit", Path: "/home/u/.tentaqles/trust/abc", Content: "x"},
		{Tool: "Write", Path: "$TQ_HOME/trust/abc.bypass", Content: "x"},
		{Tool: "Bash", Command: "echo ok > ~/.tentaqles/trust/abc.bypass"},
		{Tool: "Bash", Command: `echo ok >> "$TQ_HOME/config.yaml"`},
		{Tool: "Bash", Command: "echo ok > ${TQ_HOME}/trust/abc"},
		{Tool: "Bash", Command: "touch x && cp x ~/.tentaqles/trust/abc.bypass"},
		{Tool: "Bash", Command: "cp /tmp/config.yaml $TQ_HOME/config.yaml"},
		{Tool: "Bash", Command: "mv ~/.tentaqles/trust /tmp/old"},
		{Tool: "Bash", Command: "rm -rf ~/.tentaqles/trust"},
		{Tool: "Bash", Command: "rm $TQ_HOME/config.yaml"},
		{Tool: "Bash", Command: "echo y | tee ~/.tentaqles/trust/abc.bypass"},
		{Tool: "Bash", Command: "sed -i s/a/b/ ~/.tentaqles/config.yaml"},
		{Tool: "PowerShell", Command: `Set-Content $env:TQ_HOME\trust\abc.bypass ok`},
		{Tool: "PowerShell", Command: `Add-Content $HOME\.tentaqles\config.yaml "x"`},
		{Tool: "PowerShell", Command: `"ok" | Out-File -FilePath $env:USERPROFILE\.tentaqles\trust\abc.bypass`},
		{Tool: "PowerShell", Command: `Remove-Item -Recurse $HOME\.tentaqles\trust`},
		{Tool: "PowerShell", Command: `Copy-Item x $env:TQ_HOME\config.yaml -Force`},
		{Tool: "PowerShell", Command: `Move-Item $HOME\.tentaqles\trust C:\tmp\t`},
		{Tool: "Bash", Command: `python -c "open('/home/u/.tentaqles/trust/abc.bypass','w').write('')"`},
		{Tool: "Bash", Command: `python3 -c "import os; os.remove(os.path.expanduser('~/.tentaqles/config.yaml'))"`},
		{Tool: "Bash", Command: `node -e "require('fs').writeFileSync(process.env.HOME+'/.tentaqles/trust/abc.bypass','')"`},
		{Tool: "PowerShell", Command: `cmd /c "echo x > %TQ_HOME%\trust\abc.bypass"`},
	}
	for _, c := range deny {
		d := s.Evaluate(c)
		if d.Action != Deny || !hasRule(d, id) {
			t.Errorf("%s %q %q: want deny %s, got %s %v", c.Tool, c.Path, c.Command, id, d.Action, ruleIDs(d.Matched))
		}
	}
	allow := []ToolCall{
		{Tool: "Read", Path: "/home/u/.tentaqles/config.yaml"},
		{Tool: "Read", Path: `C:\Users\renato\.tentaqles\trust\abc.bypass`},
		{Tool: "Grep", Path: "/home/u/.tentaqles/trust"},
		{Tool: "Write", Path: "/home/u/.tentaqles/notes/config.yaml", Content: "x"},
		{Tool: "Write", Path: "/home/u/project/trust/x", Content: "x"},
		{Tool: "Bash", Command: "cat ~/.tentaqles/config.yaml"},
		{Tool: "Bash", Command: "ls -la $TQ_HOME/trust"},
		{Tool: "Bash", Command: "grep bases ~/.tentaqles/config.yaml > /tmp/out.txt"},
		{Tool: "Bash", Command: "cp ~/.tentaqles/config.yaml /tmp/backup.yaml"},
		{Tool: "Bash", Command: "cat ~/.tentaqles/config.yaml | tee /tmp/copy.yaml"},
		{Tool: "PowerShell", Command: `Get-Content $env:TQ_HOME\config.yaml`},
		{Tool: "PowerShell", Command: `Get-ChildItem $HOME\.tentaqles\trust`},
		{Tool: "Bash", Command: `python -c "print(open('/home/u/.tentaqles/config.yaml').read())"`},
		{Tool: "Bash", Command: "tq allow acme --bypass"},
	}
	for _, c := range allow {
		if d := s.Evaluate(c); hasRule(d, id) {
			t.Errorf("%s %q %q: a read must not match %s", c.Tool, c.Path, c.Command, id)
		}
	}
}

func hasRule(d Decision, id string) bool {
	for _, r := range d.Matched {
		if r.ID == id {
			return true
		}
	}
	return false
}

// tq/trust-store-write also covers the files tq renders into an identity:
// the codex identity's rules/ and hooks.json (and its config.toml, which
// holds the approval policy and hook trust), and the Claude identity's
// settings files. An agent that edits them widens its own session.
func TestTrustStoreWriteCoversIdentityFiles(t *testing.T) {
	s := builtinSet(t)
	const id = "tq/trust-store-write"
	deny := []ToolCall{
		{Tool: "Edit", Path: `C:\Users\u\.tentaqles\identities\acme\codex\rules\tq.rules`, Content: "x"},
		{Tool: "Write", Path: "/home/u/.tentaqles/identities/acme/codex/rules/extra.rules", Content: "x"},
		{Tool: "Write", Path: "/home/u/.tentaqles/identities/acme/codex/hooks.json", Content: "{}"},
		{Tool: "Edit", Path: "/home/u/.tentaqles/identities/acme/codex/config.toml", Content: "x"},
		{Tool: "Write", Path: "/home/u/.tentaqles/identities/acme/claude/settings.json", Content: "{}"},
		{Tool: "Edit", Path: "$TQ_HOME/identities/acme/claude/settings.local.json", Content: "{}"},
		{Tool: "Bash", Command: "echo x >> ~/.tentaqles/identities/acme/codex/rules/tq.rules"},
		{Tool: "Bash", Command: "rm ~/.tentaqles/identities/acme/codex/hooks.json"},
		{Tool: "PowerShell", Command: `Set-Content $env:TQ_HOME\identities\acme\claude\settings.json '{}'`},
	}
	for _, c := range deny {
		d := s.Evaluate(c)
		if d.Action != Deny || !hasRule(d, id) {
			t.Errorf("%s %q %q: want deny %s, got %s %v", c.Tool, c.Path, c.Command, id, d.Action, ruleIDs(d.Matched))
		}
	}
	allow := []ToolCall{
		{Tool: "Read", Path: "/home/u/.tentaqles/identities/acme/codex/rules/tq.rules"},
		{Tool: "Write", Path: "/home/u/.tentaqles/identities/acme/codex/notes.md", Content: "x"},
		{Tool: "Write", Path: "/home/u/project/codex/rules/x", Content: "x"},
		{Tool: "Bash", Command: "cat ~/.tentaqles/identities/acme/codex/hooks.json"},
		{Tool: "Bash", Command: "cp ~/.tentaqles/identities/acme/claude/settings.json /tmp/s.json"},
	}
	for _, c := range allow {
		if d := s.Evaluate(c); hasRule(d, id) {
			t.Errorf("%s %q %q: must not match %s", c.Tool, c.Path, c.Command, id)
		}
	}
}
