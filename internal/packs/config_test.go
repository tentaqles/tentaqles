package packs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
  baseline: ./.tq\base.json
  example_paths: [models/]
  example_cap: 10GB
  example_floor: 0.5
  example_command: python a.py
  example_block: { prod: { limit: 100, strict: true, apply: ask } }
cloud: { enabled: true }
`)
	repo := packsOf(t, `
data:
  enabled: false
  tools: [sqlfluff]
  timeouts: { dbt: 900s, sqlfluff: 60s }
  tool_paths: { dbt: /tmp/evil }
  baseline: other.json
  example_paths: [snapshots/]
  example_cap: 20GB
  example_floor: 0.8
  example_command: python b.py
  example_block: { prod: { limit: 50, strict: false, apply: auto }, dev: { limit: 1 } }
ml: { enabled: true, tools: [ruff] }
`)
	c := Merge(trusted, repo)
	d := c.Pack("data")
	if !d.Enabled || !reflect.DeepEqual(d.Tools, []string{"dbt", "sqlfluff"}) {
		t.Fatalf("enabled from the manifest only, tools union: %+v", d)
	}
	if d.Timeouts["dbt"] != 600*time.Second || d.Timeouts["sqlfluff"] != 60*time.Second {
		t.Fatalf("timeouts min: %v", d.Timeouts)
	}
	if d.ToolPaths["dbt"] != "C:/tools/dbt.exe" || d.Baseline != ".tq/base.json" {
		t.Fatalf("tool_paths and baseline are trusted only, the baseline normalised: %v %q", d.ToolPaths, d.Baseline)
	}
	if got := d.Strings("example_paths"); !reflect.DeepEqual(got, []string{"models/", "snapshots/"}) {
		t.Fatalf("path union: %v", got)
	}
	if d.String("example_cap") != "10GB" || d.String("example_floor") != "0.8" || d.String("example_command") != "python b.py" {
		t.Fatalf("min/max/repo: cap %s floor %s cmd %s", d.String("example_cap"), d.String("example_floor"), d.String("example_command"))
	}
	if d.String("example_block.prod.limit") != "50" || d.String("example_block.prod.strict") != "true" || d.String("example_block.prod.apply") != "ask" || d.String("example_block.dev.limit") != "1" {
		t.Fatalf("nested: limit %v strict %v apply %v dev %v", d.String("example_block.prod.limit"), d.String("example_block.prod.strict"), d.String("example_block.prod.apply"), d.String("example_block.dev.limit"))
	}
	if !c.IsEnabled("data") || !c.IsEnabled("cloud") || c.IsEnabled("ml") || c.IsEnabled("db") {
		t.Fatalf("the repo cannot enable a pack: %v", c.Enabled())
	}
	want := []string{"data.baseline", "data.enabled", "data.example_block.prod.apply", "data.example_block.prod.strict", "data.example_cap", "data.timeouts.dbt", "data.tool_paths", "ml."}
	if got := problemKeys(c); !reflect.DeepEqual(got, want) {
		t.Fatalf("problems %v want %v", got, want)
	}
	if d.Node("enabled") != nil || d.Node("tools") != nil {
		t.Fatal("framework keys live in the typed fields, not Raw")
	}
}

// F1: when the manifest is silent, a bound compares against the pack
// default; Or accepts only true; ordered values only tighten; unregistered
// keys are trusted-layer only.
func TestMergeAbsentKeysUseThePackDefault(t *testing.T) {
	trusted := packsOf(t, "data: { enabled: true }\n")
	for name, tc := range map[string]struct {
		repo, key, want string
		problem         bool
	}{
		"min under the default":       {"example_cap: 10GB", "example_cap", "10GB", false},
		"min over the default":        {"example_cap: 100GB", "example_cap", "", true},
		"zero means unlimited":        {"example_cap: 0", "example_cap", "", true},
		"negative cap":                {"example_cap: -1", "example_cap", "", true},
		"max above the default":       {"example_floor: 0.9", "example_floor", "0.9", false},
		"max below the default":       {"example_floor: 0.1", "example_floor", "", true},
		"or true":                     {"example_block: { prod: { strict: true } }", "example_block.prod.strict", "true", false},
		"or false":                    {"example_block: { prod: { strict: false } }", "example_block.prod.strict", "", true},
		"nested min over the default": {"example_block: { prod: { limit: 999999 } }", "example_block.prod.limit", "", true},
		"ordered stricter":            {"example_block: { prod: { apply: approve } }", "example_block.prod.apply", "approve", false},
		"ordered looser":              {"example_block: { prod: { apply: auto } }", "example_block.prod.apply", "", true},
		"ordered unknown value":       {"example_block: { prod: { apply: yolo } }", "example_block.prod.apply", "", true},
		"trusted add":                 {"example_extra: x", "example_extra", "x", false},
		"unregistered key":            {"unknown_key: x", "unknown_key", "", true},
	} {
		t.Run(name, func(t *testing.T) {
			c := Merge(trusted, packsOf(t, "data: { "+tc.repo+" }\n"))
			if got := c.Pack("data").String(tc.key); got != tc.want {
				t.Fatalf("%s = %q want %q (problems %v)", tc.key, got, tc.want, c.Problems)
			}
			if (len(c.Problems) > 0) != tc.problem {
				t.Fatalf("problems %v", c.Problems)
			}
		})
	}
	// A repo-only timeout may not exceed the check default (sqlfluff's is 300s here).
	c := Merge(trusted, packsOf(t, "data: { timeouts: { sqlfluff: 1800s } }\n"))
	if _, ok := c.Pack("data").Timeouts["sqlfluff"]; ok || len(c.Problems) != 1 {
		t.Fatalf("repo-only timeout above the check default: %v %v", c.Pack("data").Timeouts, c.Problems)
	}
	c = Merge(trusted, packsOf(t, "data: { timeouts: { sqlfluff: 60s } }\n"))
	if c.Pack("data").Timeouts["sqlfluff"] != 60*time.Second {
		t.Fatal("a lower repo timeout tightens")
	}
	// A union on a mapping is a problem, not garbage.
	c = Merge(packsOf(t, "data: { example_paths: [a] }\n"), packsOf(t, "data: { example_paths: { x: 1 } }\n"))
	if got := c.Pack("data").Strings("example_paths"); !reflect.DeepEqual(got, []string{"a"}) || len(c.Problems) != 1 {
		t.Fatalf("union on a mapping: %v %v", got, c.Problems)
	}
}

func TestLoadConfigRepoFile(t *testing.T) {
	root := t.TempDir()
	m := &manifest.Manifest{Packs: packsOf(t, "data: { enabled: true }\n")}
	c, err := LoadConfig(m, root)
	if err != nil || c.RepoFile != "" || !c.IsEnabled("data") {
		t.Fatalf("%v %+v", err, c)
	}
	writeFile(t, root, "tq-packs.yaml", "data: { tools: [ruff] }\nml: { tools: [ruff] }\n")
	c, err = LoadConfig(m, root)
	if err != nil || c.IsEnabled("ml") || c.RepoFile == "" || !c.Pack("data").Declared("ruff") {
		t.Fatalf("%v %+v", err, c)
	}
	writeFile(t, root, "tq-packs.yaml", "web: {}\n")
	c, err = LoadConfig(m, root)
	if !errors.Is(err, ErrRepoFile) || !c.IsEnabled("data") {
		t.Fatalf("an invalid repo file keeps the manifest config: %v %+v", err, c.Enabled())
	}
	writeFile(t, root, "tq-packs.yaml", "data: { example_paths: notalist }\n")
	if _, err := LoadConfig(m, root); !errors.Is(err, ErrRepoFile) {
		t.Fatalf("validator: %v", err)
	}
	// No trusted manifest: the repo layer is not read at all.
	writeFile(t, root, "tq-packs.yaml", "data: { enabled: true }\n")
	c, err = LoadConfig(nil, root)
	if err != nil || len(c.Enabled()) != 0 || c.RepoFile != "" {
		t.Fatalf("no manifest: %v %+v", err, c)
	}
	// F15: anchors, aliases and merge keys are refused.
	for _, body := range []string{"data: { x: &a 1, y: *a }\n", "data:\n  loop: &x {b: 1}\n", "base: &b {tools: [ruff]}\ndata:\n  <<: *b\n"} {
		writeFile(t, root, "tq-packs.yaml", body)
		if _, err := LoadConfig(m, root); !errors.Is(err, ErrRepoFile) {
			t.Fatalf("%q accepted: %v", body, err)
		}
	}
}

// F2: a non-finite number neither breaks nor collapses the hash.
func TestConfigHashNonFinite(t *testing.T) {
	a := Merge(packsOf(t, "data: { x: .inf }\n"), nil)
	b := Merge(packsOf(t, "data: { x: .inf, tools: [dbt] }\nml: { x: .nan }\n"), nil)
	empty := sha256.Sum256(nil)
	if a.Err() != nil || b.Err() != nil || a.Hash() == "" || a.Hash() == b.Hash() || a.Hash() == "sha256:"+hex.EncodeToString(empty[:]) {
		t.Fatalf("%v %v %s %s", a.Err(), b.Err(), a.Hash(), b.Hash())
	}
	if c := Merge(packsOf(t, "data: { x: '.inf' }\n"), nil); c.Hash() == a.Hash() {
		t.Fatal("a string and a float must hash apart")
	}
	if List(a, "", false).ConfigHash != a.Hash() {
		t.Fatal("list hash")
	}
}

// F3: overlapping merge patterns resolve deterministically, and different
// rules on overlapping patterns panic.
func TestMergePatternResolution(t *testing.T) {
	rules := map[string]MergeRule{"a.*.c": Min(""), "a.b.*": RepoValue(), "a.b.c": Union()}
	for i := 0; i < 50; i++ {
		if r, _ := ruleFor(rules, []string{"a", "b", "c"}); r.Kind != MergeUnion {
			t.Fatal("the exact pattern wins")
		}
		if r, _ := ruleFor(map[string]MergeRule{"a.*.c": Min(""), "a.b.*": Min("")}, []string{"a", "b", "c"}); r.Kind != MergeMin {
			t.Fatal("deterministic")
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("overlapping patterns with different rules must panic")
		}
	}()
	RegisterPack(Pack{Name: "db", Merge: map[string]MergeRule{"q.*.c": Min(""), "q.b.*": RepoValue()}})
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

func TestWalkFilesSkipsFixturesGeneratedStateAndAgentWorktrees(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "infra/main.bicep", "x")
	writeFile(t, root, "internal/iam/testdata/cdk.out/Stack.template.json", "x")
	writeFile(t, root, ".databricks/bundle/dev/terraform/bundle.tf.json", "x")
	writeFile(t, root, ".claude/worktrees/agent-1/infra/main.bicep", "x")
	writeFile(t, root, ".claude/settings.json", "x")
	writeFile(t, root, "ops/worktrees/notes.md", "x")
	got := WalkFiles(root, SuggestMaxFiles)
	sort.Strings(got)
	want := []string{".claude/settings.json", "infra/main.bicep", "ops/worktrees/notes.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
}
