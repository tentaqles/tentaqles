package policy

import "testing"

// Review round 1 for tq/hub-store-write: destinations followed by flags,
// redirects or a comment; path-prefixed verbs; more writers; quoted
// destinations with spaces; `//` and `..` in a file-tool path.
func TestHubStoreWriteRound1(t *testing.T) {
	const id = "tq/hub-store-write"
	cases := []struct {
		name string
		call ToolCall
		deny bool
	}{
		// Major: a destination followed by a flag, a redirect or a comment.
		{"cp dest then 2>/dev/null", ToolCall{Tool: "Bash", Command: "cp forged.jsonl ~/.tentaqles/packs/acme/cloud/approvals.jsonl 2>/dev/null"}, true},
		{"cp dest then 2>&1", ToolCall{Tool: "Bash", Command: "cp forged.jsonl ~/.tentaqles/packs/acme/cloud/approvals.jsonl 2>&1"}, true},
		{"copy-item dest then -Force", ToolCall{Tool: "PowerShell", Command: `Copy-Item .\x.json $env:TQ_HOME\packs\acme\cloud\approvals.jsonl -Force`}, true},
		{"move-item dest then -Force", ToolCall{Tool: "PowerShell", Command: `Move-Item .\x.json $HOME\.tentaqles\packs\acme\x.json -Force`}, true},
		{"mv dest then -f", ToolCall{Tool: "Bash", Command: "mv x ~/.tentaqles/hub/acme/board.db -f"}, true},
		{"rsync dest then --delete", ToolCall{Tool: "Bash", Command: "rsync -a x/ ~/.tentaqles/hub/acme/ --delete"}, true},
		{"cp dest then comment", ToolCall{Tool: "Bash", Command: "cp x $HOME/.tentaqles/packs/acme/a # done"}, true},
		{"cp dest then && ls", ToolCall{Tool: "Bash", Command: "cp x ~/.tentaqles/packs/acme/a -f && ls"}, true},
		{"cp into packs root then flag", ToolCall{Tool: "Bash", Command: "cp x ~/.tentaqles/packs -r"}, true},
		{"cp quoted dest then flag", ToolCall{Tool: "Bash", Command: `cp x "$TQ_HOME/packs/acme/a" -f`}, true},
		{"cp out of store then flag ok", ToolCall{Tool: "Bash", Command: "cp ~/.tentaqles/hub/acme/board.db /tmp/b.db -f"}, false},
		{"cp out of store 2>&1 ok", ToolCall{Tool: "Bash", Command: "cp ~/.tentaqles/hub/acme/board.db /tmp/b.db 2>&1"}, false},
		{"cp out of packs root ok", ToolCall{Tool: "Bash", Command: "cp -r ~/.tentaqles/packs /tmp/backup"}, false},
		{"copy-item out of store -Force ok", ToolCall{Tool: "PowerShell", Command: `Copy-Item $HOME\.tentaqles\hub\acme\board.db C:\tmp\b.db -Force`}, false},
		// Minor: path-prefixed verbs.
		{"/bin/cp into store", ToolCall{Tool: "Bash", Command: "/bin/cp x ~/.tentaqles/packs/acme/a"}, true},
		{"/usr/bin/tee into store", ToolCall{Tool: "Bash", Command: "echo x | /usr/bin/tee ~/.tentaqles/hub/acme/a"}, true},
		{"windows exe path rm", ToolCall{Tool: "PowerShell", Command: `C:\tools\rm.exe -f $HOME\.tentaqles\hub\acme\a`}, true},
		{"path segment named like a verb ok", ToolCall{Tool: "Bash", Command: "cat ~/.tentaqles/packs/acme/cloud/plans/rm.json"}, false},
		// Minor: more writers.
		{"curl -o", ToolCall{Tool: "Bash", Command: "curl -sSL https://example.test/x -o ~/.tentaqles/packs/acme/a"}, true},
		{"curl --output", ToolCall{Tool: "Bash", Command: "curl --output=$TQ_HOME/hub/acme/a https://example.test/x"}, true},
		{"wget -O", ToolCall{Tool: "Bash", Command: "wget -O ~/.tentaqles/hub/acme/board.db https://example.test/x"}, true},
		{"wget -P", ToolCall{Tool: "Bash", Command: "wget -P ~/.tentaqles/packs/acme https://example.test/x"}, true},
		{"invoke-webrequest -OutFile", ToolCall{Tool: "PowerShell", Command: `Invoke-WebRequest https://example.test/x -OutFile $env:TQ_HOME\packs\acme\a`}, true},
		{"iwr -o", ToolCall{Tool: "PowerShell", Command: `iwr https://example.test/x -o $HOME\.tentaqles\hub\acme\a`}, true},
		{"tar -C", ToolCall{Tool: "Bash", Command: "tar -xzf x.tgz -C ~/.tentaqles/packs/acme"}, true},
		{"tar --directory", ToolCall{Tool: "Bash", Command: "tar xf x.tar --directory=$TQ_HOME/hub/acme"}, true},
		{"unzip -d", ToolCall{Tool: "Bash", Command: "unzip -o x.zip -d ~/.tentaqles/hub/acme"}, true},
		{"expand-archive -DestinationPath", ToolCall{Tool: "PowerShell", Command: `Expand-Archive x.zip -DestinationPath $HOME\.tentaqles\packs\acme -Force`}, true},
		{"expand-archive positional", ToolCall{Tool: "PowerShell", Command: `Expand-Archive x.zip $HOME\.tentaqles\packs\acme`}, true},
		{"perl -pi", ToolCall{Tool: "Bash", Command: "perl -pi -e 's/a/b/' ~/.tentaqles/packs/acme/cloud/plans/p1.json"}, true},
		{"python -c open append", ToolCall{Tool: "Bash", Command: `python -c "import os; open(os.path.expanduser('~/.tentaqles/packs/acme/a'),'a').write('x'); print(1)"`}, true},
		{"python3 -c single quoted", ToolCall{Tool: "Bash", Command: `python3 -c 'import json; json.dump({}, open("/home/tester/.tentaqles/hub/acme/a","w"))'`}, true},
		{"node -e write", ToolCall{Tool: "Bash", Command: `node -e "require('fs').writeFileSync(process.env.HOME+'/.tentaqles/packs/acme/a','x')"`}, true},
		{"IO.File WriteAllText", ToolCall{Tool: "PowerShell", Command: `[IO.File]::WriteAllText("$HOME\.tentaqles\packs\acme\a", "x")`}, true},
		{"System.IO.File AppendAllText", ToolCall{Tool: "PowerShell", Command: `[System.IO.File]::AppendAllText('C:\Users\tester\.tentaqles\hub\acme\a', 'x')`}, true},
		{"python -c elsewhere ok", ToolCall{Tool: "Bash", Command: `python -c "import os; open('/tmp/a','w').write('x')"`}, false},
		{"curl out of store upload elsewhere ok", ToolCall{Tool: "Bash", Command: "curl -o /tmp/x https://example.test/x"}, false},
		{"tar create elsewhere ok", ToolCall{Tool: "Bash", Command: "tar czf /tmp/backup.tgz ~/.tentaqles/hub/acme"}, false},
		{"IO.File ReadAllText ok", ToolCall{Tool: "PowerShell", Command: `[IO.File]::ReadAllText("$HOME\.tentaqles\hub\acme\serve.json")`}, false},
		// Minor: quoted destination with spaces.
		{"cp quoted dest with spaces", ToolCall{Tool: "PowerShell", Command: `cp x "C:\Users\a tester\.tentaqles\packs\acme\a"`}, true},
		{"redirect quoted with spaces", ToolCall{Tool: "Bash", Command: `echo x > "/home/a tester/.tentaqles/hub/acme/a"`}, true},
		{"cp quoted source with spaces ok", ToolCall{Tool: "PowerShell", Command: `cp "C:\Users\a tester\.tentaqles\hub\acme\b.db" C:\tmp\b.db`}, false},
		// Minor: file-tool path spelling.
		{"write double slash", ToolCall{Tool: "Write", Path: "/home/tester/.tentaqles//packs/acme/a"}, true},
		{"write dot-dot", ToolCall{Tool: "Write", Path: "/home/tester/.tentaqles/x/../packs/acme/a"}, true},
		{"write backslash double", ToolCall{Tool: "Write", Path: `C:\Users\tester\.tentaqles\\hub\acme\a`}, true},
		{"write tq config ok", ToolCall{Tool: "Write", Path: "/home/tester/.tentaqles/identities/acme/x.yaml"}, false},
	}
	for _, packs := range [][]string{{"cloud"}, {"db"}} {
		s := hubStoreSet(t, packs...)
		for _, c := range cases {
			d := s.Evaluate(c.call)
			got := false
			for _, r := range d.Matched {
				if r.ID == id {
					got = true
				}
			}
			if got != c.deny {
				t.Errorf("%v %s: matched %v, want %v", packs, c.name, got, c.deny)
			}
		}
	}
}
