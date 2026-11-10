package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.HomeRun(m)) }

// fixture is the test home: a temp folder, TQ_HOME inside it, the clients folder holding acme and
// globex, and a neutral folder that is no client.
type fixture struct {
	tqHome  string
	clients string
	neutral string
}

// newHome builds the two test clients by hand and points the environment at them. Nothing here touches
// the developer's home folder.
func newHome(t *testing.T) fixture {
	t.Helper()
	tmp := t.TempDir()
	f := fixture{
		tqHome:  filepath.Join(tmp, "home", ".tentaqles"),
		clients: filepath.Join(tmp, "clients"),
		neutral: filepath.Join(tmp, "neutral"),
	}
	t.Setenv("TQ_HOME", f.tqHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	t.Setenv("USERPROFILE", filepath.Join(tmp, "home"))
	for _, name := range []string{"acme", "globex"} {
		mustWrite(t, filepath.Join(f.identity(name), "manifest.yaml"),
			"schema: tentaqles-client-v2\nclient: "+name+"\ngit: { email: dev@"+name+".test }\nidentities: { claude: {} }\n")
		mustMkdir(t, filepath.Join(f.identity(name), "claude"))
		mustMkdir(t, filepath.Join(f.identity(name), "codex"))
		mustWrite(t, filepath.Join(f.client(name), ".tentaqles.yaml"), "schema: tentaqles-marker-v1\nclient: "+name+"\n")
		mustMkdir(t, filepath.Join(f.client(name), "app"))
	}
	mustMkdir(t, f.neutral)
	mustWrite(t, filepath.Join(f.tqHome, "config.yaml"), fmt.Sprintf(
		"bases:\n  - %s\nclients:\n  acme: %s\n  globex: %s\n",
		filepath.ToSlash(f.clients), filepath.ToSlash(f.client("acme")), filepath.ToSlash(f.client("globex"))))
	return f
}

// identity is the identity folder of a client, $TQ_HOME/identities/<name>.
func (f fixture) identity(name string) string { return filepath.Join(f.tqHome, "identities", name) }

// client is the client folder of a client under the clients folder.
func (f fixture) client(name string) string { return filepath.Join(f.clients, name) }

// deps is the Deps of a test: the agent flag, a fake environment and the folder the command runs in.
func deps(inAgent bool, env map[string]string, cwd string) Deps {
	return Deps{
		InAgent: func() bool { return inAgent },
		Getenv:  func(k string) string { return env[k] },
		Getwd:   func() (string, error) { return cwd, nil },
	}
}

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
}

// wantClient asserts Target gives the named client with no error.
func wantClient(t *testing.T, d Deps, flag, name string) {
	t.Helper()
	ws, err := Target(d, flag)
	if err != nil {
		t.Fatalf("Target(client=%q): unexpected error: %v", flag, err)
	}
	if ws.Name != name {
		t.Fatalf("Target(client=%q) = %s, want %s", flag, ws.Name, name)
	}
}

// wantRefused asserts Target refuses, and that the refusal does not reveal whether a name exists.
func wantRefused(t *testing.T, d Deps, flag string) {
	t.Helper()
	ws, err := Target(d, flag)
	if err == nil {
		t.Fatalf("Target(client=%q) = %s, want a refusal", flag, ws.Name)
	}
	if strings.Contains(err.Error(), "no client") {
		t.Errorf("refusal leaks a client lookup: %v", err)
	}
}

func TestTargetInClientFolder(t *testing.T) {
	f := newHome(t)
	wantClient(t, deps(false, nil, f.client("acme")), "", "acme")
	wantClient(t, deps(false, nil, filepath.Join(f.client("acme"), "app")), "", "acme")
}

func TestTargetWithClientFlagOutsideAgent(t *testing.T) {
	f := newHome(t)
	wantClient(t, deps(false, nil, f.client("acme")), "acme", "acme")
	wantClient(t, deps(false, nil, f.client("acme")), "globex", "globex")
	wantClient(t, deps(false, nil, f.neutral), "acme", "acme")
	wantClient(t, deps(false, nil, f.client("acme")), f.client("globex"), "globex")
}

// A --client value without a path separator is a client's name, even when the current folder has a
// subfolder of that name: in acme's folder, a subfolder globex does not make --client globex a folder.
func TestTargetClientNameIsNotAFolderOfTheCurrentDirectory(t *testing.T) {
	f := newHome(t)
	mustMkdir(t, filepath.Join(f.client("acme"), "globex"))
	// The process runs in acme's folder too: a name must not be looked up as a folder of the working directory.
	t.Chdir(f.client("acme"))
	wantClient(t, deps(false, nil, f.client("acme")), "globex", "globex")
	// Named by its path, the same subfolder is a folder, and a folder inside acme's folder is acme.
	wantClient(t, deps(false, nil, f.neutral), filepath.Join(f.client("acme"), "globex"), "acme")
}

