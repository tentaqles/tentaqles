package db

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/db/drill"
	"github.com/tentaqles/tentaqles/internal/packs/db/gate"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

const prodRef = "aaaaaaaaaaaaaaaaaaaa"

const gateBlock = `
migrations:
  - {path: supabase/migrations, engine: postgres, migrator: supabase}
drill:
  - {name: main, engine: postgres, source: {kind: supabase-dump}, freshness: 30d}
environments:
  dev: {match: {dotenv: [.env], hosts: [localhost]}, migrate: auto}
  staging: {match: {dotenv: [.env.staging]}}
  prod: {match: {dotenv: [.env.prod], project_refs: [` + prodRef + `]}, migrate: approve, require_merged: false}
`

type cmdEnv struct {
	t    *testing.T
	repo string
	in   *packs.Input
	m    *manifest.Manifest
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=tester", "GIT_AUTHOR_EMAIL=tester@acme.test", "GIT_COMMITTER_NAME=tester", "GIT_COMMITTER_EMAIL=tester@acme.test")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func setupCmd(t *testing.T, block string) *cmdEnv {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	keyring.MockInit()
	for _, k := range []string{"TQ_HUB_TASK", "TENTAQLES_HEADLESS", "CLAUDECODE", "CLAUDE_PLUGIN_ROOT", "TEST_DATABASE_URL"} {
		t.Setenv(k, "")
	}
	repo := filepath.Join(home, "acme")
	os.MkdirAll(filepath.Join(repo, "supabase", "migrations"), 0o755)
	os.WriteFile(filepath.Join(repo, "supabase", "migrations", "20261001000000_orders.sql"), []byte("create table public.orders (id bigint primary key);\n"), 0o644)
	os.MkdirAll(filepath.Join(repo, "supabase", ".temp"), 0o755)
	os.WriteFile(filepath.Join(repo, "supabase", ".temp", "project-ref"), []byte(prodRef), 0o644)
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("supabase/.temp/\n"), 0o644)
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "init")
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(block), &doc); err != nil {
		t.Fatal(err)
	}
	en := true
	m := &manifest.Manifest{Packs: map[string]manifest.PackConfig{"db": {Enabled: &en, Raw: *doc.Content[0]}}, Identities: map[string]manifest.Identity{}}
	if err := manifest.ValidatePacks(m.Packs); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	cfg := packs.Merge(m.Packs, nil)
	in := &packs.Input{Root: repo, Manifest: m, Config: cfg, Git: packs.GitRepo{Root: repo}, Tools: packs.NewToolRunner(repo, cfg)}
	in.SetWorkspace("acme", repo)
	prevGate := gate.Getenv
	gate.Getenv = func(string) string { return "" }
	t.Cleanup(func() { gate.Getenv = prevGate })
	return &cmdEnv{t: t, repo: repo, in: in.ForPack("db"), m: m}
}

func (e *cmdEnv) tq(args ...string) (string, error) {
	e.t.Helper()
	parent := &cobra.Command{Use: "db", SilenceUsage: true, SilenceErrors: true}
	for _, c := range commands() {
		parent.AddCommand(c)
	}
	var out bytes.Buffer
	parent.SetOut(&out)
	parent.SetErr(io.Discard)
	parent.SetArgs(args)
	err := parent.ExecuteContext(packs.WithInput(context.Background(), e.in, nil))
	return out.String(), err
}

func (e *cmdEnv) guard(cmd string) (policy.Action, string) {
	g := packs.NewGuardCtx(packs.GuardEnv{Config: e.in.Config, Manifest: e.m, Workspace: "acme", WorkspaceRoot: e.repo, RepoRoot: e.repo, Cwd: e.repo}, "db")
	a, r, _ := gate.MigrateEnv(policy.ToolCall{Tool: "Bash", Command: cmd}, g)
	return a, r
}

