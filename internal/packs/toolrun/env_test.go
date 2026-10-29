package toolrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[strings.ToUpper(k)] = v
	}
	return m
}

// Round 5 (spec C §2.6, spec D §20): a tool sees the base allow-list, the
// locale, the names its check declares, the telemetry switches and the
// explicit pairs; never the rest of the parent environment.
func TestToolEnvIsFiltered(t *testing.T) {
	isolate(t)
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "C")
	t.Setenv("TQ_TEST_GITHUB_TOKEN", "x")
	t.Setenv("TQ_TEST_DB_PASSWORD", "x")
	t.Setenv("TQ_TEST_RANDOM", "x")
	t.Setenv("DBT_PROFILES_DIR", "/p")
	t.Setenv("PGSERVICE_TOKEN", "declared")
	m := envMap(toolEnv("dbt", []string{"DBT_PROFILES_DIR", "PGSERVICE_TOKEN"}, []string{"EXTRA=1"}))
	for _, k := range []string{"PATH", "LANG", "LC_ALL", "DBT_PROFILES_DIR", "PGSERVICE_TOKEN", "DO_NOT_TRACK", "DBT_SEND_ANONYMOUS_USAGE_STATS", "EXTRA"} {
		if _, ok := m[k]; !ok {
			t.Errorf("%s missing", k)
		}
	}
	for _, k := range []string{"TQ_TEST_GITHUB_TOKEN", "TQ_TEST_DB_PASSWORD", "TQ_TEST_RANDOM"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s passed", k)
		}
	}
	if m["PGSERVICE_TOKEN"] != "declared" {
		t.Error("a declared secret-like name must pass")
	}
	// Undeclared, the declared names do not pass either.
	m = envMap(toolEnv("", nil, nil))
	if _, ok := m["DBT_PROFILES_DIR"]; ok {
		t.Error("an undeclared name passed")
	}
}

