package cmd

// The db pack's decision guards (spec D §14, §16): the database environment
// gate and the restore target rule. The bodies live in
// internal/packs/db/gate; the framework's generic wiring in
// claudehook_policy.go runs them for workspaces with packs.db enabled.

import (
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/gate"
)

func init() {
	packs.RegisterGuard(packs.Guard{ID: gate.IDMigrate, Pack: "db", Events: gate.Events, Supersedes: gate.Supersedes, Check: gate.MigrateEnv})
	packs.RegisterGuard(packs.Guard{ID: gate.IDRestore, Default: "deny", Pack: "db", Events: gate.Events, Check: gate.RestoreTarget})
}