func (e *cmdEnv) freshDrill() {
	e.t.Helper()
	now := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if err := drill.Append(e.repo, drill.Line{Name: "main", Engine: "postgres", OK: true, HostKind: "local", RestoredAt: now, BackupTakenAt: now}); err != nil {
		e.t.Fatal(err)
	}
}

func answerDecision(t *testing.T, id, choice string) {
	t.Helper()
	st, err := store.Open("acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	key, err := ledger.SigningKey("acme")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AnswerPackApproval(id, choice, "ok", "hub:tester", store.PackSigner(ledger.AnswerSigner("acme", key))); err != nil {
		t.Fatal(err)
	}
}

func decisionIn(t *testing.T, out string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "asked: decision ") {
			return strings.Fields(l)[2]
		}
	}
	t.Fatalf("no decision in %q", out)
	return ""
}

func TestApproveMigrateFlow(t *testing.T) {
	e := setupCmd(t, gateBlock)
	if _, err := e.tq("approve", "--env", "prod"); err == nil || !strings.Contains(err.Error(), "tq pack db drill main") {
		t.Fatalf("a stale drill asked: %v", err)
	}
	e.freshDrill()
	if a, _ := e.guard("supabase db push"); a != policy.Deny {
		t.Fatal("allowed before an approval")
	}
	out, err := e.tq("approve", "--env", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "tq hub answer") || !strings.Contains(out, "never an agent") {
		t.Fatalf("output: %s", out)
	}
	id := decisionIn(t, out)
	sts, err := approval.List("acme", "db")
	if err != nil || len(sts) != 1 || sts[0].Target != "prod" || sts[0].Subject != gate.Subject {
		t.Fatalf("list: %v %+v", err, sts)
	}
	answerDecision(t, id, "apply")
	if a, r := e.guard("supabase db push"); a != policy.Allow {
		t.Fatalf("approved: %s %s", a, r)
	}
	// a changed migration voids it
	os.WriteFile(filepath.Join(e.repo, "supabase", "migrations", "20261001000000_orders.sql"), []byte("drop table public.orders;\n"), 0o644)
	if a, _ := e.guard("supabase db push"); a != policy.Deny {
		t.Fatal("a changed migration still matched")
	}
}

func TestApproveRefusals(t *testing.T) {
	e := setupCmd(t, gateBlock)
	e.freshDrill()
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"approve"}, "--env"},
		{[]string{"approve", "--env", "qa"}, "not declared"},
		{[]string{"approve", "--env", "dev"}, "needs no approval"},
		{[]string{"approve", "--env", "staging"}, "needs no approval"},
	} {
		if _, err := e.tq(c.args...); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v", c.args, err)
		}
	}
	t.Setenv("CLAUDECODE", "1")
	for _, f := range []string{"--no-drill", "--allow-failing"} {
		if _, err := e.tq("approve", "--env", "prod", f); err == nil || !strings.Contains(err.Error(), "human-only") {
			t.Errorf("%s in an agent session: %v", f, err)
		}
	}
	// the agent may still ask
	if _, err := e.tq("approve", "--env", "prod"); err != nil {
		t.Fatalf("an agent ask: %v", err)
	}
}

func TestApproveNoDrillByAHuman(t *testing.T) {
	e := setupCmd(t, gateBlock)
	out, err := e.tq("approve", "--env", "prod", "--no-drill")
	if err != nil {
		t.Fatal(err)
	}
	answerDecision(t, decisionIn(t, out), "apply")
	if a, r := e.guard("supabase db push"); a != policy.Allow {
		t.Fatalf("%s %s", a, r)
	}
}

func TestApproveRequiresMergedMigrations(t *testing.T) {
	e := setupCmd(t, strings.Replace(gateBlock, ", require_merged: false", "", 1))
	e.freshDrill()
	_, err := e.tq("approve", "--env", "prod")
	if err == nil || !strings.Contains(err.Error(), "requires merged migrations") {
		t.Fatalf("no remote ref: %v", err)
	}
	os.WriteFile(filepath.Join(e.repo, "supabase", "migrations", "20261002000000_more.sql"), []byte("select 1;\n"), 0o644)
	if _, err := e.tq("approve", "--env", "prod"); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("uncommitted: %v", err)
	}
}

