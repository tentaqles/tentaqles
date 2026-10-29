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
