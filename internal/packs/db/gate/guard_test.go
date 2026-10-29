package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/db/drill"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

const prodRef = "aaaaaaaaaaaaaaaaaaaa"
const stagingRef = "bbbbbbbbbbbbbbbbbbbb"

const dbBlock = `
migrations:
  - {path: supabase/migrations, engine: postgres, migrator: supabase}
drill:
  - {name: main, engine: postgres, source: {kind: supabase-dump}, freshness: 30d}
environments:
  dev: {match: {dotenv: [.env], hosts: [localhost]}, migrate: auto}
  staging: {match: {dotenv: [.env.staging], project_refs: [` + stagingRef + `]}}
  prod: {match: {dotenv: [.env.prod], project_refs: [` + prodRef + `], servers: [acme-sql.database.windows.net]}, migrate: approve}
`

type fixture struct {
	t    *testing.T
	home string
	repo string
	env  packs.GuardEnv
	vars map[string]string
}

func yamlNode(t *testing.T, s string) yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatal(err)
	}
	return *doc.Content[0]
}

func newFixture(t *testing.T, block string, repoBlock ...string) *fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	keyring.MockInit()
	for _, k := range []string{"TQ_HUB_TASK", "TENTAQLES_HEADLESS", "CLAUDECODE", "CLAUDE_PLUGIN_ROOT"} {
		t.Setenv(k, "")
	}
	repo := filepath.Join(home, "acme")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	os.MkdirAll(filepath.Join(repo, "supabase", "migrations"), 0o755)
	os.WriteFile(filepath.Join(repo, "supabase", "migrations", "20261001000000_orders.sql"), []byte("create table public.orders(id int);\n"), 0o644)
	en := true
	m := &manifest.Manifest{Packs: map[string]manifest.PackConfig{"db": {Enabled: &en, Raw: yamlNode(t, block)}}, Identities: map[string]manifest.Identity{}}
	var repoPacks map[string]manifest.PackConfig
	if len(repoBlock) > 0 {
		repoPacks = map[string]manifest.PackConfig{"db": {Raw: yamlNode(t, repoBlock[0])}}
	}
	f := &fixture{t: t, home: home, repo: repo, vars: map[string]string{}}
	f.env = packs.GuardEnv{Config: packs.Merge(m.Packs, repoPacks), Manifest: m, Workspace: "acme", WorkspaceRoot: repo, RepoRoot: repo, Cwd: repo}
	prev := Getenv
	Getenv = func(k string) string { return f.vars[k] }
	t.Cleanup(func() { Getenv = prev })
	return f
}

func (f *fixture) link(ref string) {
	os.MkdirAll(filepath.Join(f.repo, "supabase", ".temp"), 0o755)
	os.WriteFile(filepath.Join(f.repo, "supabase", ".temp", "project-ref"), []byte(ref+"\n"), 0o644)
}

func (f *fixture) ctx() packs.GuardCtx { return packs.NewGuardCtx(f.env, "db") }

func (f *fixture) migrate(tool, cmd string) (policy.Action, string, bool) {
	return MigrateEnv(policy.ToolCall{Tool: tool, Command: cmd}, f.ctx())
}

func (f *fixture) restore(tool, cmd string) (policy.Action, string, bool) {
	return RestoreTarget(policy.ToolCall{Tool: tool, Command: cmd}, f.ctx())
}

