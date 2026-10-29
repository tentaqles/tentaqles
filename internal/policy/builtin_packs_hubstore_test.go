package policy

import "testing"

// hubStoreSet builds D-GR's pack rules with only the given packs enabled.
// The framework's core tq/hub-store-write (builtin_framework.go, always
// active, no Pack) is broader: it also denies a copy out of the store, and
// the hook-time state-dir check denies any shell mention of hub/ or packs/.
// These tests pin D-GR's own, destination-precise block, so they leave the
// core rules out; TestHubStoreWriteCoreCoversPackRules checks the overlap.
func hubStoreSet(t *testing.T, packs ...string) *Set {
	t.Helper()
	on := map[string]bool{}
	for _, p := range packs {
		on[p] = true
	}
	s, errs := Build(Layers{Builtin: ForPacks(PackRules(), func(p string) bool { return on[p] })})
	if len(errs) > 0 {
		t.Fatalf("builtin rules do not compile: %v", errs)
	}
	return s
}

// tq/hub-store-write (spec D §16): a shell write or an Edit/Write into
// $TQ_HOME/hub/** or $TQ_HOME/packs/** is denied while cloud or db is on.
func TestHubStoreWrite(t *testing.T) {
	const id = "tq/hub-store-write"
	cases := []struct {
		name string
		call ToolCall
		deny bool
	}{
		// file tools
		{"write board db", ToolCall{Tool: "Write", Path: `C:\Users\tester\.tentaqles\hub\acme\board.db`}, true},
		{"edit approval ledger", ToolCall{Tool: "Edit", Path: "/home/tester/.tentaqles/packs/acme/cloud/approvals.jsonl"}, true},
		{"multiedit plan", ToolCall{Tool: "MultiEdit", Path: "/home/tester/.tentaqles/packs/acme/cloud/plans/p1.json"}, true},
		{"notebook in hub", ToolCall{Tool: "NotebookEdit", Path: "/home/tester/.tentaqles/hub/acme/x.ipynb"}, true},
		{"read board ok (not a write)", ToolCall{Tool: "Read", Path: "/home/tester/.tentaqles/hub/acme/board.db"}, false},
		{"write other tq file ok", ToolCall{Tool: "Write", Path: "/home/tester/.tentaqles/config.yaml"}, false},
		{"write hubble dir ok", ToolCall{Tool: "Write", Path: "/home/tester/.tentaqles/hubble/x"}, false},
		{"write repo file ok", ToolCall{Tool: "Write", Path: "/r/acme/internal/hub/store.go"}, false},
		{"write .tentaqles.yaml ok", ToolCall{Tool: "Write", Path: "/r/acme/.tentaqles.yaml"}, false},
		// sqlite3
		{"sqlite3 board", ToolCall{Tool: "Bash", Command: `sqlite3 ~/.tentaqles/hub/acme/board.db "update tasks set state='done'"`}, true},
		{"sqlite3 tq home var", ToolCall{Tool: "Bash", Command: `sqlite3 "$TQ_HOME/hub/acme/board.db" .dump`}, true},
		{"sqlite3.exe ps", ToolCall{Tool: "PowerShell", Command: `sqlite3.exe $env:TQ_HOME\packs\acme\db\ledger.db "delete from x"`}, true},
		{"python sqlite3 connect", ToolCall{Tool: "Bash", Command: `python -c "import sqlite3; sqlite3.connect('/home/tester/.tentaqles/hub/acme/board.db')"`}, true},
		{"sqlite3 elsewhere ok", ToolCall{Tool: "Bash", Command: "sqlite3 app.db .tables"}, false},
		// redirects
		{"redirect into ledger", ToolCall{Tool: "Bash", Command: `echo '{}' >> ~/.tentaqles/packs/acme/cloud/approvals.jsonl`}, true},
		{"redirect braced var", ToolCall{Tool: "Bash", Command: `printf x > "${TQ_HOME}/packs/acme/cloud/plans/p1.json"`}, true},
		{"redirect windows path", ToolCall{Tool: "PowerShell", Command: `"x" > C:\Users\tester\.tentaqles\hub\acme\board.db`}, true},
		{"redirect cmd var", ToolCall{Tool: "PowerShell", Command: `echo x > %TQ_HOME%\hub\acme\a`}, true},
		{"read store redirect elsewhere ok", ToolCall{Tool: "Bash", Command: "cat ~/.tentaqles/packs/acme/cloud/approvals.jsonl > /tmp/out.txt"}, false},
		{"stderr dup ok", ToolCall{Tool: "Bash", Command: "ls ~/.tentaqles/hub 2>&1"}, false},
		// cp / mv destinations
		{"cp into packs", ToolCall{Tool: "Bash", Command: "cp forged.jsonl ~/.tentaqles/packs/acme/cloud/approvals.jsonl"}, true},
		{"mv into hub dir", ToolCall{Tool: "Bash", Command: "mv board.db ~/.tentaqles/hub/acme/"}, true},
		{"cp -t into hub", ToolCall{Tool: "Bash", Command: "cp -t ~/.tentaqles/hub/acme board.db"}, true},
		{"copy-item destination", ToolCall{Tool: "PowerShell", Command: `Copy-Item -Destination $HOME\.tentaqles\packs\acme\ -Path .\x.json`}, true},
		{"move-item positional", ToolCall{Tool: "PowerShell", Command: `Move-Item .\x.json $env:TQ_HOME\packs\acme\cloud\plans\x.json`}, true},
		{"cp out of hub ok", ToolCall{Tool: "Bash", Command: "cp ~/.tentaqles/hub/acme/board.db /tmp/board.db"}, false},
		{"cp then other cmd ok", ToolCall{Tool: "Bash", Command: "cp ~/.tentaqles/hub/acme/board.db /tmp/b.db && ls"}, false},
		// other writers
		{"tee into hub", ToolCall{Tool: "Bash", Command: "echo x | tee -a ~/.tentaqles/hub/acme/notes"}, true},
		{"rm ledger", ToolCall{Tool: "Bash", Command: "rm -f ~/.tentaqles/packs/acme/cloud/approvals.jsonl"}, true},
		{"sed -i plan", ToolCall{Tool: "Bash", Command: "sed -i 's/a/b/' ~/.tentaqles/packs/acme/cloud/plans/p1.json"}, true},
		{"set-content ps", ToolCall{Tool: "PowerShell", Command: `Set-Content -Path $env:TQ_HOME\hub\acme\x -Value y`}, true},
		{"out-file ps", ToolCall{Tool: "PowerShell", Command: `"y" | Out-File C:\Users\tester\.tentaqles\packs\acme\x`}, true},
		{"remove-item ps", ToolCall{Tool: "PowerShell", Command: `Remove-Item -Recurse $HOME\.tentaqles\hub\acme`}, true},
		{"new-item ps", ToolCall{Tool: "PowerShell", Command: `New-Item -ItemType File $HOME\.tentaqles\packs\acme\x`}, true},
		{"ls store ok", ToolCall{Tool: "Bash", Command: "ls -la ~/.tentaqles/packs/acme"}, false},
		{"cat store ok", ToolCall{Tool: "Bash", Command: "cat ~/.tentaqles/hub/acme/serve.json"}, false},
		{"grep -ri store ok", ToolCall{Tool: "Bash", Command: "grep -ri approval ~/.tentaqles/packs/acme"}, false},
		{"docker --rm mount ok", ToolCall{Tool: "Bash", Command: "docker run --rm -v ~/.tentaqles/hub/acme:/data alpine ls /data"}, false},
		{"copy-item from store ok", ToolCall{Tool: "PowerShell", Command: `Copy-Item $HOME\.tentaqles\hub\acme\board.db -Destination C:\tmp\b.db`}, false},
		{"tq hub ok", ToolCall{Tool: "Bash", Command: "tq hub task add 'x'"}, false},
		{"heredoc note mentioning path ok", ToolCall{Tool: "Bash", Command: "cat > /tmp/note.md <<'EOF'\nnever cp into ~/.tentaqles/packs/acme\nEOF"}, false},
		{"heredoc into store denied", ToolCall{Tool: "Bash", Command: "cat > ~/.tentaqles/packs/acme/x <<'EOF'\n{}\nEOF"}, true},
	}
	for _, packs := range [][]string{{"cloud"}, {"db"}, {"cloud", "db"}} {
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
				t.Errorf("%v %s: matched %v, want %v (%s)", packs, c.name, got, c.deny, d.Reason())
			}
			if got && d.Action != Deny {
				t.Errorf("%v %s: action %q, want deny", packs, c.name, d.Action)
			}
		}
	}
}

