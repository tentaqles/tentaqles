package plans

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

func TestRecordRoundTripAndEvidence(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	id := NewID(now)
	if !ValidID(id) || ValidID("../x") || ValidID("p2026") {
		t.Fatal(id)
	}
	r := Record{ID: id, Env: "prod", Tool: "terraform", Dir: "infra/envs/prod", Subject: "infra/envs/prod", PlanFile: PlanName(id, "tfplan"),
		PlanSHA256: "sha256:aa", InputsHash: "sha256:bb", Identity: "subscription=2222", Summary: model.Summary{Total: 1, Counts: map[string]int{"create": 1}}}
	if err := Save(dir, r); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, r); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, id)
	if err != nil || got.Env != "prod" || got.Schema != Schema || got.Checks == nil {
		t.Fatalf("%v %+v", err, got)
	}
	all, err := All(dir)
	if err != nil || len(all) != 1 {
		t.Fatal(err, all)
	}
	ev := got.Evidence("")
	if ev[EvPlanID] != id || ev[EvPlanSHA] != "sha256:aa" || ev[EvEnv] != "prod" || ev[EvIdentity] != "subscription=2222" || len(ev) != 5 {
		t.Fatalf("%v", ev)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(filepath.Join(dir, Dir))
		if fi.Mode().Perm() != 0o700 {
			t.Fatalf("dir mode %v", fi.Mode())
		}
		fi, _ = os.Stat(filepath.Join(dir, RecordName(id)))
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("file mode %v", fi.Mode())
		}
	}
	if _, err := Load(dir, "p20261028120000-000000"); err == nil {
		t.Fatal("missing plan")
	}
	// cdk evidence binds one stack
	c := Record{ID: id, Tool: "cdk", Env: "prod", Stacks: []Stack{{Name: "AcmeStack", Aliases: []string{"acme-prod"}, Hash: "sha256:cc"}}}
	if e := c.Evidence("acme-prod"); e[EvStack] != "AcmeStack" || e[EvPlanSHA] != "sha256:cc" || e[EvIdentity] != None {
		t.Fatal(e)
	}
}

func TestPruneKeepsRecords(t *testing.T) {
	dir := t.TempDir()
	old := NewID(time.Now().Add(-10 * 24 * time.Hour))
	fresh := NewID(time.Now())
	for _, id := range []string{old, fresh} {
		WriteFile(dir, PlanName(id, "tfplan"), []byte("plan"))
		WriteFile(dir, RedactedName(id), []byte("{}"))
		Save(dir, Record{ID: id})
	}
	got, err := Prune(dir, 7*24*time.Hour, time.Now())
	if err != nil || len(got) != 1 || got[0] != old {
		t.Fatal(err, got)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(PlanName(old, "tfplan")))); !os.IsNotExist(err) {
		t.Fatal("old plan kept")
	}
	if _, err := Load(dir, old); err != nil {
		t.Fatal("record pruned")
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(PlanName(fresh, "tfplan")))); err != nil {
		t.Fatal("fresh plan pruned")
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=tester", "GIT_AUTHOR_EMAIL=tester@acme.test", "GIT_COMMITTER_NAME=tester", "GIT_COMMITTER_EMAIL=tester@acme.test")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func TestInputsHashAndClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	git(t, root, "init", "-q")
	os.MkdirAll(filepath.Join(root, "infra", ".terraform"), 0o755)
	os.WriteFile(filepath.Join(root, "infra", "main.tf"), []byte("resource x {}\n"), 0o644)
	os.WriteFile(filepath.Join(root, "infra", ".terraform", "environment"), []byte("prod"), 0o644)
	os.WriteFile(filepath.Join(root, "prod.tfvars"), []byte("a = 1\n"), 0o644)
	ctx := context.Background()
	h1, err := InputsHash(ctx, root, []string{"infra", "prod.tfvars"})
	if err != nil {
		t.Fatal(err)
	}
	// .terraform/ is not an input
	os.WriteFile(filepath.Join(root, "infra", ".terraform", "environment"), []byte("dev"), 0o644)
	h2, _ := InputsHash(ctx, root, []string{"infra", "prod.tfvars"})
	if h1 != h2 {
		t.Fatal(".terraform changed the hash")
	}
	os.WriteFile(filepath.Join(root, "prod.tfvars"), []byte("a = 2\n"), 0o644)
	h3, _ := InputsHash(ctx, root, []string{"infra", "prod.tfvars"})
	if h3 == h1 {
		t.Fatal("var file change not seen")
	}
	if ok, _ := Clean(ctx, root, []string{"infra"}); ok {
		t.Fatal("untracked counts as dirty")
	}
	git(t, root, "add", "infra/main.tf", "prod.tfvars")
	git(t, root, "commit", "-q", "-m", "init")
	if ok, err := Clean(ctx, root, []string{"infra"}); !ok || err != nil {
		t.Fatal("clean after commit", err)
	}
	if Head(ctx, root) == "" {
		t.Fatal("head")
	}
	if DefaultRemoteRef(ctx, root) != "" {
		t.Fatal("no remote")
	}
	if Subject(`infra\envs\prod dir`) != "infra/envs/prod_dir" {
		t.Fatal(Subject(`infra\envs\prod dir`))
	}
}
