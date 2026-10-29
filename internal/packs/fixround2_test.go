package packs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/zalando/go-keyring"
)

func init() {
	// The rule shapes spec D's cloud pack will use (the reviewer's probe).
	RegisterPack(Pack{Name: "cloud", Merge: map[string]MergeRule{
		"environments.*":       TrustedAdd(),
		"environments.*.apply": Ordered("approve", "auto", "ask", "approve", "deny"),
		"approval_ttl":         Min("60m"),
		"mode":                 Ordered("ask", "auto", "ask", "deny"),
		"cap":                  {Kind: MergeMin, Default: "50GB", ZeroUnlimited: true},
		"ratio":                Max("0.5"),
	}})
}

func cloudMerge(t *testing.T, trusted, repo string) Config {
	t.Helper()
	return Merge(packsOf(t, "cloud: { enabled: true"+trusted+" }\n"), packsOf(t, "cloud: { "+repo+" }\n"))
}

// R1: durations compare in the reference's unit; a bare number is seconds.
func TestMergeDurationUnits(t *testing.T) {
	for repo, ok := range map[string]bool{
		"approval_ttl: 86400": false, // 24 h, not 86 µs
		"approval_ttl: 1e9":   false,
		"approval_ttl: 2h":    false,
		"approval_ttl: 30m":   true,
		"approval_ttl: 600":   true, // 10 minutes
		"cap: 10GB":           true,
		"cap: 999999999999":   false, // a bare number against a size: another kind
		"cap: 10m":            false, // a duration against a size
	} {
		c := cloudMerge(t, "", repo)
		key := strings.SplitN(repo, ":", 2)[0]
		if got := c.Pack("cloud").String(key) != ""; got != ok {
			t.Errorf("%s: accepted=%v want %v (problems %v)", repo, got, ok, c.Problems)
		}
	}
}

// R2: a parent rule never hides the child rules (the environments.* probe).
func TestMergeChildRulesUnderAParent(t *testing.T) {
	c := cloudMerge(t, "", "environments: { prod2: { apply: auto } }")
	if got := c.Pack("cloud").String("environments.prod2.apply"); got != "" {
		t.Fatalf("a new environment got apply=%q", got)
	}
	// Q1: an unruled key is trusted-layer only, even in a new environment.
	c = cloudMerge(t, "", "environments: { prod2: { apply: deny, region: x } }")
	if c.Pack("cloud").String("environments.prod2.apply") != "deny" || c.Pack("cloud").String("environments.prod2.region") != "" {
		t.Fatalf("a new environment keeps only its ruled keys: %v", c.Problems)
	}
	c = cloudMerge(t, ", environments: { prod: { apply: approve } }", "environments: { prod: { apply: auto } }")
	if c.Pack("cloud").String("environments.prod.apply") != "approve" {
		t.Fatal("an existing environment loosened")
	}
	// A looser parent kind above other rules panics at registration.
	defer func() {
		if recover() == nil {
			t.Fatal("RepoValue above a bound must panic")
		}
	}()
	RegisterPack(Pack{Name: "db", Merge: map[string]MergeRule{"w": RepoValue(), "w.*.apply": Ordered("ask", "auto", "ask")}})
}

// R4: a blank trusted value means the default; a non-scalar one is kept.
func TestMergeNullAndNonScalarTrusted(t *testing.T) {
	c := cloudMerge(t, ", mode: , approval_ttl: ", "mode: auto, approval_ttl: 999d")
	if c.Pack("cloud").String("mode") == "auto" || c.Pack("cloud").String("approval_ttl") == "999d" {
		t.Fatalf("null trusted values opened the bound: %v", c.Problems)
	}
	c = cloudMerge(t, ", mode: [deny]", "mode: auto")
	if c.Pack("cloud").String("mode") == "auto" {
		t.Fatal("a list trusted value opened the bound")
	}
}

// R5, m-r1-3: Ordered, Min and Max need a Default.
func TestMergeRulesNeedADefault(t *testing.T) {
	for name, r := range map[string]MergeRule{
		"ordered": {Kind: MergeOrdered, Order: []string{"auto", "deny"}},
		"min":     Min(""),
		"max":     Max(""),
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s without a default accepted", name)
				}
			}()
			RegisterPack(Pack{Name: "db", Merge: map[string]MergeRule{"nodefault_" + name: r}})
		}()
	}
}

// R6: with ZeroUnlimited, a trusted 0 (no limit) lets the repo tighten.
func TestMergeZeroUnlimitedTrusted(t *testing.T) {
	c := cloudMerge(t, ", cap: 0", "cap: 10GB")
	if c.Pack("cloud").String("cap") != "10GB" {
		t.Fatalf("tightening an unlimited cap: %v", c.Problems)
	}
}

// R3: a baseline the growth check cannot read is growth; Load caps the size.
func TestBaselineUnreadableIsGrowth(t *testing.T) {
	root := t.TempDir()
	big := `{"entries":[{"check":"a","fingerprint":"sha256:1"},{"check":"a","fingerprint":"sha256:2"}],"pad":"` + strings.Repeat("x", 17<<20) + `"}`
	writeFile(t, root, ".tq/base.json", big)
	m := packsOf(t, "data: { enabled: true, baseline: .tq/base.json }\n")
	in := &Input{Root: root, Base: "HEAD", Config: Merge(m, nil), Changed: []ChangedFile{{Path: ".tq/base.json", Status: 'M'}},
		Git: oldSide{".tq/base.json": `{"entries":[{"check":"a","fingerprint":"sha256:1"}]}`}}
	if !BaselineGrew(in.ForPack("data")) {
		t.Fatal("a baseline over 16 MiB must count as growth")
	}
}

