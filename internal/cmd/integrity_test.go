package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// The signed integrity record (PRD v2 §8.3, ADR-0004) at every verification
// point: tq run, tq env, tq hooks verify, the session-start preamble and
// tq allow.

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendTo(t *testing.T, p, extra string) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p, string(b)+extra)
}

// mkIntegrityWS is a trusted protected-layout workspace with claude and
// codex identities, settings.json and an installed tentaqles plugin
// (hooks/, scripts/, .claude-plugin/) in its Claude config dir, signed as
// it is. It returns the root and the plugin's hooks.json.
func mkIntegrityWS(t *testing.T, name string) (root, hooksJSON string) {
	t.Helper()
	root, mp := mkLaunchWSLayout(t, name, "", true)
	clearAgentMarkers(t)
	claude := paths.IdentityDir(name, "claude")
	plugin := filepath.Join(claude, "plugins", "cache", "tentaqles", "tentaqles", "0.16.0")
	hooksJSON = filepath.Join(plugin, "hooks", "hooks.json")
	mustWrite(t, filepath.Join(claude, "settings.json"), "{}\n")
	mustWrite(t, filepath.Join(plugin, ".claude-plugin", "plugin.json"), `{"name":"tentaqles","version":"0.16.0"}`)
	mustWrite(t, hooksJSON, `{"hooks":{}}`)
	mustWrite(t, filepath.Join(plugin, "scripts", "tq_hook.sh"), "#!/usr/bin/env bash\nexit 0\n")
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow(name, h); err != nil {
		t.Fatal(err)
	}
	if !trust.IsTrusted(name, h) {
		t.Fatal("signed as it is")
	}
	return root, hooksJSON
}