// With neither pack on, the rule is inactive; each pack carries both forms.
func TestHubStoreWritePerPack(t *testing.T) {
	s := hubStoreSet(t, "data")
	for _, c := range []ToolCall{
		{Tool: "Write", Path: "/home/tester/.tentaqles/hub/acme/board.db"},
		{Tool: "Bash", Command: "sqlite3 ~/.tentaqles/hub/acme/board.db .dump"},
	} {
		for _, r := range s.Evaluate(c).Matched {
			if r.ID == "tq/hub-store-write" {
				t.Fatalf("active with cloud and db off: %+v", c)
			}
		}
	}
	forms := map[string]map[string]bool{}
	for _, r := range PackRules() {
		if r.ID != "tq/hub-store-write" {
			continue
		}
		if r.Action != Deny {
			t.Fatalf("%s (%s) must deny", r.ID, r.Pack)
		}
		if forms[r.Pack] == nil {
			forms[r.Pack] = map[string]bool{}
		}
		forms[r.Pack][map[bool]string{true: "shell", false: "file"}[r.Command != ""]] = true
	}
	for _, p := range []string{"cloud", "db"} {
		if !forms[p]["shell"] || !forms[p]["file"] {
			t.Fatalf("pack %s needs a shell and a file entry: %v", p, forms[p])
		}
	}
	if len(forms) != 2 {
		t.Fatalf("only cloud and db carry it: %v", forms)
	}
}