func TestMigratePolicyPerEnvironment(t *testing.T) {
	type tc struct {
		name, link, tool, cmd string
		vars                  map[string]string
		want                  policy.Action
		handled               bool
	}
	cases := []tc{
		{"migration up is local by default", "", "Bash", "supabase migration up", nil, policy.Allow, true},
		{"db push --local", prodRef, "Bash", "supabase db push --local", nil, policy.Allow, true},
		{"db push to linked prod", prodRef, "Bash", "supabase db push", nil, policy.Deny, true},
		{"db push to linked staging", stagingRef, "Bash", "supabase db push", nil, policy.Ask, true},
		{"migration up --linked staging", stagingRef, "Bash", "supabase migration up --linked", nil, policy.Ask, true},
		{"the project id variable wins over the file", prodRef, "Bash", "SUPABASE_PROJECT_ID=" + stagingRef + " supabase db push", nil, policy.Ask, true},
		{"the hook's project id", "", "Bash", "supabase db push", map[string]string{"SUPABASE_PROJECT_ID": prodRef}, policy.Deny, true},
		{"unlinked push: default env", "", "Bash", "supabase db push", nil, policy.Deny, true},
		{"dotenv staging", "", "Bash", "tq dotenv run --file .env.staging -- npx prisma migrate deploy", nil, policy.Ask, true},
		{"dotenv dev", "", "Bash", "tq dotenv run --file .env -- npx prisma migrate deploy", nil, policy.Allow, true},
		{"no signal: default env", "", "Bash", "npx prisma migrate deploy", nil, policy.Deny, true},
		{"inline local url", "", "Bash", "DATABASE_URL=postgres://tester@localhost:5432/acme npx prisma migrate deploy", nil, policy.Allow, true},
		{"inline pooler url of prod", "", "Bash", "DATABASE_URL=postgres://postgres." + prodRef + "@aws-0-eu-west-1.pooler.supabase.com:6543/postgres npx prisma migrate deploy", nil, policy.Deny, true},
		{"flyway jdbc staging", "", "Bash", "flyway migrate -url=jdbc:postgresql://db." + stagingRef + ".supabase.co:5432/postgres", nil, policy.Ask, true},
		{"ef core prod server", "", "Bash", `dotnet ef database update --connection "Server=tcp:acme-sql.database.windows.net,1433;Database=acme"`, nil, policy.Deny, true},
		{"golang-migrate local", "", "Bash", "migrate -database postgres://tester@localhost/acme -path db/migrations up", nil, policy.Allow, true},
		{"bash -c", prodRef, "Bash", `bash -c "supabase db push"`, nil, policy.Deny, true},
		{"powershell", stagingRef, "PowerShell", `pwsh -NoProfile -Command "supabase db push"`, nil, policy.Ask, true},
		{"docker exec is unreadable", "", "Bash", "docker compose exec app npx prisma migrate deploy", nil, policy.Deny, true},
		{"computed export floors", "", "Bash", `export DATABASE_URL=$(cat url.txt); DATABASE_URL=postgres://tester@localhost/acme prisma migrate deploy`, nil, policy.Deny, true},
		{"computed db url", "", "Bash", `supabase db push --db-url "$PROD_URL"`, nil, policy.Deny, true},
		{"python -m alembic local", "", "Bash", "DATABASE_URL=postgresql://tester@127.0.0.1/acme python -m alembic upgrade head", nil, policy.Allow, true},
		{"not a migration", "", "Bash", "supabase status && echo prisma", nil, policy.Allow, false},
		{"a comment is not a migration", "", "Bash", "ls # prisma migrate deploy", nil, policy.Allow, false},
		{"echo is not a migration", "", "Bash", "echo supabase db push", nil, policy.Allow, false},
		{"git prose", "", "Bash", `git commit -m "run supabase db push after review"`, nil, policy.Allow, false},
		{"an unknown wrapper naming the tool", "", "Bash", "dotenvx run -- supabase db push", nil, policy.Deny, true},
		{"python manage.py by path", "", "Bash", "DATABASE_URL=postgres://tester@localhost/acme python3 app/manage.py migrate", nil, policy.Allow, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, dbBlock)
			if c.link != "" {
				f.link(c.link)
			}
			for k, v := range c.vars {
				f.vars[k] = v
			}
			a, r, h := f.migrate(c.tool, c.cmd)
			if a != c.want || h != c.handled {
				t.Fatalf("%s: got %s handled=%v (%s), want %s handled=%v", c.cmd, a, h, r, c.want, c.handled)
			}
			if a != policy.Allow && !strings.Contains(r, IDMigrate) {
				t.Fatalf("reason without the rule id: %s", r)
			}
			if strings.Contains(r, "tester@") || strings.Contains(r, "pooler.supabase.com") {
				t.Fatalf("the reason prints a connection: %s", r)
			}
		})
	}
}

func TestMigrateWithoutEnvironmentsLeavesTheCoreRule(t *testing.T) {
	f := newFixture(t, "migrations: [{path: supabase/migrations, engine: postgres}]\n")
	if a, _, h := f.migrate("Bash", "supabase db push"); a != policy.Allow || h {
		t.Fatalf("got %s handled=%v", a, h)
	}
}