// R7: directory expansion is case-insensitive on Windows and has no
// command-line limit; an empty directory is a usage error.
func TestSelectPathsLargeAndCase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolate(t)
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	writeFile(t, root, "Models/a.sql", "x")
	for i := 0; i < 1500; i++ {
		writeFile(t, root, fmt.Sprintf("dir/some_fairly_long_subdirectory_name/file_number_%04d.sql", i), "x")
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "i")
	cs, _, err := Select(context.Background(), root, Selection{Paths: []string{"dir"}})
	if err != nil || len(cs) != 1500 || cs[0].Status != 'M' {
		t.Fatalf("1500 files: %d %v", len(cs), err)
	}
	if runtime.GOOS == "windows" {
		cs, _, err = Select(context.Background(), root, Selection{Paths: []string{"models/"}})
		if err != nil || len(cs) != 1 || cs[0].Path != "Models/a.sql" || cs[0].Status != 'M' {
			t.Fatalf("case: %+v %v", cs, err)
		}
	}
	os.MkdirAll(filepath.Join(root, "empty"), 0o755)
	if _, _, err := Select(context.Background(), root, Selection{Paths: []string{"empty"}}); !errors.Is(err, ErrSelection) {
		t.Fatalf("empty directory: %v", err)
	}
}

// m-r1-1: %APPDATA% token stores are not ReadLocal roots or are refused.
func TestReadLocalRefusesConfigTokenStores(t *testing.T) {
	home := isolate(t)
	ws := filepath.Join(home, "acme")
	writeFile(t, ws, "x.txt", "x")
	appdata := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("APPDATA", appdata)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	writeFile(t, appdata, "GitHub CLI/hosts.yml", "oauth_token: x")
	writeFile(t, appdata, "Microsoft/UserSecrets/abc/secrets.json", "{}")
	writeFile(t, home, ".config/gh/hosts.yml", "x")
	env := dataEnv(t)
	env.WorkspaceRoot, env.RepoRoot = ws, ws
	g := NewGuardCtx(env, "cloud")
	for _, p := range []string{filepath.Join(appdata, "GitHub CLI", "hosts.yml"), filepath.Join(appdata, "Microsoft", "UserSecrets", "abc", "secrets.json"), filepath.Join(home, ".config", "gh", "hosts.yml")} {
		if _, err := g.ReadLocal(p, 0); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: %v", p, err)
		}
	}
}

// N2 (nit): a nested command group with its own PersistentPreRunE still
// gets the run context (cobra.EnableTraverseRunHooks, set by the CLI).
func TestFromCommandNested(t *testing.T) {
	prev := cobra.EnableTraverseRunHooks
	cobra.EnableTraverseRunHooks = true
	defer func() { cobra.EnableTraverseRunHooks = prev }()
	in := (&Input{Root: "r"}).ForPack("cloud")
	in.SetWorkspace("acme", "w")
	ran := ""
	parent := &cobra.Command{Use: "cloud", PersistentPreRunE: func(c *cobra.Command, _ []string) error {
		c.SetContext(WithInput(c.Context(), in, nil))
		return nil
	}}
	group := &cobra.Command{Use: "plan", PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	leaf := &cobra.Command{Use: "show", RunE: func(c *cobra.Command, _ []string) error {
		got, err := FromCommand(c)
		if err != nil {
			return err
		}
		ran = got.Workspace() + "/" + got.Pack()
		return nil
	}}
	group.AddCommand(leaf)
	parent.AddCommand(group)
	parent.SetArgs([]string{"plan", "show"})
	if err := parent.ExecuteContext(context.Background()); err != nil || ran != "acme/cloud" {
		t.Fatalf("%v %q", err, ran)
	}
}

// Fix round 3: with no usable keychain, doctor says pack approvals cannot be granted.
func TestDoctorKeychainMissing(t *testing.T) {
	isolate(t)
	keyring.MockInitWithError(errors.New("no secret service"))
	defer keyring.MockInit()
	in := &Input{Root: t.TempDir(), Config: Merge(packsOf(t, "ml: { enabled: true }\n"), nil), Tools: fakeTools{}}
	in.SetWorkspace("acme", in.Root)
	found := false
	for _, f := range Doctor(in, nil, nil) {
		if f.Code == "packs-keychain-missing" {
			found = true
		}
	}
	if !found {
		t.Fatal("no packs-keychain-missing")
	}
}

// Q1: under an environment the manifest defines, an unruled key is refused.
func TestMergeUnruledKeysUnderAnExistingParent(t *testing.T) {
	for _, repo := range []string{
		"environments: { prod: { destroy: auto } }",
		"environments: { prod: { target: { subscription: other } } }",
	} {
		c := cloudMerge(t, ", environments: { prod: { apply: approve, target: { subscription: mine } } }", repo)
		d := c.Pack("cloud")
		if d.String("environments.prod.destroy") != "" || d.String("environments.prod.target.subscription") != "mine" || len(c.Problems) == 0 {
			t.Fatalf("%s: destroy=%q subscription=%q problems=%v", repo, d.String("environments.prod.destroy"), d.String("environments.prod.target.subscription"), c.Problems)
		}
	}
}
