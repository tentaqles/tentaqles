package cmd

import (
	"bytes"
	"github.com/zalando/go-keyring"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// isolateHome points TQ_HOME and the OS home at temp dirs so tests never touch
// the real developer home (in particular ~/.gitconfig-tentaqles).
func isolateHome(t *testing.T) string {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
	// The test binary may itself run inside a tq-managed shell (a Claude Code
	// session, `tq run`), which carries the state of the last switch in
	// __TQ_STATE. `tq env` would then "restore" that shell's variables and
	// emit unsets for a neutral cwd, so start every test from a clean slate.
	unsetenv(t, envplan.StateVar)
	keyring.MockInit() // never the real OS keychain in tests
	return home
}

// unsetenv removes k from the process environment for the duration of the
// test (t.Setenv can only set, and an empty value is still "present" to
// os.LookupEnv).
func unsetenv(t *testing.T, k string) {
	t.Helper()
	v, ok := os.LookupEnv(k)
	if !ok {
		return
	}
	if err := os.Unsetenv(k); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv(k, v) })
}

// mkUntrusted registers a base and writes a workspace manifest by hand, without
// ever calling tq allow.
func mkUntrusted(t *testing.T, name string) string {
	t.Helper()
	isolateHome(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, manifest.FileName),
		[]byte("schema: tentaqles-client-v2\nclient: "+name+"\ngit: { email: "+name+"@x.io }\nidentities: { gh: {} }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRun_RefusesUntrusted(t *testing.T) {
	mkUntrusted(t, "evil")
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"run", "evil", "--", "cmd"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for an untrusted workspace")
	}
	if !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("error must mention untrusted: %v", err)
	}
}

func TestLogin_RefusesUntrusted(t *testing.T) {
	mkUntrusted(t, "evil")
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"login", "evil", "gh"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for an untrusted workspace")
	}
	if !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("error must mention untrusted: %v", err)
	}
}

func ws(t *testing.T, mode string) *resolve.Workspace {
	t.Helper()
	return &resolve.Workspace{
		Name: "acme",
		Hash: "hash-" + mode,
		Manifest: &manifest.Manifest{
			Schema: "tentaqles-client-v2", Client: "acme",
			Claude: manifest.Claude{PermissionMode: mode},
		},
	}
}

func TestClaudeArgs(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := []string{"-p", "hi"}

	t.Run("empty mode passes args through", func(t *testing.T) {
		var warned []string
		got := claudeArgs(ws(t, ""), base, func(s string) { warned = append(warned, s) })
		if strings.Join(got, " ") != "-p hi" || len(warned) != 0 {
			t.Fatalf("got %v warned %v", got, warned)
		}
	})

	t.Run("bypass without allow downgrades to acceptEdits and warns", func(t *testing.T) {
		var warned []string
		got := claudeArgs(ws(t, "bypass"), base, func(s string) { warned = append(warned, s) })
		if strings.Join(got, " ") != "--permission-mode acceptEdits -p hi" {
			t.Fatalf("got %v", got)
		}
		if len(warned) != 1 || !strings.Contains(warned[0], "bypass") {
			t.Fatalf("expected a bypass warning, got %v", warned)
		}
	})

	t.Run("bypass with allow passes the skip flag", func(t *testing.T) {
		w := ws(t, "bypass")
		if err := trust.AllowBypass(w.Hash); err != nil {
			t.Fatal(err)
		}
		var warned []string
		got := claudeArgs(w, base, func(s string) { warned = append(warned, s) })
		if strings.Join(got, " ") != "--dangerously-skip-permissions -p hi" {
			t.Fatalf("got %v", got)
		}
		if len(warned) != 0 {
			t.Fatalf("no warning expected, got %v", warned)
		}
	})

	t.Run("allowlisted mode is forwarded", func(t *testing.T) {
		got := claudeArgs(ws(t, "plan"), base, func(string) {})
		if strings.Join(got, " ") != "--permission-mode plan -p hi" {
			t.Fatalf("got %v", got)
		}
	})
}