// tq run <ws> and tq run . refuse on an integrity mismatch, naming the
// changed file and tq hooks verify; nothing launches.
func TestRun_RefusesOnIntegrityMismatch(t *testing.T) {
	root, hooksJSON := mkIntegrityWS(t, "acme")
	calls := fakeExec(t)
	if out, err := runTq(t, "run", "acme", "--", "claude"); err != nil || len(*calls) != 1 {
		t.Fatalf("trusted: %v %s %+v", err, out, *calls)
	}
	appendTo(t, hooksJSON, "\n")
	*calls = nil
	for _, target := range []string{"acme", "."} {
		chdir(t, root)
		_, err := runTq(t, "run", target, "--", "claude")
		if err == nil || !strings.Contains(err.Error(), "workspace acme is untrusted: integrity mismatch, changed "+hooksJSON) ||
			!strings.Contains(err.Error(), "(run: tq hooks verify acme)") {
			t.Fatalf("tq run %s: %v", target, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("nothing may launch on a mismatch: %+v", *calls)
	}
	// The hook's binary itself is covered: a record whose signature fails
	// refuses the same way.
	trust.Deny("acme", manifestHash(t, root))
	mustWrite(t, trust.SignedRecordPath("acme"), "{not json")
	_, err := runTq(t, "run", "acme", "--", "claude")
	if err == nil || !strings.Contains(err.Error(), "integrity record unreadable") || len(*calls) != 0 {
		t.Fatalf("%v %+v", err, *calls)
	}
}

// tq env emits no identity env on a mismatch and one warning line (printed
// by the shell on stderr) naming the first changed file and tq hooks
// verify; --json carries it as "warning".
func TestEnv_NoIdentityAndOneWarningOnMismatch(t *testing.T) {
	root, hooksJSON := mkIntegrityWS(t, "acme")
	chdir(t, root)
	if got := runEnv(t, "env", "--shell", "bash"); !strings.Contains(got, "export TQ_WS='acme'") {
		t.Fatalf("trusted: %q", got)
	}
	appendTo(t, hooksJSON, "\n")
	unsetenv(t, "TQ_WS")
	got := runEnv(t, "env", "--shell", "bash")
	if strings.Contains(got, "TQ_WS") || strings.Contains(got, "CLAUDE_CONFIG_DIR") || strings.Contains(got, "export") {
		t.Fatalf("no identity env on a mismatch: %q", got)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	want := "printf '%s\\n' 'tq: acme untrusted: integrity mismatch, changed " + hooksJSON + " (run: tq hooks verify acme)' >&2"
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("one warning line:\n got %q\nwant %q", got, want)
	}
	if ps := runEnv(t, "env", "--shell", "pwsh"); !strings.HasPrefix(ps, "[Console]::Error.WriteLine('tq: acme untrusted: integrity mismatch, changed ") {
		t.Fatalf("pwsh: %q", ps)
	}
	if js := runEnv(t, "env", "--json"); !strings.Contains(js, `"warning":"tq: acme untrusted: integrity mismatch, changed `) {
		t.Fatalf("json: %q", js)
	}
	// A never-trusted workspace stays quiet: the warning is the mismatch's.
	trust.Deny("acme", manifestHash(t, root))
	if got := runEnv(t, "env", "--shell", "bash"); got != "" {
		t.Fatalf("no record, no warning: %q", got)
	}
}

// tq hooks verify prints what changed (kind, path, old and new hash) and
// exits non-zero on a mismatch; --accept refuses an agent session and
// re-signs for a human.
func TestHooksVerify_ReportExitAndAccept(t *testing.T) {
	root, hooksJSON := mkIntegrityWS(t, "acme")
	out, err := runTq(t, "hooks", "verify", "acme")
	if err != nil || !strings.Contains(out, "acme: integrity ok") || !strings.Contains(out, "integrity record signed ") {
		t.Fatalf("%v %s", err, out)
	}
	appendTo(t, hooksJSON, "\n")
	added := filepath.Join(filepath.Dir(hooksJSON), "extra.sh")
	mustWrite(t, added, "#!/bin/sh\n")
	out, err = runTq(t, "hooks", "verify", "acme")
	if err == nil || !strings.Contains(err.Error(), "integrity mismatch") || !strings.Contains(err.Error(), "tq hooks verify --accept acme") {
		t.Fatalf("exit non-zero with the fix: %v", err)
	}
	for _, want := range []string{"changed  " + hooksJSON + "  ", "added    " + added + "  (absent) -> ", "acme: 2 changes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "  changed") && !strings.Contains(line, " -> ") {
			t.Fatalf("old -> new hash prefixes: %q", line)
		}
	}
	for _, k := range agentMarkers {
		clearAgentMarkers(t)
		t.Setenv(k, "1")
		if _, err := runTq(t, "hooks", "verify", "--accept", "acme"); !errors.Is(err, errHumanOnly) {
			t.Fatalf("--accept under %s: want errHumanOnly, got %v", k, err)
		}
	}
	clearAgentMarkers(t)
	if _, err := runTq(t, "hooks", "verify", "acme"); err == nil {
		t.Fatal("the agents' attempts signed nothing")
	}
	out, err = runTq(t, "hooks", "verify", "--accept", "acme")
	if err != nil || !strings.Contains(out, "acme: 2 changes") || !strings.Contains(out, "Trusted acme") {
		t.Fatalf("a human accepts after the report: %v %s", err, out)
	}
	if out, err := runTq(t, "hooks", "verify", "acme"); err != nil || !strings.Contains(out, "acme: integrity ok") {
		t.Fatalf("%v %s", err, out)
	}
	// No workspace named: the current folder's.
	chdir(t, root)
	if out, err := runTq(t, "hooks", "verify"); err != nil || !strings.Contains(out, "acme: integrity ok") {
		t.Fatalf("%v %s", err, out)
	}
	chdir(t, t.TempDir())
	if _, err := runTq(t, "hooks", "verify"); err == nil || !strings.Contains(err.Error(), "not in a workspace") {
		t.Fatalf("%v", err)
	}
	// No record: tq allow is the fix.
	trust.Deny("acme", manifestHash(t, root))
	out, err = runTq(t, "hooks", "verify", "acme")
	if err == nil || !strings.Contains(err.Error(), "no integrity record") || !strings.Contains(err.Error(), "tq allow acme") || !strings.Contains(out, "no integrity record") {
		t.Fatalf("%v %s", err, out)
	}
}

// The session-start preamble names the mismatch in its tq doctor block.
func TestSessionStart_PrintsIntegrityMismatch(t *testing.T) {
	root, hooksJSON := mkIntegrityWS(t, "acme")
	appendTo(t, hooksJSON, "\n")
	code, out, errOut := runHook(t, []string{"claude-hook", "session-start"}, hookPayloadFor(t, root, ""))
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, "Identity: acme") || !strings.Contains(out, "[error] integrity:") ||
		!strings.Contains(out, "changed "+hooksJSON) || !strings.Contains(out, "tq hooks verify acme") {
		t.Fatalf("preamble:\n%s", out)
	}
	if strings.Contains(out, "[warn] untrusted") {
		t.Fatalf("a mismatch is not the plain untrusted: %s", out)
	}
}

