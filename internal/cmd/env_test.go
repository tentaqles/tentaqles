package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/envplan"
)

// chdir moves into dir for the duration of the test. env.go reads os.Getwd, so
// these tests mutate process state and must not run in parallel.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func runEnv(t *testing.T, args ...string) string {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestEnv_NeutralCwd_EmitsNothingWithoutState(t *testing.T) {
	isolateHome(t)
	chdir(t, t.TempDir())
	if got := runEnv(t, "env", "--shell", "bash"); got != "" {
		t.Fatalf("neutral cwd with no prior state must emit nothing, got %q", got)
	}
}

func TestEnv_NeutralCwd_UnsetsTrackedVars(t *testing.T) {
	isolateHome(t)
	chdir(t, t.TempDir())
	state := envplan.State{WS: "a", Prev: map[string]*string{"TQ_WS": nil}}
	t.Setenv(envplan.StateVar, state.Encode())
	t.Setenv("TQ_WS", "a")

	got := runEnv(t, "env", "--shell", "bash")
	if !strings.Contains(got, "unset TQ_WS") {
		t.Fatalf("leaving a workspace must unset TQ_WS, got %q", got)
	}
	if !strings.Contains(got, "unset "+envplan.StateVar) {
		t.Fatalf("the state var must be cleared too, got %q", got)
	}
}