// The manifest can't switch it off: Build honours Disable, so the hook relies
// on guardcfg dropping the id first. This pins that the rule is not removed
// when the hook passes a Disable list with guardcfg's never-disable ids gone.
func TestHubStoreWriteSurvivesOtherDisables(t *testing.T) {
	s, _ := Build(Layers{Builtin: ForPacks(Builtin(), func(p string) bool { return p == "db" }), Disable: []string{"tq/iac-apply"}})
	d := s.Evaluate(ToolCall{Tool: "Write", Path: "/home/tester/.tentaqles/packs/acme/db/ledger.jsonl"})
	if d.Action != Deny {
		t.Fatalf("want deny, got %q", d.Action)
	}
}

// Integration: every write D-GR's pack rules deny is denied by the core
// rule too, with no pack enabled (the D-GR block is kept, and redundant for
// these forms).
func TestHubStoreWriteCoreCoversPackRules(t *testing.T) {
	core, errs := Build(Layers{Builtin: ForPacks(Builtin(), func(string) bool { return false })})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	for _, c := range []ToolCall{
		{Tool: "Write", Path: "/home/tester/.tentaqles/hub/acme/board.db"},
		{Tool: "Edit", Path: "/home/tester/.tentaqles/packs/acme/cloud/approvals.jsonl"},
		{Tool: "Bash", Command: "cp forged.jsonl ~/.tentaqles/packs/acme/cloud/approvals.jsonl"},
		{Tool: "Bash", Command: "echo '{}' >> ~/.tentaqles/packs/acme/cloud/approvals.jsonl"},
		{Tool: "Bash", Command: "rm -f ~/.tentaqles/packs/acme/cloud/approvals.jsonl"},
		{Tool: "PowerShell", Command: `Set-Content -Path $env:TQ_HOME\hub\acme\x -Value y`},
	} {
		if d := core.Evaluate(c); d.Action != Deny {
			t.Errorf("%+v: core action %q", c, d.Action)
		}
	}
}