func TestMigrateDenyEnvAndRepoLayer(t *testing.T) {
	block := dbBlock + "  archive: {match: {hosts: [archive.acme.test]}, migrate: deny}\n"
	f := newFixture(t, block)
	if a, r, _ := f.migrate("Bash", "DATABASE_URL=postgres://tester@archive.acme.test/acme prisma migrate deploy"); a != policy.Deny || !strings.Contains(r, "migrate: deny") {
		t.Fatalf("deny env: %s %s", a, r)
	}
	// A repo file cannot loosen: a repo-only environment denies, a manifest
	// environment keeps its stricter policy and its own match lists.
	repo := `
environments:
  scratch: {match: {hosts: [scratch.acme.test]}, migrate: auto}
  prod: {migrate: auto, match: {hosts: [localhost]}}
`
	f = newFixture(t, dbBlock, repo)
	f.link(prodRef)
	if a, _, _ := f.migrate("Bash", "DATABASE_URL=postgres://tester@scratch.acme.test/acme prisma migrate deploy"); a != policy.Deny {
		t.Fatalf("repo-only env allowed: %s", a)
	}
	if a, _, _ := f.migrate("Bash", "supabase db push"); a != policy.Deny {
		t.Fatalf("repo loosened prod: %s", a)
	}
	if a, _, _ := f.migrate("Bash", "DATABASE_URL=postgres://tester@localhost/acme prisma migrate deploy"); a != policy.Allow {
		t.Fatalf("repo extended prod's match: %s", a)
	}
}

// ---- approvals -------------------------------------------------------------

