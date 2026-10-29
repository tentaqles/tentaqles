package trust

import (
	"os"
	"testing"
)

func TestGrantRoundTripAndDeny(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	h := "abc123"
	if g, err := LoadGrant(h); g != nil || err != nil {
		t.Fatalf("no grant yet: %v %v", g, err)
	}
	want := Grant{Mode: "custom", Table: map[string]string{"db-destructive": "skip"}, Overrides: map[string]string{"tq/iac-apply": "ask"}}
	if err := SaveGrant(h, want); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGrant(h)
	if err != nil || g == nil || g.Mode != "custom" || g.Table["db-destructive"] != "skip" || g.Overrides["tq/iac-apply"] != "ask" || g.GrantedAt.IsZero() {
		t.Fatalf("round trip: %+v %v", g, err)
	}
	if err := Allow(h); err != nil {
		t.Fatal(err)
	}
	if err := Deny(h); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(GrantPath(h)); !os.IsNotExist(err) {
		t.Fatalf("tq deny must remove the .mode grant: %v", err)
	}
}

func TestGrantInvalidAndDelete(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	if err := SaveGrant(GlobalGrantKey, Grant{}); err == nil {
		t.Fatal("a grant without mode must not be saved")
	}
	if err := SaveGrant(GlobalGrantKey, Grant{Mode: "relaxed", Digest: "d"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GrantPath("bad"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if g, err := LoadGrant("bad"); g != nil || err == nil {
		t.Fatalf("invalid grant must be an error: %v %v", g, err)
	}
	if err := DeleteGrant(GlobalGrantKey); err != nil {
		t.Fatal(err)
	}
	if err := DeleteGrant(GlobalGrantKey); err != nil {
		t.Fatal("deleting a missing grant is not an error")
	}
}

func TestLegacyBypassReadsAsRelaxedGrant(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	h := "h1"
	if err := AllowBypass(h); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGrant(h)
	if err != nil || g == nil || g.Mode != "relaxed" || !g.FromBypass {
		t.Fatalf("a .bypass file is a relaxed grant: %+v %v", g, err)
	}
	if err := SaveGrant(h, Grant{Mode: "custom", Workspace: "acme"}); err != nil {
		t.Fatal(err)
	}
	if g, _ := LoadGrant(h); g.Mode != "custom" || g.FromBypass {
		t.Fatalf("the .mode file wins: %+v", g)
	}
	if err := AllowBypass(GlobalGrantKey); err != nil {
		t.Fatal(err)
	}
	if g, _ := LoadGrant(GlobalGrantKey); g != nil {
		t.Fatalf("the global grant has no legacy form: %+v", g)
	}
}

func TestBypassAllowedFollowsTheGrant(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	if IsBypassAllowed("h2") {
		t.Fatal("nothing granted")
	}
	if err := SaveGrant("h2", Grant{Mode: "relaxed"}); err != nil {
		t.Fatal(err)
	}
	if !IsBypassAllowed("h2") {
		t.Fatal("tq allow --bypass is tq allow --mode relaxed: a grant allows bypass")
	}
}

func TestGrantsForWorkspace(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	_ = SaveGrant("old", Grant{Mode: "relaxed", Workspace: "acme"})
	_ = SaveGrant("other", Grant{Mode: "relaxed", Workspace: "beta"})
	_ = SaveGrant(GlobalGrantKey, Grant{Mode: "relaxed", Digest: "d"})
	_ = os.WriteFile(GrantPath("bad"), []byte("{"), 0o600)
	got := GrantsFor("acme")
	if len(got) != 1 || got[0].Key != "old" || got[0].Grant.Mode != "relaxed" {
		t.Fatalf("got %+v", got)
	}
	if len(GrantsFor("")) != 0 {
		t.Fatal("an empty name matches nothing")
	}
}