func TestTargetNeutralFolderWithoutFlag(t *testing.T) {
	f := newHome(t)
	_, err := Target(deps(false, nil, f.neutral), "")
	if err == nil {
		t.Fatal("a folder that is no client must be an error without --client")
	}
	if !strings.Contains(err.Error(), "--client") {
		t.Errorf("error should tell the person to use --client: %v", err)
	}
}

func TestTargetOutsideAgentUnknownClient(t *testing.T) {
	f := newHome(t)
	_, err := Target(deps(false, nil, f.client("acme")), "nobody")
	if err == nil || !strings.Contains(err.Error(), "tentaqles list") {
		t.Fatalf("an unknown client should be an error that names tentaqles list, got %v", err)
	}
}

func TestTargetInAgentOwnClient(t *testing.T) {
	f := newHome(t)
	wantClient(t, deps(true, nil, f.client("acme")), "", "acme")
	wantClient(t, deps(true, nil, f.client("acme")), "acme", "acme")
	wantClient(t, deps(true, nil, filepath.Join(f.client("acme"), "app")), "acme", "acme")
}

func TestTargetInAgentOtherClientRefused(t *testing.T) {
	f := newHome(t)
	wantRefused(t, deps(true, nil, f.client("acme")), "globex")
	wantRefused(t, deps(true, nil, f.client("acme")), f.client("globex"))
}

func TestTargetInAgentNeutralFolderRefused(t *testing.T) {
	f := newHome(t)
	wantRefused(t, deps(true, nil, f.neutral), "acme")
	wantRefused(t, deps(true, nil, f.neutral), "globex")
}

func TestTargetInAgentUnknownNameRefusedWithoutLookup(t *testing.T) {
	f := newHome(t)
	wantRefused(t, deps(true, nil, f.client("acme")), "nobody")
}

func TestCheckLoginRefusesOtherLogin(t *testing.T) {
	f := newHome(t)
	acme, err := Target(deps(false, nil, f.client("acme")), "acme")
	if err != nil {
		t.Fatal(err)
	}
	globexClaude := paths.IdentityDir("globex", "claude")
	globexCodex := paths.IdentityDir("globex", "codex")
	cases := map[string]map[string]string{
		"claude set to globex": {"CLAUDE_CONFIG_DIR": globexClaude},
		"codex set to globex":  {"CODEX_HOME": globexCodex},
		"claude ok, codex globex": {
			"CLAUDE_CONFIG_DIR": paths.IdentityDir("acme", "claude"),
			"CODEX_HOME":        globexCodex,
		},
	}
	for name, env := range cases {
		if err := CheckLogin(deps(true, env, f.client("acme")), acme); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
}

func TestCheckLoginOwnOrUnsetAllowed(t *testing.T) {
	f := newHome(t)
	acme, err := Target(deps(false, nil, f.client("acme")), "acme")
	if err != nil {
		t.Fatal(err)
	}
	own := map[string]map[string]string{
		"claude own":        {"CLAUDE_CONFIG_DIR": paths.IdentityDir("acme", "claude")},
		"claude own, slash": {"CLAUDE_CONFIG_DIR": paths.IdentityDir("acme", "claude") + string(filepath.Separator)},
		"codex own":         {"CODEX_HOME": paths.IdentityDir("acme", "codex")},
		"both unset":        nil,
	}
	for name, env := range own {
		if err := CheckLogin(deps(true, env, f.client("acme")), acme); err != nil {
			t.Errorf("%s: want no refusal, got %v", name, err)
		}
	}
}

func TestCheckLoginOutsideAgentAlwaysFine(t *testing.T) {
	f := newHome(t)
	acme, err := Target(deps(false, nil, f.client("acme")), "acme")
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"CLAUDE_CONFIG_DIR": paths.IdentityDir("globex", "claude"),
		"CODEX_HOME":        paths.IdentityDir("globex", "codex"),
	}
	if err := CheckLogin(deps(false, env, f.client("acme")), acme); err != nil {
		t.Errorf("outside an agent session every login is fine, got %v", err)
	}
}

func TestRefusePerson(t *testing.T) {
	if err := RefusePerson(deps(true, nil, "")); err == nil {
		t.Error("inside an agent session a person-only verb must refuse")
	}
	if err := RefusePerson(deps(false, nil, "")); err != nil {
		t.Errorf("outside an agent session a person-only verb may run, got %v", err)
	}
}
