package policy

import "testing"

// tq/hub-store-write is a core rule only: no pack carries an entry for it
// (D-GR's were dropped in integration round 3 as redundant with the core
// rule and the hook-time state-dir check; see cmd's
// TestHubStoreWriteCoreCoversDroppedDGRForms).
func TestHubStoreWriteIsCoreOnly(t *testing.T) {
	for _, r := range PackRules() {
		if r.ID == "tq/hub-store-write" {
			t.Fatalf("a pack entry for the core rule: %+v", r)
		}
	}
	core := 0
	for _, r := range Builtin() {
		if r.ID == "tq/hub-store-write" {
			if r.Pack != "" || r.Action != Deny {
				t.Fatalf("the core rule has no pack and denies: %+v", r)
			}
			core++
		}
	}
	if core != 2 {
		t.Fatalf("want a file and a shell entry, got %d", core)
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

// The core rule's static entries deny the common write forms with no pack
// enabled (the hook-time check covers the rest).
func TestHubStoreWriteCoreStatic(t *testing.T) {
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
