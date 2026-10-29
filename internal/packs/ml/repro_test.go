package ml

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

func TestRegisteredChecks(t *testing.T) {
	want := []string{"ml/data-snapshot", "ml/dvc-status", "ml/env-lock", "ml/leakage", "ml/lock-check", "ml/lock-check-poetry",
		"ml/notebook-outputs", "ml/ruff", "ml/run-tracking", "ml/seeds", "ml/unsafe-deserialize"}
	// Other workstreams register ml checks too: assert ours are present.
	got := map[string]bool{}
	tools := map[string]string{}
	for _, c := range packs.Checks("ml") {
		got[c.ID] = true
		if c.Kind == packs.External {
			tools[c.ID] = c.Tool
		}
	}
	sort.Strings(want)
	for _, id := range want {
		if !got[id] {
			t.Errorf("check %s is not registered", id)
		}
	}
	for id, tool := range map[string]string{"ml/ruff": "ruff", "ml/lock-check": "uv", "ml/lock-check-poetry": "poetry", "ml/dvc-status": "dvc"} {
		if tools[id] != tool {
			t.Errorf("%s tool = %q, want %q", id, tools[id], tool)
		}
	}
}

func packsFrom(t *testing.T, body string) map[string]manifest.PackConfig {
	t.Helper()
	var m map[string]manifest.PackConfig
	if err := yaml.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestConfigValidation(t *testing.T) {
	good := `ml:
  tools: [ruff]
  paths: {train: ["src/train/**"], features: ["src/features/**"], eval: ["src/eval/**"]}
  seed_helper: src/common/seed.py:set_seed
  data_manifest: data/snapshots.yaml
`
	if err := manifest.ValidatePacks(packsFrom(t, good)); err != nil {
		t.Fatalf("valid config refused: %v", err)
	}
	bad := map[string]string{
		"unknown path key": "ml: {paths: {training: [src/**]}}",
		"paths not a map":  "ml: {paths: [src/**]}",
		"glob not a list":  "ml: {paths: {train: src/**}}",
		"dotdot glob":      "ml: {paths: {train: [../other/**]}}",
		"helper no func":   "ml: {seed_helper: src/common/seed.py}",
		"helper absolute":  "ml: {seed_helper: /etc/seed.py:set_seed}",
		"manifest abs":     "ml: {data_manifest: C:/data/snapshots.yaml}",
		"manifest empty":   `ml: {data_manifest: ""}`,
	}
	for name, body := range bad {
		if err := manifest.ValidatePacks(packsFrom(t, body)); err == nil {
			t.Errorf("%s: accepted %q", name, body)
		}
	}
}

func TestPathsMergeUnion(t *testing.T) {
	trusted := packsFrom(t, "ml: {paths: {train: [src/train/**]}}")
	repo := packsFrom(t, "ml: {paths: {train: [notebooks/**], features: [src/features/**]}}")
	s := packs.Merge(trusted, repo).Pack("ml")
	if got := strings.Join(s.Strings("paths.train"), ","); got != "src/train/**,notebooks/**" {
		t.Errorf("paths.train = %q", got)
	}
	if got := strings.Join(s.Strings("paths.features"), ","); got != "src/features/**" {
		t.Errorf("paths.features = %q", got)
	}
}

func flagInput(t *testing.T, cfg string, files map[string]string) *packs.Input {
	t.Helper()
	root := t.TempDir()
	in := &packs.Input{Root: root, Config: packs.Merge(packsFrom(t, cfg), nil)}
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		in.Changed = append(in.Changed, packs.ChangedFile{Path: p, Status: 'M'})
	}
	return in
}

func raised(in *packs.Input) bool {
	for _, f := range packs.RaiseFlags(in) {
		if f == "ml" {
			return true
		}
	}
	return false
}

func TestMLFlag(t *testing.T) {
	lintIsolate(t)
	if !raised(flagInput(t, "ml: {paths: {features: [src/features/**]}}", map[string]string{"src/features/f.py": "x = 1\n"})) {
		t.Error("a change under paths.features does not raise ml")
	}
	if !raised(flagInput(t, "ml: {enabled: true}", map[string]string{"train.py": "model.fit(X, y)\n"})) {
		t.Error("a training file does not raise ml when paths.train is empty")
	}
	if raised(flagInput(t, "ml: {enabled: true}", map[string]string{"app.py": "print('acme')\n"})) {
		t.Error("a plain file raises ml")
	}
	if raised(flagInput(t, "ml: {paths: {train: [src/train/**]}}", map[string]string{"other.py": "model.fit(X, y)\n"})) {
		t.Error("with paths.train set, content outside it raises ml")
	}
	crew := packs.Crew(packs.Merge(packsFrom(t, "ml: {enabled: true}"), nil), []string{"ml"})
	if strings.Join(crew, ",") != "ml-reviewer" {
		t.Errorf("crew = %v", crew)
	}
}

func TestSuppressionNeedsReason(t *testing.T) {
	src := "model.fit(X_test, y_test)  # tq:allow L2:\nmodel.fit(X_test, y_test)  # tq:allow L2: frozen holdout replay for acme\n"
	f, err := parsePy("train.py", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	fs := leakFile(selected{f: f, training: true}, nil)
	if len(fs) != 1 || fs[0].Line != 1 || fs[0].Rule != ruleL2 {
		t.Fatalf("findings = %v, want one L2 at line 1", fs)
	}
}

func TestFindingsCarryNoValues(t *testing.T) {
	// A notebook output with an e-mail must never be echoed into a finding.
	lintIsolate(t)
	in := caseInput(t, filepath.Join("testdata", "repro-notebook", "notebook-output-pii-pos"))
	r := runCase(t, "ml/notebook-outputs", in)
	for _, f := range r.Findings {
		if strings.Contains(f.Message, "tester@acme.test") || strings.Contains(r.Detail, "tester@") {
			t.Fatalf("a value leaked into %v", f)
		}
	}
}

func TestBuiltinChecksSkipWhenNothingApplies(t *testing.T) {
	lintIsolate(t)
	in := flagInput(t, "ml: {enabled: true}", map[string]string{"README.md": "acme\n"})
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"ml"}, BuiltinOnly: true})
	if len(rep.Results) != 0 {
		t.Fatalf("results = %+v, want none for a README change", rep.Results)
	}
}
