package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/db/drill"
	"github.com/tentaqles/tentaqles/internal/packs/db/gate"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
)

const dbHookManifest = `guard: { disable: [tq/example-cmd-apply] }
packs:
  db:
    migrations:
      - {path: supabase/migrations, engine: postgres, migrator: supabase}
    environments:
      dev: {match: {dotenv: [.env], hosts: [localhost]}, migrate: auto}
      staging: {match: {dotenv: [.env.staging]}}
      prod: {match: {dotenv: [.env.prod], project_refs: [aaaaaaaaaaaaaaaaaaaa]}, migrate: approve}
`

func dbHook(t *testing.T, ws string) func(string) (int, string) {
	return func(cmd string) (int, string) {
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, cmd))
		return code, out
	}
}

func TestHookDBMigrateEnv(t *testing.T) {
	clearAgentMarkers(t)
	t.Setenv("SUPABASE_PROJECT_ID", "")
	ws := setupTrustedWorkspaceWithManifest(t, "acme", dbHookManifest)
	hook := dbHook(t, ws)
	// auto really allows: the core tq/remote-migration ask is superseded
	if code, out := hook("tq dotenv run --file .env -- npx prisma migrate deploy"); code != 0 || strings.Contains(out, "tq/remote-migration") || strings.Contains(out, `"ask"`) || strings.Contains(out, `"deny"`) {
		t.Fatalf("dev auto: %d %s", code, out)
	}
	if code, out := hook("tq dotenv run --file .env.staging -- npx prisma migrate deploy"); code != 0 || !strings.Contains(out, "tq/db-migrate-env") || !strings.Contains(out, "CONFIRM") {
		t.Fatalf("staging ask: %d %s", code, out)
	}
	if code, out := hook("SUPABASE_PROJECT_ID=aaaaaaaaaaaaaaaaaaaa supabase db push"); code != 2 || !strings.Contains(out, "tq/db-migrate-env") || !strings.Contains(out, "tq pack db approve --env prod") {
		t.Fatalf("prod deny: %d %s", code, out)
	}
	// unresolved: the strictest environment
	if code, out := hook("npx prisma migrate deploy"); code != 2 || !strings.Contains(out, "default environment") {
		t.Fatalf("unresolved: %d %s", code, out)
	}
	// a restore into a non-local host
	if code, out := hook("pg_restore -h db.aaaaaaaaaaaaaaaaaaaa.supabase.co -d postgres acme.dump"); code != 2 || !strings.Contains(out, "tq/restore-target") {
		t.Fatalf("restore: %d %s", code, out)
	}
	if code, out := hook("pg_restore -h localhost -d scratch acme.dump"); code != 0 || strings.Contains(out, "tq/restore-target") {
		t.Fatalf("local restore: %d %s", code, out)
	}
}

func TestHookDBGuardsWithoutEnvironments(t *testing.T) {
	clearAgentMarkers(t)
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "guard: { disable: [tq/example-cmd-apply] }\npacks:\n  db:\n    migrations: [{path: supabase/migrations, engine: postgres}]\n")
	hook := dbHook(t, ws)
	if code, out := hook("supabase db push"); code != 0 || !strings.Contains(out, "tq/remote-migration") || strings.Contains(out, "tq/db-migrate-env") {
		t.Fatalf("no environments: the core ask: %d %s", code, out)
	}
	if code, out := hook("psql -h prod.acme.test -f dump.sql"); code != 2 || !strings.Contains(out, "tq/restore-target") {
		t.Fatalf("restore without environments: %d %s", code, out)
	}
	// without the pack neither guard runs
	ws = setupTrustedWorkspaceWithManifest(t, "acme2", "guard: { disable: [tq/example-cmd-apply] }\n")
	if code, out := dbHook(t, ws)("psql -h prod.acme.test -f dump.sql"); code != 0 || strings.Contains(out, "tq/restore-target") {
		t.Fatalf("no pack: %d %s", code, out)
	}
}

func TestHookDBApprovedMigrationOnce(t *testing.T) {
	clearAgentMarkers(t)
	t.Setenv("SUPABASE_PROJECT_ID", "")
	ws := setupTrustedWorkspaceWithManifest(t, "acme", dbHookManifest)
	os.MkdirAll(filepath.Join(ws, "supabase", "migrations"), 0o755)
	os.WriteFile(filepath.Join(ws, "supabase", "migrations", "20261001000000_orders.sql"), []byte("create table public.orders(id int);\n"), 0o644)
	os.MkdirAll(filepath.Join(ws, "supabase", ".temp"), 0o755)
	os.WriteFile(filepath.Join(ws, "supabase", ".temp", "project-ref"), []byte("aaaaaaaaaaaaaaaaaaaa\n"), 0o644)
	hook := dbHook(t, ws)
	if code, out := hook("supabase db push"); code != 2 {
		t.Fatalf("no approval: %d %s", code, out)
	}
	h, err := gate.MigrationsHash(gate.DiskReader{Root: ws}, []string{"supabase/migrations"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := approval.Ask("acme", approval.Request{Pack: "db", Action: "migrate", Subject: gate.Subject, Target: "prod",
		Evidence: map[string]string{gate.EvMigrations: h, gate.EvDrill: drill.BindingNone}, Summary: "migrate prod", Options: []string{"apply", "reject"}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open("acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ledger.SigningKey("acme")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AnswerPackApproval(id, "apply", "ok", "hub:tester", store.PackSigner(ledger.AnswerSigner("acme", key))); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if code, out := hook("supabase db push"); code != 0 || strings.Contains(out, `"deny"`) || strings.Contains(out, "tq/remote-migration") {
		t.Fatalf("approved: %d %s", code, out)
	}
	if code, out := hook("supabase db push"); code != 2 || !strings.Contains(out, "used") {
		t.Fatalf("second use: %d %s", code, out)
	}
}

// tq pack db approve only asks: an agent may run it; there is no db
// executor, and any `tq pack db ... apply` stays denied as human-only.
func TestHookDBApproveOnlyAsks(t *testing.T) {
	clearAgentMarkers(t)
	ws := setupTrustedWorkspaceWithManifest(t, "acme", dbHookManifest)
	hook := dbHook(t, ws)
	if code, out := hook("tq pack db approve --env prod"); code != 0 || strings.Contains(out, "tq/human-answer") {
		t.Fatalf("an agent may ask: %d %s", code, out)
	}
	if code, out := hook("tq pack db migrate apply --env prod"); code != 2 || !strings.Contains(out, "human") {
		t.Fatalf("an executor form: %d %s", code, out)
	}
}
