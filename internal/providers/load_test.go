package providers

import (
	"strings"
	"testing"
)

// envplan.Desired flattens every enabled provider's variables into one map, so
// two providers claiming the same key means one silently wins and the other
// reads its directory -- isolation that looks real and is not. Several CLIs are
// configurable only through generic variables like XDG_CONFIG_HOME, so this is
// a mistake the catalog invites rather than a theoretical one.
func TestCatalog_RefusesTwoProvidersClaimingTheSameEnvVar(t *testing.T) {
	c := &Catalog{byID: map[string]Provider{
		"alpha": {ID: "alpha", Identity: Identity{Env: map[string]string{"XDG_CONFIG_HOME": "{dir}"}}},
		"beta":  {ID: "beta", Identity: Identity{Env: map[string]string{"XDG_CONFIG_HOME": "{dir}"}}},
	}}
	err := c.checkEnvCollisions()
	if err == nil {
		t.Fatal("a shared identity variable must be refused, not silently resolved")
	}
	for _, want := range []string{"alpha", "beta", "XDG_CONFIG_HOME"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q: %v", want, err)
		}
	}
}

func TestCatalog_DistinctEnvVarsAreFine(t *testing.T) {
	c := &Catalog{byID: map[string]Provider{
		"gh": {ID: "gh", Identity: Identity{Env: map[string]string{"GH_CONFIG_DIR": "{dir}"}}},
		"az": {ID: "az", Identity: Identity{Env: map[string]string{"AZURE_CONFIG_DIR": "{dir}"}}},
		"no": {ID: "no"},
	}}
	if err := c.checkEnvCollisions(); err != nil {
		t.Fatalf("distinct keys must load: %v", err)
	}
}

// The shipped catalog must stay collision-free, so adding a provider that
// steals another's variable fails here rather than in someone's shell.
func TestEmbeddedCatalog_HasNoEnvCollisions(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("the embedded catalog must load: %v", err)
	}
	if err := c.checkEnvCollisions(); err != nil {
		t.Fatal(err)
	}
}
