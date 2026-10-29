package cmd

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
)

// Final review, correctness m4: tq guard list shows each pack decision
// guard's own default (ask, deny, or the pack policy), not deny for all.
// The data and ml guards are linked into the tq binary, not into this
// test; their own packages pin Default.
func TestPackGuardDefaults(t *testing.T) {
	want := map[string]string{
		"tq/iac-apply-env":      "policy",
		"tq/db-migrate-env":     "policy",
		"tq/iac-identity-drift": "deny",
		"tq/restore-target":     "deny",
	}
	got := map[string]string{}
	for _, r := range knownGuardRules(nil, guardcfg.File{}) {
		got[r.ID] = string(r.Action)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: default %q, want %q", id, got[id], w)
		}
	}
}