func diskEvidence(t *testing.T, f *fixture, drillValue string) map[string]string {
	t.Helper()
	h, err := MigrationsHash(DiskReader{Root: f.repo}, []string{"supabase/migrations"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if drillValue == "" {
		raw, _ := drill.ReadLog(f.repo)
		drillValue = drill.Binding(raw, []drill.Drill{{Name: "main"}})
	}
	return map[string]string{EvMigrations: h, EvDrill: drillValue}
}

func (f *fixture) ask(env string, ev map[string]string) string {
	f.t.Helper()
	id, _, err := approval.Ask("acme", approval.Request{Pack: "db", Action: "migrate", Subject: Subject, Target: env,
		Evidence: ev, Summary: "migrations to " + env, Options: []string{"apply", "reject"}})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) answer(id, choice string) {
	f.t.Helper()
	st, err := store.Open("acme", nil)
	if err != nil {
		f.t.Fatal(err)
	}
	defer st.Close()
	key, err := ledger.SigningKey("acme")
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := st.AnswerPackApproval(id, choice, "ok", "hub:tester", store.PackSigner(ledger.AnswerSigner("acme", key))); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) drillLine(restored time.Time) {
	f.t.Helper()
	if err := drill.Append(f.repo, drill.Line{Name: "main", Engine: "postgres", OK: true, HostKind: "local",
		RestoredAt: restored.Format(time.RFC3339), BackupTakenAt: restored.Format(time.RFC3339)}); err != nil {
		f.t.Fatal(err)
	}
}

func TestApprovedMigrationSingleUse(t *testing.T) {
	f := newFixture(t, dbBlock)
	f.link(prodRef)
	f.drillLine(time.Now().Add(-time.Hour))
	if a, r, _ := f.migrate("Bash", "supabase db push"); a != policy.Deny || !strings.Contains(r, "tq pack db approve --env prod") {
		t.Fatalf("before approval: %s %s", a, r)
	}
	id := f.ask("prod", diskEvidence(t, f, ""))
	if a, _, _ := f.migrate("Bash", "supabase db push"); a != policy.Deny {
		t.Fatal("a pending approval allowed")
	}
	f.answer(id, "apply")
	g := f.ctx()
	a, r, h := MigrateEnv(policy.ToolCall{Tool: "Bash", Command: "supabase db push"}, g)
	if a != policy.Allow || !h {
		t.Fatalf("approved: %s %s", a, r)
	}
	// two migrations in one command, or the approved one next to an ask
	if a, r, _ := f.migrate("Bash", "supabase db push && supabase db push"); a != policy.Deny || !strings.Contains(r, "used twice") {
		t.Fatalf("twice: %s %s", a, r)
	}
	if a, _, _ := f.migrate("Bash", "supabase db push; tq dotenv run --file .env.staging -- prisma migrate deploy"); a != policy.Deny {
		t.Fatalf("with an ask: %s", a)
	}
	// the hook claims it after the allow; then it is used
	if err := approval.MarkUsed("acme", "db", id); err != nil {
		t.Fatal(err)
	}
	if a, r, _ := f.migrate("Bash", "supabase db push"); a != policy.Deny || !strings.Contains(r, "used") {
		t.Fatalf("after use: %s %s", a, r)
	}
}

func TestApprovalMismatches(t *testing.T) {
	t.Run("a changed migration", func(t *testing.T) {
		f := newFixture(t, dbBlock)
		f.link(prodRef)
		f.drillLine(time.Now().Add(-time.Hour))
		f.answer(f.ask("prod", diskEvidence(t, f, "")), "apply")
		os.WriteFile(filepath.Join(f.repo, "supabase", "migrations", "20261002000000_drop.sql"), []byte("drop table public.orders;\n"), 0o644)
		if a, r, _ := f.migrate("Bash", "supabase db push"); a != policy.Deny || !strings.Contains(r, "evidence changed") {
			t.Fatalf("%s %s", a, r)
		}
	})
	t.Run("a newer drill line", func(t *testing.T) {
		f := newFixture(t, dbBlock)
		f.link(prodRef)
		f.drillLine(time.Now().Add(-2 * time.Hour))
		f.answer(f.ask("prod", diskEvidence(t, f, "")), "apply")
		f.drillLine(time.Now().Add(-time.Hour))
		if a, _, _ := f.migrate("Bash", "supabase db push"); a != policy.Deny {
			t.Fatal("allowed with another drill")
		}
	})
	t.Run("asked with --no-drill", func(t *testing.T) {
		f := newFixture(t, dbBlock)
		f.link(prodRef)
		f.answer(f.ask("prod", diskEvidence(t, f, drill.BindingSkipped)), "apply")
		if a, r, _ := f.migrate("Bash", "supabase db push"); a != policy.Allow {
			t.Fatalf("%s %s", a, r)
		}
	})
	t.Run("rejected", func(t *testing.T) {
		f := newFixture(t, dbBlock)
		f.link(prodRef)
		f.answer(f.ask("prod", diskEvidence(t, f, "")), "reject")
		if a, _, _ := f.migrate("Bash", "supabase db push"); a != policy.Deny {
			t.Fatal("a rejected approval allowed")
		}
	})
	t.Run("another environment's approval", func(t *testing.T) {
		block := strings.Replace(dbBlock, "project_refs: ["+stagingRef+"]}}", "project_refs: ["+stagingRef+"]}, migrate: approve}", 1)
		f := newFixture(t, block)
		f.link(prodRef)
		f.answer(f.ask("staging", diskEvidence(t, f, "")), "apply")
		if a, _, _ := f.migrate("Bash", "supabase db push"); a != policy.Deny {
			t.Fatal("a staging approval allowed prod")
		}
	})
	t.Run("an unreadable command never matches", func(t *testing.T) {
		f := newFixture(t, dbBlock)
		f.answer(f.ask("prod", diskEvidence(t, f, "")), "apply")
		if a, _, _ := f.migrate("Bash", "docker compose exec app supabase db push"); a != policy.Deny {
			t.Fatal("an opaque command used the approval")
		}
	})
	t.Run("an unsigned answer", func(t *testing.T) {
		f := newFixture(t, dbBlock)
		f.link(prodRef)
		id := f.ask("prod", diskEvidence(t, f, ""))
		if err := ledger.Append("acme", ledger.Entry{Event: ledger.Answered, Decision: id, Pack: "db", Choice: "apply", ActorKind: "human", TS: time.Now().UTC().Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
		if a, _, _ := f.migrate("Bash", "supabase db push"); a != policy.Deny {
			t.Fatal("an unsigned answer allowed")
		}
	})
}

func TestMigrationsHashLimits(t *testing.T) {
	f := newFixture(t, dbBlock)
	g := f.ctx()
	gh, err := MigrationsHash(GuardReader{g}, []string{"supabase/migrations"}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	dh, _ := MigrationsHash(DiskReader{Root: f.repo}, []string{"./supabase/migrations/"}, time.Time{})
	if gh != dh {
		t.Fatal("the guard and the command hash differently")
	}
	// a missing path hashes as missing on both sides
	gm, err1 := MigrationsHash(GuardReader{g}, []string{"supabase/migrations", "db/none"}, time.Time{})
	dm, err2 := MigrationsHash(DiskReader{Root: f.repo}, []string{"supabase/migrations", "db/none"}, time.Time{})
	if err1 != nil || err2 != nil || gm != dm || gm == gh {
		t.Fatalf("missing: %v %v", err1, err2)
	}
	if _, err := MigrationsHash(DiskReader{Root: f.repo}, []string{"../other"}, time.Time{}); err == nil {
		t.Fatal("a path outside the repository")
	}
	if _, err := MigrationsHash(GuardReader{g}, []string{"supabase/migrations"}, time.Now().Add(-time.Second)); err == nil {
		t.Fatal("an expired budget")
	}
}

// ---- restore ---------------------------------------------------------------

func TestRestoreTarget(t *testing.T) {
	cases := []struct {
		tool, cmd string
		vars      map[string]string
		deny      bool
	}{
		{"Bash", "pg_restore --no-owner -d postgres://tester@localhost/scratch acme.dump", nil, false},
		{"Bash", "pg_restore -h db." + prodRef + ".supabase.co -d postgres acme.dump", nil, true},
		{"Bash", "pg_restore -d postgres acme.dump", map[string]string{"PGHOST": "prod.acme.test"}, true},
		{"Bash", "PGHOST=localhost pg_restore -d postgres acme.dump", map[string]string{"PGHOST": "prod.acme.test"}, false},
		{"Bash", "pg_restore -d postgres acme.dump", map[string]string{"PGSERVICE": "prod"}, true},
		{"Bash", "pg_restore -f out.sql acme.dump", nil, false},
		{"Bash", "psql -f dump.sql", nil, false},
		{"Bash", "psql -h localhost -f dump.sql", nil, false},
		{"Bash", `psql "postgres://tester@10.1.1.1/acme" < dump.sql`, nil, true},
		{"Bash", "psql -Xf dump.sql -h prod.acme.test", nil, true},
		{"Bash", "psql -dfoo -h prod.acme.test -c 'select 1'", nil, false},
		{"Bash", "tq dotenv run --file .env.prod -- psql -f dump.sql", nil, true},
		{"Bash", "tq dotenv run --file .env -- psql -f dump.sql", nil, true},
		{"Bash", "psql -h $DBHOST -f dump.sql", nil, true},
		{"Bash", "sqlpackage /a:Import /sf:acme.bacpac /tsn:acme-sql.database.windows.net /tdn:acme", nil, true},
		{"Bash", "sqlpackage /Action:Import /SourceFile:acme.bacpac /TargetServerName:localhost /TargetDatabaseName:scratch", nil, false},
		{"Bash", "sqlpackage -a:Import -sf:acme.bacpac", nil, true},
		{"Bash", `sqlcmd -S acme-sql.database.windows.net -Q "RESTORE DATABASE acme FROM DISK='x.bak'"`, nil, true},
		{"Bash", `sqlcmd -S localhost,1433 -Q "RESTORE DATABASE scratch FROM DISK='x.bak'"`, nil, false},
		{"Bash", "supabase db reset", nil, false},
		{"Bash", "supabase db reset --linked", nil, true},
		{"Bash", "supabase db reset --db-url postgres://tester@localhost:54322/postgres", nil, false},
		{"Bash", "docker exec acme-db pg_restore -d acme /tmp/acme.dump", nil, true},
		{"PowerShell", `pg_restore -d postgres://tester@prod.acme.test/acme C:\dumps\acme.dump`, nil, true},
		{"PowerShell", `pg_restore -d postgres://tester@127.0.0.1/acme C:\dumps\acme.dump`, nil, false},
		{"Bash", "echo pg_restore -d prod", nil, false},
	}
	for _, c := range cases {
		t.Run(c.cmd, func(t *testing.T) {
			f := newFixture(t, dbBlock)
			for k, v := range c.vars {
				f.vars[k] = v
			}
			a, r, h := f.restore(c.tool, c.cmd)
			if (a == policy.Deny) != c.deny || (c.deny && (!h || !strings.Contains(r, IDRestore))) {
				t.Fatalf("got %s handled=%v %q, want deny=%v", a, h, r, c.deny)
			}
			if strings.Contains(r, "tester@") {
				t.Fatalf("the reason prints a connection: %s", r)
			}
		})
	}
}

func TestRestoreTargetWithoutEnvironments(t *testing.T) {
	f := newFixture(t, "migrations: [{path: supabase/migrations, engine: postgres}]\n")
	if a, _, _ := f.restore("Bash", "pg_restore -h prod.acme.test -d acme x.dump"); a != policy.Deny {
		t.Fatal("a non-local restore without environments")
	}
	if a, _, _ := f.restore("Bash", "pg_restore -h localhost -d acme x.dump"); a != policy.Allow {
		t.Fatal("a local restore")
	}
}

func TestGuardsStayInBudget(t *testing.T) {
	f := newFixture(t, dbBlock)
	f.link(prodRef)
	big := strings.Repeat("echo tester acme ", 4000) + "; supabase db push"
	for _, guard := range []func(policy.ToolCall, packs.GuardCtx) (policy.Action, string, bool){MigrateEnv, RestoreTarget} {
		start := time.Now()
		guard(policy.ToolCall{Tool: "Bash", Command: big}, f.ctx())
		if d := time.Since(start); d > 200*time.Millisecond {
			t.Fatalf("a 64 KiB command took %s", d)
		}
	}
}
