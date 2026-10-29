package data

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/packs"
)

// dbtDoctor is the dbt part of the data pack's doctor probes (spec C §7):
// data-dbt-state-missing when dbt.build is dev and dbt.state holds no
// manifest.json.
func dbtDoctor(in *packs.Input) []doctor.Finding {
	cfg := dbtSettings(in.Settings())
	if cfg.Build != "dev" {
		return nil
	}
	fix := "export the production manifest.json (dbt parse or a prod run's target/) to the dir named by packs.data.dbt.state"
	if cfg.State == "" {
		return []doctor.Finding{{Level: "warn", Code: "data-dbt-state-missing",
			Msg: "packs.data.dbt.build is dev but dbt.state is not set", Fix: fix}}
	}
	if in.Root == "" {
		return nil
	}
	p := filepath.Join(in.Root, filepath.FromSlash(cfg.State), "manifest.json")
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return []doctor.Finding{{Level: "warn", Code: "data-dbt-state-missing",
			Msg: fmt.Sprintf("packs.data.dbt.build is dev but %s/manifest.json is missing", cfg.State), Fix: fix}}
	}
	return nil
}
