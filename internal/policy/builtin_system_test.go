package policy

import "testing"

func evalCorpus(t *testing.T, id string, want Action, calls []ToolCall) {
	t.Helper()
	s := builtinSet(t)
	for _, c := range calls {
		d := s.Evaluate(c)
		got := hasRule(d, id)
		if want == Allow {
			if got {
				t.Errorf("%s %q %q: %s must not match, got %s %v", c.Tool, c.Path, c.Command, id, d.Action, ruleIDs(d.Matched))
			}
			continue
		}
		if !got || d.Action != want {
			t.Errorf("%s %q %q: want %s %s, got %s %v", c.Tool, c.Path, c.Command, want, id, d.Action, ruleIDs(d.Matched))
		}
	}
}

func TestSystemDirWrite(t *testing.T) {
	const id = "tq/system-dir-write"
	evalCorpus(t, id, Deny, []ToolCall{
		{Tool: "Write", Path: `C:\Windows\System32\drivers\etc\hosts`, Content: "x"},
		{Tool: "Edit", Path: `C:\Program Files\App\config.ini`, Content: "x"},
		{Tool: "Write", Path: `c:/program files (x86)/x/y.txt`, Content: "x"},
		{Tool: "MultiEdit", Path: `C:\ProgramData\x\y`, Content: "x"},
		{Tool: "Write", Path: "/etc/hosts", Content: "x"},
		{Tool: "Write", Path: "/usr/local/bin/tool", Content: "x"},
		{Tool: "Write", Path: "/c/Windows/x.ini", Content: "x"},
		{Tool: "Bash", Command: "echo 127.0.0.1 x > /etc/hosts"},
		{Tool: "Bash", Command: "echo x | tee -a /etc/profile"},
		{Tool: "Bash", Command: "sudo rm -rf /usr/lib/x"},
		{Tool: "Bash", Command: "rm /etc/hosts"},
		{Tool: "Bash", Command: "cp ./tool /usr/local/bin/"},
		{Tool: "Bash", Command: "mv build/app /bin/app"},
		{Tool: "Bash", Command: "sed -i s/a/b/ /etc/ssh/sshd_config"},
		{Tool: "Bash", Command: `cp x "C:\Program Files\App\x.dll"`},
		{Tool: "PowerShell", Command: `Set-Content -Path C:\Windows\x.ini -Value 1`},
		{Tool: "PowerShell", Command: `Remove-Item -Recurse -Force "C:\Program Files\Old"`},
		{Tool: "PowerShell", Command: `"x" | Out-File C:\ProgramData\x.txt`},
		{Tool: "PowerShell", Command: `Copy-Item .\a.dll C:\Windows\System32\a.dll`},
		{Tool: "Bash", Command: "mkdir -p /Library/Foo"},
	})
	evalCorpus(t, id, Allow, []ToolCall{
		{Tool: "Read", Path: `C:\Windows\System32\drivers\etc\hosts`},
		{Tool: "Write", Path: `C:\repos\windows\x.txt`, Content: "x"},
		{Tool: "Write", Path: "/home/u/etc/x", Content: "x"},
		{Tool: "Bash", Command: "cat /etc/hosts"},
		{Tool: "Bash", Command: "ls /usr/bin | grep x"},
		{Tool: "Bash", Command: "cp /etc/hosts ./hosts.bak"},
		{Tool: "Bash", Command: "grep -r x /usr/include > out.txt"},
		{Tool: "Bash", Command: "go build ./... 2>/dev/null"},
		{Tool: "Bash", Command: "export PATH=/usr/local/bin:$PATH"},
		{Tool: "PowerShell", Command: `Get-Content C:\Windows\win.ini`},
		{Tool: "Bash", Command: "rm -rf ./build && ls /usr/bin"},
	})
}

func TestElevation(t *testing.T) {
	const id = "tq/elevation"
	evalCorpus(t, id, Ask, []ToolCall{
		{Tool: "Bash", Command: "sudo apt-get install x"},
		{Tool: "Bash", Command: "cd x && sudo make install"},
		{Tool: "Bash", Command: "doas pkg_add x"},
		{Tool: "Bash", Command: "su -c 'id'"},
		{Tool: "Bash", Command: "su root -c id"},
		{Tool: "Bash", Command: "pkexec env x"},
		{Tool: "Bash", Command: "echo x | sudo tee /tmp/x"},
		{Tool: "PowerShell", Command: "gsudo choco install x"},
		{Tool: "PowerShell", Command: `runas /user:Administrator cmd`},
		{Tool: "PowerShell", Command: `Start-Process pwsh -Verb RunAs -ArgumentList '-c x'`},
		{Tool: "PowerShell", Command: `Start-Process -FilePath cmd -Verb "RunAs"`},
		{Tool: "Bash", Command: "/usr/bin/sudo id"},
	})
	evalCorpus(t, id, Allow, []ToolCall{
		{Tool: "Bash", Command: "echo use sudo carefully"},
		{Tool: "Bash", Command: `git commit -m "docs: never run sudo"`},
		{Tool: "Bash", Command: "grep -r sudoers docs/"},
		{Tool: "Bash", Command: "suspend-check --x"},
		{Tool: "PowerShell", Command: "Start-Process notepad"},
		{Tool: "Bash", Command: "cat <<'EOF' > notes.md\nrun sudo make install\nEOF"},
	})
}

func TestModeChange(t *testing.T) {
	const id = "tq/mode-change"
	evalCorpus(t, id, Deny, []ToolCall{
		{Tool: "Bash", Command: "tq mode set relaxed"},
		{Tool: "Bash", Command: "tq mode toggle system skip --ws acme"},
		{Tool: "Bash", Command: "tq mode override tq/iac-apply skip"},
		{Tool: "Bash", Command: "tq mode edit"},
		{Tool: "Bash", Command: "cd x && tq.exe mode set custom --base relaxed"},
		{Tool: "PowerShell", Command: `& 'C:\Users\u\bin\tq.exe' mode set relaxed`},
		{Tool: "Bash", Command: "tq allow --mode acme relaxed"},
		{Tool: "Bash", Command: "tq allow acme --mode custom"},
		{Tool: "Bash", Command: "tq allow --bypass acme"},
		{Tool: "Bash", Command: "tq allow --mode --global relaxed"},
		{Tool: "Bash", Command: "tq unattended on"},
		{Tool: "Bash", Command: "tq unattended off --ws acme"},
		{Tool: "Bash", Command: `bash -c "tq mode set relaxed"`},
		{Tool: "Bash", Command: "sudo tq mode set relaxed"},
		{Tool: "Bash", Command: "/home/u/go/bin/tq mode set strict"},
	})
	evalCorpus(t, id, Allow, []ToolCall{
		{Tool: "Bash", Command: "tq mode"},
		{Tool: "Bash", Command: "tq mode --json"},
		{Tool: "Bash", Command: "tq mode log --since 24h"},
		{Tool: "Bash", Command: "tq allow acme"},
		{Tool: "Bash", Command: "tq unattended status"},
		{Tool: "Bash", Command: `git commit -m "docs: tq mode set relaxed"`},
		{Tool: "Bash", Command: "echo tq mode set relaxed"},
		{Tool: "Bash", Command: "cat <<'EOF' > notes.md\ntq mode set relaxed\nEOF"},
	})
}
