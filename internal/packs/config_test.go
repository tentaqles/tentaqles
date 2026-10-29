package packs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
	"gopkg.in/yaml.v3"
)

func problemKeys(c Config) []string {
	var out []string
	for _, p := range c.Problems {
		out = append(out, p.Pack+"."+p.Key)
	}
	sort.Strings(out)
	return out
}

func TestMergeRules(t *testing.T) {
	trusted := packsOf(t, `
data:
  enabled: true
  tools: [dbt]
  timeouts: { dbt: 600s }
  tool_paths: { dbt: C:/tools/dbt.exe }
  example_paths: [models/]
  example_cap: 10GB
  example_floor: 0.5
  example_command: python a.py
  example_secret: s
  example_block: { prod: { limit: 100, strict: true } }
cloud: { enabled: true }
`)
	repo := packsOf(t, `
data:
  enabled: false
  tools: [sqlfluff]
  timeouts: { dbt: 900s, sqlfluff: 60s }
  tool_paths: { dbt: /tmp/evil }
  example_paths: [snapshots/]
  example_cap: 20GB
  example_floor: 0.8
  example_command: python b.py
  example_block: { prod: { limit: 50, strict: false }, dev: { limit: 1 } }
ml: { tools: [ruff] }
`)
	c := Merge(trusted, repo)
	d := c.Pack("data")
	if !d.Enabled || !reflect.DeepEqual(d.Tools, []string{"dbt", "sqlfluff"}) {
		t.Fatalf("enabled OR, tools union: %+v", d)
	}
	if d.Timeouts["dbt"] != 600*time.Second || d.Timeouts["sqlfluff"] != 60*time.Second {
		t.Fatalf("timeouts min: %v", d.Timeouts)
	}
	if d.ToolPaths["dbt"] != "C:/tools/dbt.exe" {
		t.Fatalf("tool_paths trusted only: %v", d.ToolPaths)
	}
	if got := d.Strings("example_paths"); !reflect.DeepEqual(got, []string{"models/", "snapshots/"}) {
		t.Fatalf("path union: %v", got)
	}
	if d.String("example_cap") != "10GB" || d.String("example_floor") != "0.8" || d.String("example_command") != "python b.py" {
		t.Fatalf("min/max/repo: cap %s floor %s cmd %s", d.String("example_cap"), d.String("example_floor"), d.String("example_command"))
	}
	if d.String("example_block.prod.limit") != "50" || d.String("example_block.prod.strict") != "true" || d.String("example_block.dev.limit") != "1" {
		t.Fatalf("nested: %v / %v / %v", d.String("example_block.prod.limit"), d.String("example_block.prod.strict"), d.String("example_block.dev.limit"))
	}
	if !c.IsEnabled("ml") || !c.IsEnabled("cloud") || c.IsEnabled("db") {
		t.Fatalf("enabled: %v", c.Enabled())
	}
	want := []string{"data.enabled", "data.example_block.prod.strict", "data.example_cap", "data.timeouts.dbt", "data.tool_paths"}
	if got := problemKeys(c); !reflect.DeepEqual(got, want) {
		t.Fatalf("problems %v want %v", got, want)
	}
	if d.Node("enabled") != nil || d.Node("tools") != nil {
		t.Fatal("framework keys live in the typed fields, not Raw")
	}
}

func TestMergeTrustedKeysAndRepoAdds(t *testing.T) {
	c := Merge(packsOf(t, "data: { example_command: a }\n"), packsOf(t, "data: { unknown_key: x, example_secret: y }\n"))
	d := c.Pack("data")
	if d.String("unknown_key") != "x" {
		t.Fatal("the repo may add a key the manifest lacks")
	}
	if d.String("example_secret") != "" || len(c.Problems) != 1 {
		t.Fatalf("trusted-only key from the repo: %q %v", d.String("example_secret"), c.Problems)
	}
	c = Merge(packsOf(t, "data: { other: a }\n"), packsOf(t, "data: { other: b }\n"))
	if c.Pack("data").String("other") != "a" || len(c.Problems) != 1 {
		t.Fatal("default kind: manifest wins, change reported")
	}
}

func TestConfigHashStable(t *testing.T) {
	a := Merge(packsOf(t, "data: { tools: [dbt, sqlfluff], example_paths: [b, a] }\nml: { enabled: true }\n"), nil)
	b := Merge(packsOf(t, "ml: { enabled: true }\ndata:\n  example_paths: [b, a]\n  tools: [sqlfluff, dbt]\n"), nil)
	if a.Hash() != b.Hash() || !strings.HasPrefix(a.Hash(), "sha256:") {
		t.Fatalf("%s != %s", a.Hash(), b.Hash())
	}
	c := Merge(packsOf(t, "data: { tools: [dbt, sqlfluff], example_paths: [a, b] }\nml: { enabled: true }\n"), nil)
	if c.Hash() == a.Hash() {
		t.Fatal("list order is config, the hash must change")
	}
	if Merge(nil, nil).Hash() == a.Hash() {
		t.Fatal("empty")
	}
}

