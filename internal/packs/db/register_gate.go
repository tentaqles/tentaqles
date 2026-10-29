package db

// Workstream D-DG (spec D §13, §14): the database environment gate and the
// restore drill.
//
// Keys:     environments (envgate.DB: match keys of the cloud pack plus
//           hosts, project_refs and servers; migrate in place of apply),
//           drill.
// Merge:    ruling S1: a repo file may add an environment with its own match
//           (environments.*.match TrustedAdd, no Union on match.*); migrate
//           is Ordered with the strictest Default (deny); approval_ttl is
//           Min; require_merged is Or. drill is trusted-layer only: its
//           source names what tq reads and a command source is run.
// Check:    db/restore-drill (built-in), when a drill is declared and a
//           migration changed (decided at spec review; otherwise doctor
//           warns).
// Commands: tq pack db drill, tq pack db approve (ask only).
// Guards:   tq/db-migrate-env and tq/restore-target, registered from
//           internal/cmd/claudehook_db.go; bodies in internal/packs/db/gate.

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/drill"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
	"gopkg.in/yaml.v3"
)

func init() {
	merge := envgate.DB.MergeRules()
	merge["drill"] = packs.MergeRule{} // trusted layer only
	packs.RegisterPack(packs.Pack{
		Name: "db",
		ConfigKeys: map[string]func(yaml.Node) error{
			"environments": envgate.DB.Validate,
			"drill":        drill.Validate,
		},
		Merge:    merge,
		Commands: func() []*cobra.Command { return commands() },
	})
	packs.Register(packs.Check{ID: drill.CheckID, Pack: "db", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool {
			ds, err := drill.Parse(in.Settings())
			// an invalid block applies, so the check reports it
			return (err != nil || len(ds) > 0) && anyMigrationChanged(in, "")
		},
		Run: func(_ context.Context, in *packs.Input) packs.Result { return drill.RunCheck(in) },
	})
}
