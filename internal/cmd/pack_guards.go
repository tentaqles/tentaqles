package cmd

// The db and cloud packs' decision guards, registered so tq guard and tq
// pack list them (packs.Guards()). The hook does not run pack guards; the
// guards go with the packs.

import (
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/packs"
	cloudgate "github.com/tentaqles/tentaqles/internal/packs/cloud/gate"
	dbgate "github.com/tentaqles/tentaqles/internal/packs/db/gate"
)

func init() {
	packs.RegisterGuard(packs.Guard{ID: dbgate.IDMigrate, Category: guardcfg.CatDBDestructive, Pack: "db", Events: dbgate.Events, Supersedes: dbgate.Supersedes, Check: dbgate.MigrateEnv})
	packs.RegisterGuard(packs.Guard{ID: dbgate.IDRestore, Category: guardcfg.CatDBDestructive, Default: "deny", Pack: "db", Events: dbgate.Events, Check: dbgate.RestoreTarget})
	packs.RegisterGuard(packs.Guard{ID: cloudgate.IDApplyEnv, Category: guardcfg.CatCloudDestructive, Pack: "cloud", Events: cloudgate.Events, Supersedes: cloudgate.Supersedes, Check: cloudgate.ApplyEnv})
	packs.RegisterGuard(packs.Guard{ID: cloudgate.IDDrift, Category: guardcfg.CatIdentity, Default: "deny", Pack: "cloud", Events: cloudgate.Events, Check: cloudgate.IdentityDrift})
}
