package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func withRemote(t *testing.T, run func(args ...string) string) {
	t.Helper()
	remote := filepath.Join(testutil.TempDir(t), "remote.git")
	_ = exec.Command("git", "init", "-q", "--bare", "-b", "main", remote).Run()
	run("remote", "add", "origin", remote)
	run("push", "-q", "-u", "origin", "main")
	run("remote", "set-head", "origin", "main")
}

func noHooks(t *testing.T) string { return "core.hooksPath=" + testutil.TempDir(t) }

func TestPushPII_ValueAddedThenRemovedInRangeIsDenied(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	withRemote(t, run)
	stage(t, ws, run, "export.csv", "529.982.247-25\n") // tq:allow-pii
	run("-c", noHooks(t), "commit", "-q", "-m", "feat: export")
	run("rm", "-q", "export.csv")
	run("-c", noHooks(t), "commit", "-q", "-m", "chore: drop export")
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git push"))
	if code != 2 || !strings.Contains(errOut, "tq/push-pii") || !strings.Contains(errOut, "(cpf)") || strings.Contains(errOut, "529") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestPushPII_ScansNamedBranchNotJustHead(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	withRemote(t, run)
	run("checkout", "-q", "-b", "feature")
	stage(t, ws, run, "export.csv", "529.982.247-25\n") // tq:allow-pii
	run("-c", noHooks(t), "commit", "-q", "-m", "feat: export")
	run("checkout", "-q", "main")
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git push origin feature"))
	if code != 2 || !strings.Contains(errOut, "tq/push-pii") || !strings.Contains(errOut, "export.csv") {
		t.Fatalf("named branch: code=%d err=%q", code, errOut)
	}
	code, _, errOut = runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git push origin main"))
	if strings.Contains(errOut, "tq/push-pii") {
		t.Fatalf("clean main push denied: code=%d err=%q", code, errOut)
	}
}

func TestPushPII_MirrorAsks(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	withRemote(t, run)
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git push --mirror"))
	if code == 2 || !strings.Contains(out+errOut, "tq/push-pii") || !strings.Contains(out+errOut, "--mirror") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestCommitPII_MarkerOnlyInsideTestDirs(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "src/seed.sql", "insert into c values ('529.982.247-25'); -- tq:allow-pii\n") // tq:allow-pii
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat: seed"`))
	if code != 2 || !strings.Contains(errOut, "only applies inside test and fixture directories") || strings.Contains(errOut, "529") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestCommitPII_UnscannableCommitAsks(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData) // not a git repository
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat: x"`))
	if code == 2 || !strings.Contains(out+errOut, "could not scan what this commit records") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestTextPII_SegmentsFlagsAndAsks(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	_ = run
	hook := []string{"claude-hook", "pre-tool-use"}
	// a CPF in another segment is not the commit's text
	code, _, errOut := runHook(t, hook, hookPayloadFor(t, ws, `psql -c "select * from customers where cpf='529.982.247-25'" && git commit -m "fix: x"`)) // tq:allow-pii
	if strings.Contains(errOut, "tq/text-pii") {
		t.Fatalf("other segment must not count: code=%d err=%q", code, errOut)
	}
	if err := os.WriteFile(filepath.Join(ws, "body.md"), []byte("cpf 529.982.247-25\n"), 0o600); err != nil { // tq:allow-pii
		t.Fatal(err)
	}
	for _, cmd := range []string{
		`gh pr create --body-file=body.md`,
		`gh pr create -Fbody.md`,
		`gh pr create --title t --body-file "body.md"`,
	} {
		code, _, errOut = runHook(t, hook, hookPayloadFor(t, ws, cmd))
		if code != 2 || !strings.Contains(errOut, "tq/text-pii") || !strings.Contains(errOut, "body.md") {
			t.Errorf("%s: code=%d err=%q", cmd, code, errOut)
		}
	}
	for cmd, want := range map[string]string{
		`gh pr create -F -`:                   "stdin cannot be scanned",
		`gh pr create --body-file missing.md`: "could not read the body file",
		`gh pr create --body-file .`:          "non-regular",
		`gh pr create --body "unterminated`:   "could not parse the command safely",
		`git commit -m "unterminated`:         "could not parse the command safely",
	} {
		code, out, errOut := runHook(t, hook, hookPayloadFor(t, ws, cmd))
		if code == 2 || !strings.Contains(out+errOut, want) {
			t.Errorf("%s: want ask %q, got code=%d out=%q err=%q", cmd, want, code, out, errOut)
		}
	}
}

func TestPiiFixtureDump_ReadsViaFileFlagAllowed(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	for _, cmd := range []string{
		`grep -f tests/patterns.txt data/exports/q3.csv`,
		`psql -c "select * from customers" -f tests/check.sql`,
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
		if code != 0 || strings.Contains(errOut, "pii") {
			t.Errorf("%s: wrongly denied (code=%d): %q", cmd, code, errOut)
		}
	}
}

func TestPiiFixtureDump_QuotedNestedChainedWrites(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	for _, cmd := range []string{
		`cd app && cp data/exports/q3.csv "tests/fixtures/q3 copy.csv"`,
		`rsync data/exports/ pkg/tests/fixtures/`,
		`sudo cp data/exports/q3.csv ../tests/q3.csv`,
		`cp -t src/test/ data/exports/q3.csv`,
		`cat data/exports/q3.csv | tee seeds/q3.csv`,
		`mysqldump -r tests/dump.sql customers && head data/exports/a.csv`,
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
		if code != 2 || !strings.Contains(errOut, "tq/pii-fixture-dump") {
			t.Errorf("%s: code=%d err=%q", cmd, code, errOut)
		}
	}
}

func TestPiiFixtureDump_RedirectsWithoutSpaces(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	for _, cmd := range []string{
		`psql -c "select * from customers">tests/out.txt`,
		`cat data/exports/q3.csv>tests/q3.csv`,
		`cat data/exports/q3.csv &> tests/out.txt`,
		`cat data/exports/q3.csv &>tests/out.txt`,
		`cat data/exports/q3.csv >>tests/q3.csv`,
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
		if code != 2 || !strings.Contains(errOut, "tq/pii-fixture-dump") {
			t.Errorf("%s: code=%d err=%q", cmd, code, errOut)
		}
	}
}

func TestPiiFixtureDump_CopyTargetGaps(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	for _, cmd := range []string{
		`cp data/exports/q3.csv tests/fixtures/q3.csv > /dev/null 2>&1`,
		`cp data/exports/q3.csv tests`,
		`cp data/exports/q3.csv pkg/tests`,
		`find data/exports/q3.csv -exec cp {} tests/fixtures/ \;`,
		`ls data/exports/*.csv | xargs cp -t tests/fixtures/`,
		`sudo busybox cp data/exports/q3.csv seeds/`,
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
		if code != 2 || !strings.Contains(errOut, "tq/pii-fixture-dump") {
			t.Errorf("%s: code=%d err=%q", cmd, code, errOut)
		}
	}
	for _, cmd := range []string{
		`rsync -t tests/helpers.txt data/exports/q3.csv`, // -t is "preserve times" for rsync
		`cp data/exports/q3.csv src/seed.sql`,            // a file called seed.sql is not a seed directory
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
		if code != 0 || strings.Contains(errOut, "pii") {
			t.Errorf("%s: wrongly denied (code=%d): %q", cmd, code, errOut)
		}
	}
}

func TestCommitPII_SpoofedDiffHeaderDoesNotReenableMarker(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	stage(t, ws, run, "src/a.txt", "++ tests/x\ninsert ('529.982.247-25') -- tq:allow-pii\n") // tq:allow-pii
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat: a"`))
	if code != 2 || !strings.Contains(errOut, "src/a.txt:2 (cpf)") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestPushPII_ScanFailureAfterResolvedRangeAsks(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	withRemote(t, run)
	stage(t, ws, run, "a.txt", "plain\n")
	run("-c", noHooks(t), "commit", "-q", "-m", "feat: a")
	orig := pushGit
	defer func() { pushGit = orig }()
	pushGit = func(ctx context.Context, cwd string, limit int, args ...string) (string, bool, error) {
		if len(args) > 0 && args[0] == "log" {
			return "", false, errors.New("simulated git failure")
		}
		return orig(ctx, cwd, limit, args...)
	}
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git push"))
	if code == 2 || !strings.Contains(out+errOut, "could not scan the commits about to be pushed") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestPushPII_DeadlineAsks(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	run := gitRepo(t, ws)
	withRemote(t, run)
	origGit, origDeadline := pushGit, scanBudget
	defer func() { pushGit, scanBudget = origGit, origDeadline }()
	scanBudget = 50 * time.Millisecond
	pushGit = func(ctx context.Context, cwd string, limit int, args ...string) (string, bool, error) {
		<-ctx.Done()
		return "", false, ctx.Err()
	}
	start := time.Now()
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "git push"))
	if code == 2 || !strings.Contains(out+errOut, "scan did not finish in time") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("hook stalled")
	}
}

// TestHelperProcess is a subprocess used by TestRunBounded: it keeps stdout
// open for a while, like a git launcher whose child holds the pipe.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("TQ_HELPER") {
	case "parent":
		c := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		c.Env = append(os.Environ(), "TQ_HELPER=child")
		c.Stdout = os.Stdout
		_ = c.Start()
		time.Sleep(2 * time.Second)
	case "child":
		time.Sleep(2 * time.Second)
	}
}