func TestLoadConfigRepoFile(t *testing.T) {
	root := t.TempDir()
	m := &manifest.Manifest{Packs: packsOf(t, "data: { enabled: true }\n")}
	c, err := LoadConfig(m, root)
	if err != nil || c.RepoFile != "" || !c.IsEnabled("data") {
		t.Fatalf("%v %+v", err, c)
	}
	writeFile(t, root, "tq-packs.yaml", "ml: { tools: [ruff] }\n")
	c, err = LoadConfig(m, root)
	if err != nil || !c.IsEnabled("ml") || c.RepoFile == "" {
		t.Fatalf("%v %+v", err, c)
	}
	writeFile(t, root, "tq-packs.yaml", "web: {}\n")
	c, err = LoadConfig(m, root)
	if !errors.Is(err, ErrRepoFile) || !c.IsEnabled("data") || c.IsEnabled("ml") {
		t.Fatalf("an invalid repo file keeps the manifest config: %v %+v", err, c.Enabled())
	}
	// The pack validator runs on the repo file too.
	writeFile(t, root, "tq-packs.yaml", "data: { example_paths: notalist }\n")
	if _, err := LoadConfig(m, root); !errors.Is(err, ErrRepoFile) {
		t.Fatalf("validator: %v", err)
	}
}

func TestManifestValidatorFromRegisterPack(t *testing.T) {
	if err := manifest.ValidatePacks(packsOf(t, "data: { example_pathz: [a] }\n")); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("unknown pack key: %v", err)
	}
	if err := manifest.ValidatePacks(packsOf(t, "data: { example_paths: [a], tools: [dbt] }\n")); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterPanics(t *testing.T) {
	run := func(t *testing.T, name string, fn func()) {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			fn()
		})
	}
	noop := func(ctx context.Context, in *Input) Result { return Result{} }
	run(t, "duplicate check", func() { Register(Check{ID: "data/example-warn", Pack: "data", Kind: Builtin, Run: noop}) })
	run(t, "id outside pack", func() { Register(Check{ID: "ml/x", Pack: "data", Kind: Builtin, Run: noop}) })
	run(t, "unknown pack", func() { Register(Check{ID: "web/x", Pack: "web", Kind: Builtin, Run: noop}) })
	run(t, "external without tool", func() { Register(Check{ID: "data/x1", Pack: "data", Kind: External, Run: noop}) })
	run(t, "duplicate key", func() {
		RegisterPack(Pack{Name: "data", ConfigKeys: map[string]func(yaml.Node) error{"example_cap": nil}})
	})
	run(t, "framework key", func() { RegisterPack(Pack{Name: "data", ConfigKeys: map[string]func(yaml.Node) error{"tools": nil}}) })
	run(t, "duplicate flag", func() {
		RegisterPack(Pack{Name: "data", Flags: []Flag{{Name: "example_sql", Raise: func(*Input) bool { return false }}}})
	})
	run(t, "reviewer-only on an unknown flag", func() {
		RegisterPack(Pack{Name: "db", Flags: []Flag{{Name: "migrations", Reviewers: []string{"db-reviewer"}}}})
	})
	run(t, "bad hint regex", func() { RegisterPack(Pack{Name: "db", Hints: []Hint{{Glob: "*.sql", Contains: "("}}}) })
	run(t, "duplicate command", func() {
		RegisterPack(Pack{Name: "data", Commands: func() []*cobra.Command { return []*cobra.Command{{Use: "example"}} }})
	})
}

func TestFlagsAndCrew(t *testing.T) {
	cfg := Merge(packsOf(t, "data: { enabled: true }\n"), nil)
	in := &Input{Config: cfg, Changed: []ChangedFile{{Path: "models/a.sql", Status: 'A'}}}
	if got := RaiseFlags(in); !reflect.DeepEqual(got, []string{"example_sql"}) {
		t.Fatalf("raised %v", got)
	}
	if got := Crew(cfg, []string{"migration", "example_sql", "auth"}); !reflect.DeepEqual(got, []string{"db-reviewer", "pipeline-reviewer"}) {
		t.Fatalf("crew %v", got)
	}
	if got := Crew(Merge(nil, nil), []string{"migration"}); len(got) != 0 {
		t.Fatalf("a disabled pack adds no reviewer: %v", got)
	}
	if q := Questions(cfg); len(q) != 1 || q["example_question"].Instructions == "" {
		t.Fatalf("questions %+v", q)
	}
	if q := Questions(Merge(nil, nil)); len(q) != 0 {
		t.Fatal("a disabled pack adds no question")
	}
}