func TestApproveRefusesFailingChecks(t *testing.T) {
	e := setupCmd(t, gateBlock)
	e.freshDrill()
	// an unsuppressed lint error in a changed migration
	os.WriteFile(filepath.Join(e.repo, "supabase", "migrations", "20261003000000_drop.sql"), []byte("alter table public.orders drop column id;\n"), 0o644)
	_, err := e.tq("approve", "--env", "prod")
	if err == nil || !strings.Contains(err.Error(), "db/migration-lint") {
		t.Fatalf("err = %v", err)
	}
}

func TestCommandAnnotations(t *testing.T) {
	for _, c := range commands() {
		switch c.Name() {
		case "approve":
			if c.Annotations[packs.AnnotationHumanOnly] != packs.ApprovalAsk {
				t.Fatal("approve must be annotated ask")
			}
		default:
			if c.Annotations[packs.AnnotationHumanOnly] == packs.HumanOnlyExecute || strings.Contains(c.Name(), "apply") {
				t.Fatalf("%s: no db command executes an approval", c.Name())
			}
		}
	}
}

func TestDrillCommandRefusals(t *testing.T) {
	e := setupCmd(t, gateBlock)
	if _, err := e.tq("drill", "nightly"); err == nil || !strings.Contains(err.Error(), "declared: main") {
		t.Fatalf("unknown drill: %v", err)
	}
	if _, err := e.tq("drill", "main"); err == nil || !strings.Contains(err.Error(), "test_db") {
		t.Fatalf("no test_db: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.repo, filepath.FromSlash(drill.EvidenceFile))); err == nil {
		t.Fatal("a refused drill wrote evidence")
	}
}

func TestMergeRulesOfTheGateKeys(t *testing.T) {
	trusted := map[string]manifest.PackConfig{"db": {Enabled: boolp(true), Raw: node(t, gateBlock)}}
	repo := map[string]manifest.PackConfig{"db": {Raw: node(t, `
drill:
  - {name: evil, engine: postgres, source: {kind: command, command: [curl, x]}}
environments:
  prod: {migrate: auto, approval_ttl: 10m, match: {hosts: [localhost]}}
  qa: {match: {hosts: [qa.acme.test]}, migrate: auto}
`)}}
	cfg := packs.Merge(trusted, repo)
	ds, err := drill.Parse(cfg.Pack("db"))
	if err != nil || len(ds) != 1 || ds[0].Name != "main" {
		t.Fatalf("a repo file changed the drills: %v %+v", err, ds)
	}
	if !problem(cfg, "drill") {
		t.Fatalf("no problem for the repo drill: %+v", cfg.Problems)
	}
	m := &manifest.Manifest{Packs: trusted}
	g := envgateFor(m, cfg)
	if g.Policy("prod", "migrate") != "approve" || g.Policy("qa", "migrate") != "deny" || g.TTL("prod") != 10*time.Minute {
		t.Fatalf("prod %s qa %s ttl %s", g.Policy("prod", "migrate"), g.Policy("qa", "migrate"), g.TTL("prod"))
	}
	if len(g.Match("prod")["hosts"]) != 0 {
		t.Fatal("a repo file extended prod's match")
	}
}

func boolp(b bool) *bool { return &b }

func node(t *testing.T, s string) yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatal(err)
	}
	return *doc.Content[0]
}

func problem(cfg packs.Config, key string) bool {
	for _, p := range cfg.Problems {
		if p.Pack == "db" && strings.HasPrefix(p.Key, key) {
			return true
		}
	}
	return false
}

func envgateFor(m *manifest.Manifest, cfg packs.Config) envgate.Gate {
	return envgate.FromManifest(envgate.DB, m, cfg)
}
