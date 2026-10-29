package policy

import "testing"

// Every static pack rule names its pack and compiles; Builtin carries them.
func TestPackRulesCarryTheirPack(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range Builtin() {
		ids[r.ID+"\x00"+r.Pack] = true
	}
	for _, r := range PackRules() {
		if r.Pack == "" {
			t.Fatalf("%s: a pack rule needs Pack", r.ID)
		}
		if !ids[r.ID+"\x00"+r.Pack] {
			t.Fatalf("%s (%s) is not in Builtin()", r.ID, r.Pack)
		}
		rr := r
		if err := rr.Compile(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestForPacksDropsDisabledPacks(t *testing.T) {
	rules := []Rule{{ID: "core"}, {ID: "tq/x", Pack: "cloud"}, {ID: "tq/x", Pack: "db"}}
	got := ForPacks(rules, func(p string) bool { return p == "db" })
	if len(got) != 2 || got[0].ID != "core" || got[1].Pack != "db" {
		t.Fatalf("%+v", got)
	}
	if got := ForPacks(rules, nil); len(got) != 1 {
		t.Fatalf("nil enabled keeps core only: %+v", got)
	}
}

// The framework's core rules: active with no pack enabled.
func TestFrameworkRules(t *testing.T) {
	set, errs := Build(Layers{Builtin: ForPacks(Builtin(), nil)})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	for _, c := range []struct {
		call ToolCall
		id   string
		want Action
	}{
		{ToolCall{Tool: "Write", Path: "C:/r/tq-packs.yaml", Content: "data: {}"}, "tq/pack-config-change", Ask},
		{ToolCall{Tool: "Edit", Path: "repo/.tq/sqlfluff-baseline.json"}, "tq/pack-config-change", Ask},
		{ToolCall{Tool: "Bash", Command: "echo 'cloud: {}' > tq-packs.yaml"}, "tq/pack-config-change", Ask},
		{ToolCall{Tool: "Bash", Command: "mkfifo tq-packs.yaml"}, "tq/pack-config-change", Ask},
		{ToolCall{Tool: "Edit", Path: "C:/ws/acme/.tentaqles.yaml", Content: "packs:\n  data: { enabled: true }"}, "tq/pack-config-change", Ask},
		{ToolCall{Tool: "Write", Path: "C:/Users/tester/.tentaqles/packs/acme/ml/ledger.jsonl", Content: "{}"}, "tq/hub-store-write", Deny},
		{ToolCall{Tool: "Bash", Command: `echo '{"event":"answered"}' >> ~/.tentaqles/packs/acme/ml/ledger.jsonl`}, "tq/hub-store-write", Deny},
		{ToolCall{Tool: "Bash", Command: `rm -rf $TQ_HOME/packs/acme/ml/claims`}, "tq/hub-store-write", Deny},
		{ToolCall{Tool: "PowerShell", Command: `Remove-Item $env:TQ_HOME/packs/acme/ml/claims/d1`}, "tq/hub-store-write", Deny},
		{ToolCall{Tool: "Bash", Command: `sqlite3 ~/.tentaqles/hub/acme/hub.db "update decisions set status='answered'"`}, "tq/hub-store-write", Deny},
	} {
		d := set.Evaluate(c.call)
		hit := false
		for _, r := range d.Matched {
			if r.ID == c.id {
				hit = true
			}
		}
		if d.Action != c.want || !hit {
			t.Errorf("%+v: %s %v", c.call, d.Action, d.Matched)
		}
	}
	for _, ok := range []ToolCall{
		{Tool: "Bash", Command: "cat tq-packs.yaml"},
		{Tool: "Bash", Command: "tq pack approvals --json"},
		{Tool: "Read", Path: "C:/Users/tester/.tentaqles/packs/acme/ml/ledger.jsonl"},
	} {
		if d := set.Evaluate(ok); d.Action != Allow {
			t.Errorf("%+v: %s %v", ok, d.Action, d.Matched[0].ID)
		}
	}
}