// tq allow renders the identity files and then signs, so the rendered
// settings.json and Codex files are covered by the record it leaves.
func TestAllow_SignsAfterRenderingIdentityFiles(t *testing.T) {
	_, mp := mkLaunchWSLayout(t, "acme", "", true)
	clearAgentMarkers(t)
	claude := paths.IdentityDir("acme", "claude")
	if err := os.MkdirAll(claude, 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := runTq(t, "allow", "acme")
	if err != nil || !strings.Contains(out, "Trusted acme") {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(claude, "settings.json")); err != nil {
		t.Fatal("tq allow renders settings.json")
	}
	if _, err := os.Stat(filepath.Join(paths.IdentityDir("acme", "codex"), "hooks.json")); err != nil {
		t.Fatal("tq allow renders the Codex hook file")
	}
	h, _ := trust.HashFile(mp)
	rep, err := trust.Verify("acme")
	if err != nil || !rep.Trusted(h) {
		t.Fatalf("the record covers what was rendered: %+v %v", rep.Changes, err)
	}
}

// tq settings render signs the record again for a human; in an agent
// session the record is left stale and the human is told what to run.
func TestSettingsRender_ResignsForHumanNotAgent(t *testing.T) {
	_, mp := mkLaunchWSLayout(t, "acme", "", true)
	clearAgentMarkers(t)
	if out, err := runTq(t, "allow", "acme"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	// A manifest change (re-allowed by a human) that the next render acts
	// on: the record then covers settings.json as rendered for the old
	// mode, so a render that writes the new one must sign again.
	setMode := func(mode string) string {
		mustWrite(t, mp, "schema: tentaqles-client-v2\nclient: acme\ngit: { email: tester@example.com }\nidentities: { claude: {}, codex: {} }\nclaude: { permission_mode: "+mode+" }\n")
		h, _ := trust.HashFile(mp)
		if err := trust.Allow("acme", h); err != nil {
			t.Fatal(err)
		}
		return h
	}
	h := setMode("plan")
	t.Setenv("CLAUDECODE", "1")
	out, err := runTq(t, "settings", "render", "acme")
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if trust.IsTrusted("acme", h) {
		t.Fatalf("an agent's render must not sign: %s", out)
	}
	if !strings.Contains(out, "integrity record is stale") || !strings.Contains(out, "tq hooks verify --accept acme") {
		t.Fatalf("the human is told: %s", out)
	}
	clearAgentMarkers(t)
	if out, err := runTq(t, "hooks", "verify", "--accept", "acme"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	h = setMode("acceptEdits")
	out, err = runTq(t, "settings", "render", "acme")
	if err != nil || !strings.Contains(out, "updated") {
		t.Fatalf("%v %s", err, out)
	}
	if !trust.IsTrusted("acme", h) {
		t.Fatalf("a human's render signs again: %s", out)
	}
}