func TestRunBounded_ReturnsWhenGrandchildHoldsPipe(t *testing.T) {
	t.Setenv("TQ_HELPER", "parent")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := runBounded(ctx, os.Args[0], "", 1<<20, "-test.run=^TestHelperProcess$")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want deadline exceeded", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("runBounded took %v, want under 1s", d)
	}
	time.Sleep(2200 * time.Millisecond) // let the helper processes exit so the test binary can be removed
}

func TestCommitPII_DeadlineAsksInTime(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	gitRepo(t, ws)
	origGit, origDeadline := pushGit, scanBudget
	defer func() { pushGit, scanBudget = origGit, origDeadline }()
	scanBudget = 50 * time.Millisecond
	pushGit = func(ctx context.Context, cwd string, limit int, args ...string) (string, bool, error) {
		<-ctx.Done()
		return "", false, ctx.Err()
	}
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, `git commit -m "feat: x"`))
	if code == 2 || !strings.Contains(out+errOut, "scan did not finish in time") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestPiiFixtureDump_WrapperOptionsAndQuotedRedirects(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", piiData)
	for _, cmd := range []string{
		`ls data/exports/*.csv | xargs -I{} cp {} tests/fixtures/`,
		`ls data/exports/*.csv | xargs -I {} cp {} tests/fixtures/`,
		`ls data/exports/*.csv | xargs -0 cp -t tests/fixtures/`,
		`ls data/exports/*.csv | xargs -n1 cp -t tests/fixtures/`,
		`find data/exports/q3.csv -exec sudo cp {} tests/fixtures/ \;`,
		`sudo -u bob cp data/exports/q3.csv tests/fixtures/q3.csv`,
		`nice -n 5 cp data/exports/q3.csv tests/fixtures/q3.csv`,
		`cp -S '>' data/exports/q3.csv tests/fixtures/q3.csv`,
		`cat data/exports/q3.csv >| tests/q3.csv`,
		`cat data/exports/q3.csv >&tests/q3.csv`,
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
		if code != 2 || !strings.Contains(errOut, "tq/pii-fixture-dump") {
			t.Errorf("%s: code=%d err=%q", cmd, code, errOut)
		}
	}
	for _, cmd := range []string{
		`grep -n "->" tests/foo_test.go data/exports/x.csv`,
		`cat data/exports/q3.csv 2>&1`,
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
		if code != 0 || strings.Contains(errOut, "pii") {
			t.Errorf("%s: wrongly denied (code=%d): %q", cmd, code, errOut)
		}
	}
}
