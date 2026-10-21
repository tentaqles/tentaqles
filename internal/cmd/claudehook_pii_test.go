package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

const piiData = "data:\n  classes: [pii, financial]\n  paths:\n    - { path: \"data/exports/**\", classes: [pii] }\n    - { table: public.customers, classes: [pii] }\n"

func gitRepo(t *testing.T, ws string) func(args ...string) string {
	t.Helper()
	run := func(args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = ws
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "init")
	return run
}

func stage(t *testing.T, ws string, run func(...string) string, name, body string) {
	t.Helper()
	p := filepath.Join(ws, name)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", name)
}

func TestCommitPII_DeniedWithCPFEvenWithoutDataBlock(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	run := gitRepo(t, ws)
	stage(t, ws, run, "seed.sql", "insert into c values ('529.982.247-25');\n") // tq:allow-pii
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat: seed"`))
	if code != 2 || !strings.Contains(errOut, "tq/commit-pii") || !strings.Contains(errOut, "seed.sql:1 (cpf)") || strings.Contains(errOut, "529") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestCommitPII_EmailOnlyWhenPiiDeclared(t *testing.T) {
	plain := setupTrustedWorkspaceWithManifest(t, "beta", "")
	run := gitRepo(t, plain)
	stage(t, plain, run, "users.csv", "ana.silva@example.com\n")
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, plain, `git commit -m "feat: users"`)); code != 0 {
		t.Fatalf("no data block: e-mail must not be scanned: %q", errOut)
	}
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run = gitRepo(t, ws)
	stage(t, ws, run, "users.csv", "ana.silva@example.com\n")
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat: users"`))
	if code != 2 || !strings.Contains(errOut, "(email)") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestCommitPII_AllowMarkerAndCleanCommit(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "tests/fixtures/people.sql", "insert into c values ('529.982.247-25'); -- synthetic, tq:allow-pii\n") // tq:allow-pii
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "test: fixture"`)); code != 0 {
		t.Fatalf("allow marker: %q", errOut)
	}
	stage(t, ws, run, "src/a.go", "package a\n")
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat: a"`)); code != 0 {
		t.Fatalf("clean commit: %q", errOut)
	}
}

func TestPushPII_DeniedForUnpushedCommits(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	remote := filepath.Join(testutil.TempDir(t), "remote.git")
	_ = exec.Command("git", "init", "-q", "--bare", "-b", "main", remote).Run()
	run("remote", "add", "origin", remote)
	run("push", "-q", "-u", "origin", "main")
	stage(t, ws, run, "export.csv", "4111 1111 1111 1111\n") // tq:allow-pii
	run("-c", "core.hooksPath="+testutil.TempDir(t), "commit", "-q", "-m", "feat: export")
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git push"))
	if code != 2 || !strings.Contains(errOut, "tq/push-pii") || !strings.Contains(errOut, "export.csv") || !strings.Contains(errOut, "(card)") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git push --dry-run")); code != 0 {
		t.Fatal("dry run must not be scanned")
	}
}

func TestPushPII_UnresolvableRangeAllows(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "export.csv", "4111 1111 1111 1111\n") // tq:allow-pii
	run("-c", "core.hooksPath="+testutil.TempDir(t), "commit", "-q", "-m", "feat: export")
	// no remote at all: tq has no range to scan and gives no opinion (the identity guard still applies)
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git push -u origin main"))
	if strings.Contains(errOut, "tq/push-pii") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestTextPII_CommitMessageAndGhBody(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	gitRepo(t, ws)
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "fix: customer 529.982.247-25 could not log in"`)) // tq:allow-pii
	if code != 2 || !strings.Contains(errOut, "tq/text-pii") || !strings.Contains(errOut, "(cpf)") || strings.Contains(errOut, "529") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	body := filepath.Join(ws, "body.md")
	if err := os.WriteFile(body, []byte("Reported by ana.silva@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `gh pr create --title "fix: login" --body-file body.md`))
	if code != 2 || !strings.Contains(errOut, "tq/text-pii") || !strings.Contains(errOut, "body.md") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `gh pr create --title "fix: login" --body "no personal data here"`)); strings.Contains(errOut, "text-pii") {
		t.Fatalf("clean body denied: code=%d %q", code, errOut)
	}
}

func TestPiiFixtureDump_Denied(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	for _, cmd := range []string{
		`psql "$DATABASE_URL" -c "\copy (select * from customers limit 100) to 'tests/fixtures/customers.csv' csv"`,
		`pg_dump "$DATABASE_URL" -t public.customers --data-only -f seeds/customers.sql`,
		`psql -c "select * from customers" > tests/customers.txt`,
		`cp data/exports/q3.csv tests/fixtures/q3.csv`,
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
		if code != 2 || !strings.Contains(errOut, "tq/pii-fixture-dump") || !strings.Contains(errOut, "synthetic") {
			t.Errorf("%s: code=%d err=%q", cmd, code, errOut)
		}
	}
}

func TestPiiFixtureDump_AllowedCases(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	for _, cmd := range []string{
		`head -n 5 data/exports/q3.csv`,                                 // reading is allowed
		`psql "$DATABASE_URL" -c "select * from customers limit 5"`,     // querying is allowed
		`psql -c "select * from orders" > tests/orders.txt`,             // unclassified table
		`pg_dump "$DATABASE_URL" -t customers -f backups/customers.sql`, // not a fixture dir
		`python scripts/make_fixtures.py --faker --out tests/fixtures/`, // synthetic generator
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
		if code != 0 || strings.Contains(errOut, "pii") {
			t.Errorf("%s: wrongly denied (code=%d): %q", cmd, code, errOut)
		}
	}
	plain := setupTrustedWorkspaceWithManifest(t, "beta", "")
	if code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, plain, `cp data/exports/q3.csv tests/fixtures/q3.csv`)); code != 0 {
		t.Fatal("workspace without a data block must not be affected by the fixture rule")
	}
}

func TestPIIRules_CanBeDisabledInManifest(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData+"guard:\n  disable: [tq/commit-pii]\n")
	run := gitRepo(t, ws)
	stage(t, ws, run, "seed.sql", "insert into c values ('529.982.247-25');\n") // tq:allow-pii
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat: seed"`)); code != 0 {
		t.Fatalf("disable by id must work: %q", errOut)
	}
}
