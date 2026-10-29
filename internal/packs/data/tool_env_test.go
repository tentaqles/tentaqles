package data

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// toolrun passes a filtered environment: every external data check names
// the variables its tool needs, and a dbt run adds the profile's env_var()
// names (names only).
func TestExternalChecksDeclareEnv(t *testing.T) {
	for _, c := range packs.Checks("data") {
		if c.Kind == packs.External && len(c.Env) == 0 {
			t.Errorf("%s runs %s with no Check.Env", c.ID, c.Tool)
		}
	}
	if len(estimateEnv) != len(costEngines) {
		t.Fatalf("every engine has an estimate env: %v", estimateEnv)
	}
}

func TestDBTRunEnvReadsProfileNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("DBT_PROFILES_DIR", "")
	dir := t.TempDir()
	write := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "dbt_project.yml"), "vars:\n  day: \"{{ env_var('ACME_DAY', '1') }}\"\n")
	write(filepath.Join(home, ".dbt", "profiles.yml"), "acme:\n  outputs:\n    dev:\n      user: \"{{ env_var(\"ACME_DB_USER\") }}\"\n      password: \"{{ env_var('DBT_ENV_SECRET_ACME_PW') }}\"\n")
	if got := dbtRunEnv(dir); !reflect.DeepEqual(got, []string{"ACME_DAY", "ACME_DB_USER", "DBT_ENV_SECRET_ACME_PW"}) {
		t.Fatalf("%v", got)
	}
}