// The fake tool sees only what toolEnv passes (end to end).
func TestRunPassesOnlyTheFilteredEnv(t *testing.T) {
	isolate(t)
	bin := fakeBin(t, t.TempDir(), "dbt")
	r := &Runner{Root: t.TempDir(), LookPath: func(string) (string, error) { return bin, nil }}
	t.Setenv("TQ_FAKE_TOOL", "env")
	t.Setenv("TQ_TEST_GITHUB_TOKEN", "leak")
	t.Setenv("DBT_PROFILES_DIR", "/p")
	out, err := r.Run(context.Background(), Spec{Pack: "data", Tool: "dbt", PassEnv: []string{"DBT_PROFILES_DIR"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out.Stdout)
	if strings.Contains(s, "TQ_TEST_GITHUB_TOKEN") || !strings.Contains(s, "DBT_PROFILES_DIR=/p") {
		t.Fatalf("tool env: %s", s)
	}
}

// R5-5: a declared secret never reaches a binary resolved inside the repo
// (.venv); the run warns with the name only. LC_* and LANG values that are
// not locales are dropped.
func TestRepoLocalToolGetsNoDeclaredSecret(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	venv := filepath.Join(root, ".venv", "bin")
	if runtime.GOOS == "windows" {
		venv = filepath.Join(root, ".venv", "Scripts")
	}
	os.MkdirAll(venv, 0o700)
	fakeBin(t, venv, "dbt")
	r := &Runner{Root: root, LookPath: func(string) (string, error) { return "", errors.New("no") }}
	t.Setenv("TQ_FAKE_TOOL", "env")
	t.Setenv("DBT_PASSWORD", "hunter2-value")
	t.Setenv("DBT_PROFILES_DIR", "/p")
	out, err := r.Run(context.Background(), Spec{Pack: "data", Tool: "dbt", PassEnv: []string{"DBT_PASSWORD", "DBT_PROFILES_DIR"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out.Stdout)
	if strings.Contains(s, "hunter2-value") || !strings.Contains(s, "DBT_PROFILES_DIR=/p") {
		t.Fatalf("repo tool env: %s", s)
	}
	if w := strings.Join(out.Warnings, " "); !strings.Contains(w, "DBT_PASSWORD") || strings.Contains(w, "hunter2") {
		t.Fatalf("warnings: %q", out.Warnings)
	}
	// The same declared secret reaches a tool outside the repo.
	bin := fakeBin(t, t.TempDir(), "dbt2")
	r2 := &Runner{Root: root, LookPath: func(string) (string, error) { return bin, nil }}
	out, _ = r2.Run(context.Background(), Spec{Pack: "data", Tool: "dbt2", PassEnv: []string{"DBT_PASSWORD"}})
	if !strings.Contains(string(out.Stdout), "hunter2-value") || len(out.Warnings) != 0 {
		t.Fatalf("outside tool: %v %s", out.Warnings, out.Stdout)
	}
}

func TestLocaleValuesAreFiltered(t *testing.T) {
	isolate(t)
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "C")
	t.Setenv("LC_MESSAGES", "de_DE@euro")
	t.Setenv("LC_TQ_SMUGGLE", "ghp_notalocale value/with=stuff")
	m := envMap(toolEnv("", nil, nil))
	for _, k := range []string{"LANG", "LC_ALL", "LC_MESSAGES"} {
		if _, ok := m[k]; !ok {
			t.Errorf("%s dropped", k)
		}
	}
	if _, ok := m["LC_TQ_SMUGGLE"]; ok {
		t.Error("a non-locale LC_* value passed")
	}
	t.Setenv("LANG", "x y z")
	if _, ok := envMap(toolEnv("", nil, nil))["LANG"]; ok {
		t.Error("a non-locale LANG passed")
	}
}

// R6-3: URL and URI credentials look like secrets, by name or by value.
func TestURLCredentialsAreSecrets(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	venv := filepath.Join(root, ".venv", "bin")
	if runtime.GOOS == "windows" {
		venv = filepath.Join(root, ".venv", "Scripts")
	}
	os.MkdirAll(venv, 0o700)
	fakeBin(t, venv, "dbt")
	r := &Runner{Root: root, LookPath: func(string) (string, error) { return "", errors.New("no") }}
	t.Setenv("TQ_FAKE_TOOL", "env")
	t.Setenv("DATABASE_URL", "postgres://db.example.test/app")
	t.Setenv("REDIS_URI", "redis://cache.example.test")
	t.Setenv("WAREHOUSE_DSN", "x")
	t.Setenv("DBT_TARGET_HOST", "postgres://tester:pw-value@db.example.test/app")
	t.Setenv("DBT_PROFILES_DIR", "/p")
	out, err := r.Run(context.Background(), Spec{Pack: "data", Tool: "dbt", PassEnv: []string{"DATABASE_URL", "REDIS_URI", "WAREHOUSE_DSN", "DBT_TARGET_HOST", "DBT_PROFILES_DIR"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out.Stdout)
	for _, k := range []string{"DATABASE_URL=", "REDIS_URI=", "WAREHOUSE_DSN=", "pw-value"} {
		if strings.Contains(s, k) {
			t.Errorf("%s reached a repo binary", k)
		}
	}
	if !strings.Contains(s, "DBT_PROFILES_DIR=/p") {
		t.Error("a plain declared name was dropped")
	}
	if w := strings.Join(out.Warnings, " "); !strings.Contains(w, "DATABASE_URL") || !strings.Contains(w, "DBT_TARGET_HOST") || strings.Contains(w, "pw-value") {
		t.Errorf("warnings %q", out.Warnings)
	}
	// A base variable with credentials in its value is dropped too.
	t.Setenv("TMPDIR", "https://tester:pw@example.test/")
	if _, ok := envMap(toolEnv("", nil, nil))["TMPDIR"]; ok {
		t.Error("a base variable carrying credentials passed")
	}
}

// R6-3: a path that cannot be resolved counts as repo-local (fail closed).
func TestRepoLocalFailsClosed(t *testing.T) {
	prev := FinalPath
	FinalPath = func(string) (string, error) { return "", errors.New("cannot resolve") }
	defer func() { FinalPath = prev }()
	r := &Runner{Root: t.TempDir()}
	if !r.RepoLocal(Probe{Path: filepath.Join(t.TempDir(), "dbt"), Source: "path"}) {
		t.Fatal("an unresolvable path passed as outside the repo")
	}
	if r.RepoLocal(Probe{Path: "/opt/dbt", Source: "tool_paths"}) {
		t.Fatal("tool_paths is the trusted layer")
	}
}