func TestSuggestAndDoctor(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeFile(t, root, "dbt_project.yml", "name: acme\n")
	writeFile(t, root, "train/fit.py", "from sklearn.linear_model import LogisticRegression\n")
	writeFile(t, root, "other.py", "import os\n")
	cfg := Merge(packsOf(t, "cloud: { enabled: true }\n"), nil)
	in := &Input{Root: root, Config: cfg}
	got := Suggest(in, WalkFiles(root, SuggestMaxFiles))
	sort.Strings(got)
	if len(got) != 2 || !strings.HasPrefix(got[0], "data pack: a dbt project") || !strings.Contains(got[1], "train/fit.py") {
		t.Fatalf("suggest %v", got)
	}
	cfg = Merge(packsOf(t, "data: { tools: [dbt] }\n"), nil)
	in = &Input{Root: root, Config: cfg, Tools: fakeTools{}}
	in.SetWorkspace("acme", root)
	fs := Doctor(in, nil, errors.New("tq-packs.yaml is invalid: x"))
	codes := map[string]bool{}
	for _, f := range fs {
		codes[f.Code] = true
	}
	for _, c := range []string{"packs-tool-missing", "packs-repo-file-invalid", "data-example"} {
		if !codes[c] {
			t.Fatalf("missing %s in %+v", c, fs)
		}
	}
}

func TestCommandsRegistered(t *testing.T) {
	cs := Commands("data")
	if len(cs) != 1 || cs[0].Name() != "example" {
		t.Fatalf("%v", cs)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "user.email=tester@acme.test", "-c", "user.name=tester", "-c", "commit.gpgsign=false"}, args...)...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func TestSelectOverGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolate(t)
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	writeFile(t, root, "a.sql", "select 1")
	writeFile(t, root, "b.sql", "select 2")
	writeFile(t, root, "old.sql", "select 3")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "init")
	gitRun(t, root, "tag", "base")
	writeFile(t, root, "a.sql", "select 10")
	writeFile(t, root, "new.sql", "select 4")
	gitRun(t, root, "mv", "old.sql", "renamed.sql")
	os.Remove(filepath.Join(root, "b.sql"))
	writeFile(t, root, "staged.sql", "x")
	gitRun(t, root, "add", "staged.sql")
	writeFile(t, root, ".gitignore", "ignored.txt\n")
	writeFile(t, root, "ignored.txt", "x")
	ctx := context.Background()

	set := func(cs []ChangedFile) map[string]byte {
		m := map[string]byte{}
		for _, c := range cs {
			m[c.Path] = c.Status
		}
		return m
	}
	cs, base, err := Select(ctx, root, Selection{})
	m := set(cs)
	if err != nil || base != "HEAD" || m["a.sql"] != 'M' || m["b.sql"] != 'D' || m["new.sql"] != 'A' || m["staged.sql"] != 'A' || m[".gitignore"] != 'A' {
		t.Fatalf("default: %v %s %v", err, base, m)
	}
	if _, ok := m["ignored.txt"]; ok {
		t.Fatal("ignored files are not changed")
	}
	cs, base, _ = Select(ctx, root, Selection{Staged: true})
	m = set(cs)
	if base != "HEAD" || m["staged.sql"] != 'A' || m["renamed.sql"] == 0 {
		t.Fatalf("staged: %v", m)
	}
	if _, ok := m["a.sql"]; ok {
		t.Fatal("unstaged change in --staged")
	}
	cs, base, _ = Select(ctx, root, Selection{Base: "base"})
	if base != "base" || set(cs)["a.sql"] != 'M' {
		t.Fatalf("base: %v", cs)
	}
	cs, _, _ = Select(ctx, root, Selection{Paths: []string{"a.sql", "new.sql"}})
	if m = set(cs); m["a.sql"] != 'M' || m["new.sql"] != 'A' {
		t.Fatalf("paths: %v", m)
	}
	cs, base, _ = Select(ctx, root, Selection{All: true})
	if m = set(cs); base != "" || m["b.sql"] != 'A' || len(m) < 3 {
		t.Fatalf("all: %s %v", base, m)
	}
	if _, _, err := Select(ctx, root, Selection{All: true, Staged: true}); !errors.Is(err, ErrSelection) {
		t.Fatalf("conflict: %v", err)
	}
	if _, _, err := Select(ctx, root, Selection{Paths: []string{"../x"}}); !errors.Is(err, ErrSelection) {
		t.Fatalf("escape: %v", err)
	}
	g := GitRepo{Root: root}
	if b, err := g.Show("HEAD", "a.sql"); err != nil || string(b) != "select 1" {
		t.Fatalf("show: %q %v", b, err)
	}
	if b, err := g.Show(":", "staged.sql"); err != nil || string(b) != "x" {
		t.Fatalf("index: %q %v", b, err)
	}
	in := &Input{Root: root, Base: "HEAD", Git: g}
	if b, _ := in.ReadOld("a.sql"); string(b) != "select 1" {
		t.Fatal("ReadOld")
	}
	if b, _ := in.ReadNew("a.sql", 0); string(b) != "select 10" {
		t.Fatal("ReadNew")
	}
	_ = policy.Allow
}

func TestMatchGlob(t *testing.T) {
	for _, c := range []struct {
		g, p string
		ok   bool
	}{{"*.sql", "models/a.sql", true}, {"models/", "models/x/y.sql", true}, {"models/**/*.sql", "models/a.sql", true}, {"dbt_project.yml", "sub/dbt_project.yml", true}, {"contracts/*.yaml", "x/contracts/a.yaml", false}} {
		if MatchGlob(c.g, c.p) != c.ok {
			t.Fatalf("%s %s", c.g, c.p)
		}
	}
}
