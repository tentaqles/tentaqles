package cmd

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
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

// --- spec E §8.1, §8.4: tq env --off / --on, and tq env under off -------

func TestEnv_PrintsNothingWhenOff(t *testing.T) {
	for _, kv := range [][2]string{{"TQ_MODE", "off"}, {"TQ_ENABLED", "0"}} {
		t.Run(kv[0], func(t *testing.T) {
			root := mkLaunchWS(t, "acme", "")
			chdir(t, root)
			t.Setenv(kv[0], kv[1])
			if got := runEnv(t, "env", "--shell", "bash"); got != "" {
				// The output holds env values: never print it (rules).
				t.Fatalf("tq env under %s=%s must print nothing, got %d bytes", kv[0], kv[1], len(got))
			}
		})
	}
}

func TestEnv_Off(t *testing.T) {
	root := mkLaunchWS(t, "acme", "")
	clearAgentMarkers(t)
	chdir(t, root)
	st := envplan.State{WS: "acme", Prev: map[string]*string{"TQ_WS": nil, "CLAUDE_CONFIG_DIR": nil}}
	t.Setenv(envplan.StateVar, st.Encode())
	t.Setenv("TQ_WS", "acme")
	t.Setenv("CLAUDE_CONFIG_DIR", "/id/acme/claude")
	got := runEnv(t, "env", "--off", "--shell", "bash")
	for _, want := range []string{"unset CLAUDE_CONFIG_DIR", "unset TQ_WS", "unset " + envplan.StateVar, "export TQ_MODE='off'"} {
		if !strings.Contains(got, want) {
			t.Errorf("tq env --off: missing %q in %q", want, got)
		}
	}
	// Already off: --off still prints the code (idempotent).
	t.Setenv("TQ_MODE", "off")
	if got := runEnv(t, "env", "--off", "--shell", "pwsh"); !strings.Contains(got, "$env:TQ_MODE = 'off'") {
		t.Fatalf("pwsh: %q", got)
	}
}

func TestEnv_OffRefusesAgents(t *testing.T) {
	mkLaunchWS(t, "acme", "")
	for _, k := range agentMarkers {
		clearAgentMarkers(t)
		t.Setenv(k, "1")
		if _, err := runTq(t, "env", "--off", "--shell", "bash"); !errors.Is(err, runner.ErrHumanOnly) {
			t.Errorf("%s: want ErrHumanOnly, got %v", k, err)
		}
	}
}

func TestEnv_On(t *testing.T) {
	isolateHome(t)
	chdir(t, t.TempDir())
	t.Setenv("TQ_MODE", "off")
	if got := runEnv(t, "env", "--on", "--shell", "bash"); got != "unset TQ_MODE\n" {
		t.Fatalf("bash: %q", got)
	}
	if got := runEnv(t, "env", "--on", "--shell", "fish"); got != "set -e TQ_MODE\n" {
		t.Fatalf("fish: %q", got)
	}
	if _, err := runTq(t, "env", "--on", "--off"); err == nil {
		t.Fatal("--on with --off must be refused")
	}
}
