package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/secret"
)

// secretCmdValue is built at run time so no token-shaped literal sits in the source.
func secretCmdValue() string { return "tq" + strings.Repeat("Hx7", 9) }

// secretWS is a trusted workspace "acme" with a jira tracker, an isolated home, no agent markers
// and go-keyring's in-memory mock for the keychain.
func secretWS(t *testing.T) string {
	t.Helper()
	root := setupTrustedWorkspaceWithManifest(t, "acme", "tracker:\n  provider: jira\n  auth: keychain\n  scope: { org: acme }\n")
	home := filepath.Dir(os.Getenv("TQ_HOME"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK"} {
		unsetenv(t, k)
	}
	secret.MockInit()
	return root
}

func noValue(t *testing.T, outs ...string) {
	t.Helper()
	for _, o := range outs {
		if strings.Contains(o, secretCmdValue()) || strings.Contains(o, secretCmdValue()[:12]) {
			t.Fatalf("a token value was printed: %q", o)
		}
	}
}

func TestSecretSetStatusRm(t *testing.T) {
	secretWS(t)
	out, errOut, err := runTQIn(t, strings.NewReader(secretCmdValue()+"\n"), "secret", "set", "acme", "jira")
	if err != nil {
		t.Fatal(err, errOut)
	}
	noValue(t, out, errOut)
	if !strings.Contains(out, "acme/jira") {
		t.Fatalf("set output %q", out)
	}
	if v, err := secret.Get("acme", "jira"); err != nil || v != secretCmdValue() {
		t.Fatalf("not stored: %v", err)
	}

	out, errOut, err = runTQIn(t, strings.NewReader(""), "secret", "status")
	if err != nil {
		t.Fatal(err)
	}
	noValue(t, out, errOut)
	if !strings.Contains(out, "acme") || !strings.Contains(out, "jira") || !strings.Contains(out, "present") {
		t.Fatalf("status %q", out)
	}

	out, errOut, err = runTQIn(t, strings.NewReader(""), "secret", "status", "acme", "--json")
	if err != nil {
		t.Fatal(err)
	}
	noValue(t, out, errOut)
	var rows []map[string]string
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(rows) != 1 || rows[0]["workspace"] != "acme" || rows[0]["provider"] != "jira" ||
		rows[0]["account"] != "acme/jira" || rows[0]["keychain"] != "present" || rows[0]["auth"] != "keychain" {
		t.Fatalf("rows %v", rows)
	}

	out, errOut, err = runTQIn(t, strings.NewReader(""), "secret", "rm", "acme", "jira")
	if err != nil {
		t.Fatal(err)
	}
	noValue(t, out, errOut)
	if ok, _ := secret.Has("acme", "jira"); ok {
		t.Fatal("rm left the token")
	}
	out, _, _ = runTQIn(t, strings.NewReader(""), "secret", "status", "acme", "jira", "--json")
	if !strings.Contains(out, `"absent"`) {
		t.Fatalf("status after rm %q", out)
	}
	if _, _, err := runTQIn(t, strings.NewReader(""), "secret", "rm", "acme", "jira"); err == nil || !strings.Contains(err.Error(), "no token") {
		t.Fatalf("rm of nothing: %v", err)
	}
}

// TestSecretStatusWithoutATracker: a workspace with no tracker block shows every tracker.
func TestSecretStatusProvidersOfAWorkspaceWithoutTracker(t *testing.T) {
	setupTrustedWorkspaceWithManifest(t, "acme", "")
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK"} {
		unsetenv(t, k)
	}
	secret.MockInit()
	secret.Set("acme", "linear", secretCmdValue())
	out, errOut, err := runTQIn(t, strings.NewReader(""), "secret", "status", "acme", "--json")
	if err != nil {
		t.Fatal(err)
	}
	noValue(t, out, errOut)
	var rows []map[string]string
	json.Unmarshal([]byte(out), &rows)
	if len(rows) != 5 {
		t.Fatalf("rows %v", rows)
	}
	for _, r := range rows {
		if (r["provider"] == "linear") != (r["keychain"] == "present") {
			t.Fatalf("row %v", r)
		}
	}
	// status of all workspaces lists only workspaces with a tracker: none here.
	out, _, err = runTQIn(t, strings.NewReader(""), "secret", "status", "--json")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestSecretRefusesBadNames(t *testing.T) {
	secretWS(t)
	cases := [][]string{
		{"secret", "set", "nope", "jira"},
		{"secret", "set", "acme", "trello"},
		{"secret", "rm", "nope", "jira"},
		{"secret", "status", "nope"},
	}
	for _, args := range cases {
		in := &countingReader{}
		_, _, err := runTQIn(t, in, args...)
		if err == nil {
			t.Fatalf("%v accepted", args)
		}
		if in.reads != 0 {
			t.Fatalf("%v read stdin before checking names", args)
		}
	}
}

func TestSecretSetRefusesAnEmptyToken(t *testing.T) {
	secretWS(t)
	if _, _, err := runTQIn(t, strings.NewReader("\n"), "secret", "set", "acme", "jira"); err == nil {
		t.Fatal("empty token stored")
	}
	if ok, _ := secret.Has("acme", "jira"); ok {
		t.Fatal("stored")
	}
}

// TestSecretSetHiddenPromptOnATerminal: a terminal stdin is read with echo off, and the prompt
// goes to stderr.
func TestSecretSetHiddenPromptOnATerminal(t *testing.T) {
	secretWS(t)
	prevT, prevR := stdinIsTerminal, readHidden
	t.Cleanup(func() { stdinIsTerminal, readHidden = prevT, prevR })
	stdinIsTerminal = func(io.Reader) bool { return true }
	hidden := 0
	readHidden = func(io.Reader) ([]byte, error) { hidden++; return []byte(secretCmdValue()), nil }
	in := &countingReader{}
	out, errOut, err := runTQIn(t, in, "secret", "set", "acme", "jira")
	if err != nil {
		t.Fatal(err)
	}
	noValue(t, out, errOut)
	if hidden != 1 || in.reads != 0 {
		t.Fatalf("hidden reads %d, plain reads %d", hidden, in.reads)
	}
	if !strings.Contains(errOut, "hidden") {
		t.Fatalf("prompt %q", errOut)
	}
	if v, _ := secret.Get("acme", "jira"); v != secretCmdValue() {
		t.Fatal("not stored")
	}
	readHidden = func(io.Reader) ([]byte, error) { return nil, errors.New("interrupted") }
	if _, _, err := runTQIn(t, in, "secret", "set", "acme", "jira"); err == nil {
		t.Fatal("a failed prompt stored something")
	}
}

// TestSecretIsHumanOnly: each agent marker alone refuses set, rm and status, with the hub's
// human-only wording, before stdin or the keychain is touched.
func TestSecretIsHumanOnly(t *testing.T) {
	secretWS(t)
	secret.Set("acme", "jira", secretCmdValue())
	markers := [][2]string{{"TENTAQLES_HEADLESS", "1"}, {"CLAUDECODE", "1"}, {"CLAUDE_PLUGIN_ROOT", "/p"}, {"TQ_HUB_TASK", `{"hub":"c1"}`}}
	for _, m := range markers {
		t.Run(m[0], func(t *testing.T) {
			t.Setenv(m[0], m[1])
			for _, args := range [][]string{
				{"secret", "set", "acme", "jira"},
				{"secret", "rm", "acme", "jira"},
				{"secret", "status"},
				{"secret", "status", "acme", "--json"},
			} {
				in := &countingReader{}
				out, errOut, err := runTQIn(t, in, args...)
				if !errors.Is(err, runner.ErrHumanOnly) {
					t.Fatalf("%v: %v", args, err)
				}
				if in.reads != 0 {
					t.Fatalf("%v read stdin", args)
				}
				noValue(t, out, errOut, err.Error())
			}
		})
	}
	if v, _ := secret.Get("acme", "jira"); v != secretCmdValue() {
		t.Fatal("an agent session changed the keychain")
	}
}
