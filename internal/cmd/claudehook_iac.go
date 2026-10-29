package cmd

// The cloud pack's decision guards (spec D §16): the apply and destroy gate
// and the identity drift check. The bodies live in
// internal/packs/cloud/gate; the framework's generic wiring in
// claudehook_policy.go runs them for workspaces with packs.cloud enabled.

import (
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/gate"
)

func init() {
	packs.RegisterGuard(packs.Guard{ID: gate.IDApplyEnv, Pack: "cloud", Events: gate.Events, Supersedes: gate.Supersedes, Check: gate.ApplyEnv})
	packs.RegisterGuard(packs.Guard{ID: gate.IDDrift, Pack: "cloud", Events: gate.Events, Check: gate.IdentityDrift})
}
