package cmd

import (
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// Integration round 3 (ruling on D-GR): the D-GR pack entries of
// tq/hub-store-write were dropped as redundant. Every write they denied is
// denied with no pack enabled by the core rule: its static entries
// (policy.frameworkRules) or the hook-time state-dir check.
func TestHubStoreWriteCoreCoversDroppedDGRForms(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	core, errs := policy.Build(policy.Layers{Builtin: policy.ForPacks(policy.Builtin(), func(string) bool { return false })})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	for _, c := range []struct {
		name string
		call policy.ToolCall
	}{
		{"write board db", policy.ToolCall{Tool: "Write", Path: `C:\Users\tester\.tentaqles\hub\acme\board.db`}},
		{"edit approval ledger", policy.ToolCall{Tool: "Edit", Path: "/home/tester/.tentaqles/packs/acme/cloud/approvals.jsonl"}},
		{"multiedit plan", policy.ToolCall{Tool: "MultiEdit", Path: "/home/tester/.tentaqles/packs/acme/cloud/plans/p1.json"}},
		{"notebook in hub", policy.ToolCall{Tool: "NotebookEdit", Path: "/home/tester/.tentaqles/hub/acme/x.ipynb"}},
		{"sqlite3 board", policy.ToolCall{Tool: "Bash", Command: `sqlite3 ~/.tentaqles/hub/acme/board.db "update tasks set state='done'"`}},
		{"sqlite3 tq home var", policy.ToolCall{Tool: "Bash", Command: `sqlite3 "$TQ_HOME/hub/acme/board.db" .dump`}},
		{"sqlite3.exe ps", policy.ToolCall{Tool: "PowerShell", Command: `sqlite3.exe $env:TQ_HOME\packs\acme\db\ledger.db "delete from x"`}},
		{"python sqlite3 connect", policy.ToolCall{Tool: "Bash", Command: `python -c "import sqlite3; sqlite3.connect('/home/tester/.tentaqles/hub/acme/board.db')"`}},
		{"redirect into ledger", policy.ToolCall{Tool: "Bash", Command: `echo '{}' >> ~/.tentaqles/packs/acme/cloud/approvals.jsonl`}},
		{"redirect braced var", policy.ToolCall{Tool: "Bash", Command: `printf x > "${TQ_HOME}/packs/acme/cloud/plans/p1.json"`}},
		{"redirect windows path", policy.ToolCall{Tool: "PowerShell", Command: `"x" > C:\Users\tester\.tentaqles\hub\acme\board.db`}},
		{"redirect cmd var", policy.ToolCall{Tool: "PowerShell", Command: `echo x > %TQ_HOME%\hub\acme\a`}},
		{"cp into packs", policy.ToolCall{Tool: "Bash", Command: "cp forged.jsonl ~/.tentaqles/packs/acme/cloud/approvals.jsonl"}},
		{"mv into hub dir", policy.ToolCall{Tool: "Bash", Command: "mv board.db ~/.tentaqles/hub/acme/"}},
		{"cp -t into hub", policy.ToolCall{Tool: "Bash", Command: "cp -t ~/.tentaqles/hub/acme board.db"}},
		{"copy-item destination", policy.ToolCall{Tool: "PowerShell", Command: `Copy-Item -Destination $HOME\.tentaqles\packs\acme\ -Path .\x.json`}},
		{"move-item positional", policy.ToolCall{Tool: "PowerShell", Command: `Move-Item .\x.json $env:TQ_HOME\packs\acme\cloud\plans\x.json`}},
		{"tee into hub", policy.ToolCall{Tool: "Bash", Command: "echo x | tee -a ~/.tentaqles/hub/acme/notes"}},
		{"rm ledger", policy.ToolCall{Tool: "Bash", Command: "rm -f ~/.tentaqles/packs/acme/cloud/approvals.jsonl"}},
		{"sed -i plan", policy.ToolCall{Tool: "Bash", Command: "sed -i 's/a/b/' ~/.tentaqles/packs/acme/cloud/plans/p1.json"}},
		{"set-content ps", policy.ToolCall{Tool: "PowerShell", Command: `Set-Content -Path $env:TQ_HOME\hub\acme\x -Value y`}},
		{"out-file ps", policy.ToolCall{Tool: "PowerShell", Command: `"y" | Out-File C:\Users\tester\.tentaqles\packs\acme\x`}},
		{"remove-item ps", policy.ToolCall{Tool: "PowerShell", Command: `Remove-Item -Recurse $HOME\.tentaqles\hub\acme`}},
		{"new-item ps", policy.ToolCall{Tool: "PowerShell", Command: `New-Item -ItemType File $HOME\.tentaqles\packs\acme\x`}},
		{"heredoc into store denied", policy.ToolCall{Tool: "Bash", Command: "cat > ~/.tentaqles/packs/acme/x <<'EOF'\n{}\nEOF"}},
		{"cp dest then 2>/dev/null", policy.ToolCall{Tool: "Bash", Command: "cp forged.jsonl ~/.tentaqles/packs/acme/cloud/approvals.jsonl 2>/dev/null"}},
		{"cp dest then 2>&1", policy.ToolCall{Tool: "Bash", Command: "cp forged.jsonl ~/.tentaqles/packs/acme/cloud/approvals.jsonl 2>&1"}},
		{"copy-item dest then -Force", policy.ToolCall{Tool: "PowerShell", Command: `Copy-Item .\x.json $env:TQ_HOME\packs\acme\cloud\approvals.jsonl -Force`}},
		{"move-item dest then -Force", policy.ToolCall{Tool: "PowerShell", Command: `Move-Item .\x.json $HOME\.tentaqles\packs\acme\x.json -Force`}},
		{"mv dest then -f", policy.ToolCall{Tool: "Bash", Command: "mv x ~/.tentaqles/hub/acme/board.db -f"}},
		{"rsync dest then --delete", policy.ToolCall{Tool: "Bash", Command: "rsync -a x/ ~/.tentaqles/hub/acme/ --delete"}},
		{"cp dest then comment", policy.ToolCall{Tool: "Bash", Command: "cp x $HOME/.tentaqles/packs/acme/a # done"}},
		{"cp dest then && ls", policy.ToolCall{Tool: "Bash", Command: "cp x ~/.tentaqles/packs/acme/a -f && ls"}},
		{"cp into packs root then flag", policy.ToolCall{Tool: "Bash", Command: "cp x ~/.tentaqles/packs -r"}},
		{"cp quoted dest then flag", policy.ToolCall{Tool: "Bash", Command: `cp x "$TQ_HOME/packs/acme/a" -f`}},
		{"/bin/cp into store", policy.ToolCall{Tool: "Bash", Command: "/bin/cp x ~/.tentaqles/packs/acme/a"}},
		{"/usr/bin/tee into store", policy.ToolCall{Tool: "Bash", Command: "echo x | /usr/bin/tee ~/.tentaqles/hub/acme/a"}},
		{"windows exe path rm", policy.ToolCall{Tool: "PowerShell", Command: `C:\tools\rm.exe -f $HOME\.tentaqles\hub\acme\a`}},
		{"curl -o", policy.ToolCall{Tool: "Bash", Command: "curl -sSL https://example.test/x -o ~/.tentaqles/packs/acme/a"}},
		{"curl --output", policy.ToolCall{Tool: "Bash", Command: "curl --output=$TQ_HOME/hub/acme/a https://example.test/x"}},
		{"wget -O", policy.ToolCall{Tool: "Bash", Command: "wget -O ~/.tentaqles/hub/acme/board.db https://example.test/x"}},
		{"wget -P", policy.ToolCall{Tool: "Bash", Command: "wget -P ~/.tentaqles/packs/acme https://example.test/x"}},
		{"invoke-webrequest -OutFile", policy.ToolCall{Tool: "PowerShell", Command: `Invoke-WebRequest https://example.test/x -OutFile $env:TQ_HOME\packs\acme\a`}},
		{"iwr -o", policy.ToolCall{Tool: "PowerShell", Command: `iwr https://example.test/x -o $HOME\.tentaqles\hub\acme\a`}},
		{"tar -C", policy.ToolCall{Tool: "Bash", Command: "tar -xzf x.tgz -C ~/.tentaqles/packs/acme"}},
		{"tar --directory", policy.ToolCall{Tool: "Bash", Command: "tar xf x.tar --directory=$TQ_HOME/hub/acme"}},
		{"unzip -d", policy.ToolCall{Tool: "Bash", Command: "unzip -o x.zip -d ~/.tentaqles/hub/acme"}},
		{"expand-archive -DestinationPath", policy.ToolCall{Tool: "PowerShell", Command: `Expand-Archive x.zip -DestinationPath $HOME\.tentaqles\packs\acme -Force`}},
		{"expand-archive positional", policy.ToolCall{Tool: "PowerShell", Command: `Expand-Archive x.zip $HOME\.tentaqles\packs\acme`}},
		{"perl -pi", policy.ToolCall{Tool: "Bash", Command: "perl -pi -e 's/a/b/' ~/.tentaqles/packs/acme/cloud/plans/p1.json"}},
		{"python -c open append", policy.ToolCall{Tool: "Bash", Command: `python -c "import os; open(os.path.expanduser('~/.tentaqles/packs/acme/a'),'a').write('x'); print(1)"`}},
		{"python3 -c single quoted", policy.ToolCall{Tool: "Bash", Command: `python3 -c 'import json; json.dump({}, open("/home/tester/.tentaqles/hub/acme/a","w"))'`}},
		{"node -e write", policy.ToolCall{Tool: "Bash", Command: `node -e "require('fs').writeFileSync(process.env.HOME+'/.tentaqles/packs/acme/a','x')"`}},
		{"IO.File WriteAllText", policy.ToolCall{Tool: "PowerShell", Command: `[IO.File]::WriteAllText("$HOME\.tentaqles\packs\acme\a", "x")`}},
		{"System.IO.File AppendAllText", policy.ToolCall{Tool: "PowerShell", Command: `[System.IO.File]::AppendAllText('C:\Users\tester\.tentaqles\hub\acme\a', 'x')`}},
		{"cp quoted dest with spaces", policy.ToolCall{Tool: "PowerShell", Command: `cp x "C:\Users\a tester\.tentaqles\packs\acme\a"`}},
		{"redirect quoted with spaces", policy.ToolCall{Tool: "Bash", Command: `echo x > "/home/a tester/.tentaqles/hub/acme/a"`}},
		{"write double slash", policy.ToolCall{Tool: "Write", Path: "/home/tester/.tentaqles//packs/acme/a"}},
		{"write dot-dot", policy.ToolCall{Tool: "Write", Path: "/home/tester/.tentaqles/x/../packs/acme/a"}},
		{"write backslash double", policy.ToolCall{Tool: "Write", Path: `C:\Users\tester\.tentaqles\\hub\acme\a`}},
	} {
		d := policy.Merge(core.Evaluate(c.call), stateDirDecision(c.call, home))
		hit := false
		for _, r := range d.Matched {
			if r.ID == "tq/hub-store-write" {
				hit = true
			}
		}
		if d.Action != policy.Deny || !hit {
			t.Errorf("%s: %q %v", c.name, d.Action, d.Matched)
		}
	}
}
