package guardcfg

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/policy"
)

func TestCategoriesClassifyEveryBuiltinAndAreValid(t *testing.T) {
	for id := range builtinIDs() {
		if _, ok := Categories[id]; !ok {
			t.Errorf("built-in rule %s has no category in guardcfg.Categories", id)
		}
	}
	for id, c := range Categories {
		if _, ok := ParseCategory(string(c)); !ok {
			t.Errorf("%s: unknown category %q", id, c)
		}
	}
	for _, id := range IdentityGuardIDs {
		if Categories[id] != CatIdentity {
			t.Errorf("%s must be identity", id)
		}
	}
	if len(AllCategories()) != 13 {
		t.Fatalf("want 13 categories, got %d", len(AllCategories()))
	}
	for _, c := range LockedCategories() {
		if !Locked(c) {
			t.Errorf("%s must be locked", c)
		}
	}
	for _, id := range []string{"tq/commit-pii", "tq/push-pii", "tq/text-pii", "tq/pii-fixture-dump"} {
		if Categories[id] != CatPrivacy {
			t.Errorf("%s must be privacy (locked)", id)
		}
	}
	if got := LockedIDs(); len(got[CatPrivacy]) != 4 || len(got[CatGitHistory]) != 0 {
		t.Fatalf("LockedIDs: %v", got)
	}
}

func TestCategoryOf(t *testing.T) {
	if c := CategoryOf(&policy.Rule{ID: "tq/force-push", Category: "hygiene"}); c != CatGitHistory {
		t.Fatalf("built-in id wins over a declared category: %q", c)
	}
	if c := CategoryOf(&policy.Rule{ID: "acme/x", Category: "db-destructive"}); c != CatDBDestructive {
		t.Fatalf("custom rule: %q", c)
	}
	if c := CategoryOf(&policy.Rule{ID: "acme/y", Category: "nope"}); c != "" {
		t.Fatalf("unknown declared category is unclassified: %q", c)
	}
}

func TestPresetsMatchSpecTable(t *testing.T) {
	want := map[string]map[Category]Value{
		ModeDefault: {},
		ModeRelaxed: {CatGitHistory: ValueAsk, CatSystem: ValueAsk, CatProdChanges: ValueSkip, CatCloudDestructive: ValueSkip, CatDBDestructive: ValueSkip, CatExternalWrites: ValueSkip, CatCISupplyChain: ValueSkip, CatHygiene: ValueSkip, CatPacks: ValueSkip},
		ModeStrict:  {CatProdChanges: ValueDeny, CatCloudDestructive: ValueDeny, CatGitHistory: ValueDeny, CatExternalWrites: ValueAsk},
	}
	for name, diff := range want {
		tb, ok := Preset(name)
		if !ok || len(tb) != 13 {
			t.Fatalf("%s: %v %d", name, ok, len(tb))
		}
		for _, c := range AllCategories() {
			exp := ValueRule
			if v, ok := diff[c]; ok {
				exp = v
			}
			if tb.Get(c) != exp {
				t.Errorf("%s/%s = %s, want %s", name, c, tb.Get(c), exp)
			}
			if Locked(c) && tb.Get(c) != ValueRule {
				t.Errorf("%s: locked %s must be rule", name, c)
			}
		}
	}
	if _, ok := Preset(ModeOff); ok {
		t.Fatal("off is not a preset")
	}
	if _, ok := Preset(ModeCustom); ok {
		t.Fatal("custom is not a preset")
	}
	tb, _ := Preset(ModeRelaxed)
	tb[CatSystem] = ValueSkip
	if again, _ := Preset(ModeRelaxed); again[CatSystem] != ValueAsk {
		t.Fatal("Preset must return a copy")
	}
	if ValidStoredMode(ModeOff) || !ValidStoredMode(ModeCustom) || ValidBase(ModeCustom) || !ValidBase(ModeStrict) {
		t.Fatal("mode validity")
	}
}

func TestValueApply(t *testing.T) {
	cases := []struct {
		v    Value
		own  policy.Action
		want policy.Action
	}{
		{ValueSkip, policy.Deny, policy.Allow},
		{ValueAsk, policy.Deny, policy.Ask},
		{ValueDeny, policy.Ask, policy.Deny},
		{ValueRule, policy.Deny, policy.Deny},
		{"", policy.Ask, policy.Ask},
	}
	for _, c := range cases {
		if got := c.v.Apply(c.own); got != c.want {
			t.Errorf("%q.Apply(%q) = %q, want %q", c.v, c.own, got, c.want)
		}
	}
	if _, ok := ParseValue("allow"); ok {
		t.Fatal("allow is not a value")
	}
}

// The interface-commit stubs: Resolve is default, Effective is the rule's own
// action. WS1 replaces both; these pin the stub contract until then.
func TestResolveStubIsDefault(t *testing.T) {
	r := Resolve(nil, "", "", "", "", File{}, RepoFile{}, "", nil)
	if r.Mode != ModeDefault || !r.IsDefault() || r.LooserThanDefault() {
		t.Fatalf("stub: %+v", r)
	}
	rule := &policy.Rule{ID: "tq/force-push-main", Action: policy.Deny}
	if got := r.Effective(rule); got != policy.Deny {
		t.Fatalf("default keeps the rule's own action: %q", got)
	}
	r.Overrides = map[string]Value{"tq/iac-apply": ValueAsk}
	if r.IsDefault() {
		t.Fatal("overrides are not default")
	}
}
