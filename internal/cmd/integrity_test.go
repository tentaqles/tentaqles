package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
	"github.com/tentaqles/tentaqles/internal/workspace"
	"github.com/zalando/go-keyring"
)

// The signed integrity record (PRD v2 §8.3, ADR-0004) at every verification
// point: the hook, tq run, tq env, tq hooks verify, the session-start
// preamble and tq allow; and the gate every signature passes.

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
// codex identities, a settings.json with a hook and an installed tentaqles
// plugin (hooks/, scripts/, .claude-plugin/) in its Claude config dir,
// signed as it is. It returns the root and the plugin's hooks.json.
func mkIntegrityWS(t *testing.T, name string) (root, hooksJSON string) {
	t.Helper()
	root, mp := mkLaunchWSLayout(t, name, "", true)
	clearAgentMarkers(t)
	claude := paths.IdentityDir(name, "claude")
	plugin := filepath.Join(claude, "plugins", "cache", "tentaqles", "tentaqles", "0.16.0")
	hooksJSON = filepath.Join(plugin, "hooks", "hooks.json")
	mustWrite(t, filepath.Join(claude, "settings.json"), `{"hooks":{"PreToolUse":[]}}`)
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

// asIdentityShell sets the environment of a tq-activated shell in ws, so a
// trusted workspace's git push is allowed and only the record decides.
func asIdentityShell(t *testing.T, ws string) {
	t.Helper()
	t.Setenv("TQ_WS", ws)
	t.Setenv("CLAUDE_CONFIG_DIR", paths.IdentityDir(ws, "claude"))
}

// The hook treats a mismatch at least as strictly as a workspace never
// allowed: after one byte appended to the plugin's hooks.json, git push
// and gh are denied with the reason naming the file, a local commit is
// denied as untrusted, and a plain command still runs.
func TestPreToolUse_IntegrityMismatchDeniesRemoteAndGit(t *testing.T) {
	root, hooksJSON := mkIntegrityWS(t, "acme")
	asIdentityShell(t, "acme")
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, root, "git push origin main")); code != 0 {
		t.Fatalf("trusted: git push allowed, got %d %q", code, errOut)
	}
	appendTo(t, hooksJSON, "\n")
	for _, cmd := range []string{"git push origin main", "gh pr create --fill", "az vm create -n x"} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, root, cmd))
		if code != 2 || !strings.Contains(errOut, "integrity mismatch, changed "+trust.Key(hooksJSON)) || !strings.Contains(errOut, "tq hooks verify acme") {
			t.Fatalf("%s: want a deny naming the file, got %d %q", cmd, code, errOut)
		}
	}
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, root, `git commit -m x`)); code != 2 || !strings.Contains(errOut, "not trusted") {
		t.Fatalf("a local commit is refused as untrusted: %d %q", code, errOut)
	}
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, root, "ls -la")); code != 0 {
		t.Fatalf("a plain command still runs: %d %q", code, errOut)
	}
}

// One pre-tool-use call verifies the record once, however many times
// resolve and doctor ask (trust.MemoScope).
func TestPreToolUse_VerifiesOncePerCall(t *testing.T) {
	root, hooksJSON := mkIntegrityWS(t, "acme")
	appendTo(t, hooksJSON, "\n") // untrusted: resolve and doctor's integrity check both ask
	before := trust.Verifications()
	if code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, root, "git push origin main")); code != 2 {
		t.Fatal("denied")
	}
	if n := trust.Verifications() - before; n != 1 {
		t.Fatalf("%d verifications in one pre-tool-use call", n)
	}
	before = trust.Verifications()
	runHook(t, []string{"claude-hook", "pre-tool-use"}, `{"tool_name":"Read","cwd":`+jsonPath(t, root)+`,"tool_input":{"file_path":"x.txt"}}`)
	if n := trust.Verifications() - before; n != 1 {
		t.Fatalf("%d verifications for a Read", n)
	}
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
		if err == nil || !strings.Contains(err.Error(), "workspace acme is untrusted: integrity mismatch, changed "+trust.Key(hooksJSON)) ||
			!strings.Contains(err.Error(), "(run: tq hooks verify acme)") {
			t.Fatalf("tq run %s: %v", target, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("nothing may launch on a mismatch: %+v", *calls)
	}
	// A record that is not JSON refuses the same way.
	mustWrite(t, trust.SignedRecordPath("acme"), "{not json")
	_, err := runTq(t, "run", "acme", "--", "claude")
	if err == nil || !strings.Contains(err.Error(), "integrity record unreadable") || len(*calls) != 0 {
		t.Fatalf("%v %+v", err, *calls)
	}
}

// tq env emits no identity env on a mismatch and one fixed line naming the
// workspace and tq hooks verify, never a path; --json carries it.
func TestEnv_NoIdentityAndOneNoticeOnMismatch(t *testing.T) {
	root, hooksJSON := mkIntegrityWS(t, "acme")
	chdir(t, root)
	if got := runEnv(t, "env", "--shell", "bash"); !strings.Contains(got, "export TQ_WS='acme'") || strings.Contains(got, "printf") {
		t.Fatalf("trusted, signed: the identity and no notice: %q", got)
	}
	appendTo(t, hooksJSON, "\n")
	unsetenv(t, "TQ_WS")
	got := runEnv(t, "env", "--shell", "bash")
	if strings.Contains(got, "TQ_WS") || strings.Contains(got, "CLAUDE_CONFIG_DIR") || strings.Contains(got, "export") {
		t.Fatalf("no identity env on a mismatch: %q", got)
	}
	want := "printf '%s\\n' 'tq: integrity mismatch: run tq hooks verify acme' >&2"
	if strings.TrimSpace(got) != want {
		t.Fatalf("one notice line:\n got %q\nwant %q", got, want)
	}
	if strings.Contains(got, filepath.ToSlash(hooksJSON)) || strings.Contains(got, "hooks.json") {
		t.Fatalf("no path in the shell line: %q", got)
	}
	if ps := runEnv(t, "env", "--shell", "pwsh"); strings.TrimSpace(ps) != "[Console]::Error.WriteLine('tq: integrity mismatch: run tq hooks verify acme')" {
		t.Fatalf("pwsh: %q", ps)
	}
	if js := runEnv(t, "env", "--json"); !strings.Contains(js, `"notice":"tq: integrity mismatch: run tq hooks verify acme"`) {
		t.Fatalf("json: %q", js)
	}
}

// A never-trusted workspace prints its own per-prompt line (tq allow).
func TestEnv_NeverTrustedPrintsANotice(t *testing.T) {
	root := mkUntrusted(t, "evil")
	clearAgentMarkers(t)
	chdir(t, root)
	if got := strings.TrimSpace(runEnv(t, "env", "--shell", "bash")); got != "printf '%s\\n' 'tq: untrusted: run tq allow evil' >&2" {
		t.Fatalf("%q", got)
	}
}

// While a workspace is trusted through a marker of an older tq, every
// prompt says so; once a human signs, the line goes.
func TestEnv_LegacyTrustPrintsANotice(t *testing.T) {
	root := mkUntrusted(t, "acme")
	clearAgentMarkers(t)
	keyring.MockInit() // nothing signed on this machine yet
	mustWrite(t, trust.LegacyRecordPath(manifestHash(t, root)), "")
	chdir(t, root)
	got := runEnv(t, "env", "--shell", "bash")
	if !strings.Contains(got, "export TQ_WS='acme'") || !strings.Contains(got, "printf '%s\\n' 'tq: legacy trust record: run tq allow acme to sign hooks and manifest' >&2") {
		t.Fatalf("a legacy marker still trusts, and says so: %q", got)
	}
	if out, err := runTq(t, "allow", "acme"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if got := runEnv(t, "env", "--shell", "bash"); strings.Contains(got, "printf") {
		t.Fatalf("signed: no notice: %q", got)
	}
}

// After a signature, deleting the record and planting a marker does not
// bring the trust back (the snapshot never lists the planted marker).
func TestLegacyDowngradeIsUntrusted(t *testing.T) {
	root, _ := mkIntegrityWS(t, "acme")
	h := manifestHash(t, root)
	if err := os.Remove(trust.SignedRecordPath("acme")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, trust.LegacyRecordPath(h), "")
	mustWrite(t, trust.RecordPath("acme", h), "")
	calls := fakeExec(t)
	if _, err := runTq(t, "run", "acme", "--", "claude"); err == nil || len(*calls) != 0 {
		t.Fatalf("a downgrade must refuse: %v %+v", err, *calls)
	}
	chdir(t, root)
	if got := strings.TrimSpace(runEnv(t, "env", "--shell", "bash")); got != "printf '%s\\n' 'tq: untrusted: run tq allow acme' >&2" {
		t.Fatalf("%q", got)
	}
}

// tq hooks verify prints what changed (kind, key, old and new hash) and
// exits non-zero on a mismatch; --accept refuses an agent session, asks
// the human to confirm (no sign on anything but yes) and re-signs.
func TestHooksVerify_ReportExitAndAccept(t *testing.T) {
	root, hooksJSON := mkIntegrityWS(t, "acme")
	out, err := runTq(t, "hooks", "verify", "acme")
	if err != nil || !strings.Contains(out, "acme: integrity ok") || !strings.Contains(out, " with key ") {
		t.Fatalf("%v %s", err, out)
	}
	appendTo(t, hooksJSON, "\n")
	added := filepath.Join(filepath.Dir(hooksJSON), "extra.sh")
	mustWrite(t, added, "#!/bin/sh\n")
	out, err = runTq(t, "hooks", "verify", "acme")
	if err == nil || !strings.Contains(err.Error(), "integrity mismatch") || !strings.Contains(err.Error(), "tq hooks verify --accept acme") {
		t.Fatalf("exit non-zero with the fix: %v", err)
	}
	for _, want := range []string{"changed  " + trust.Key(hooksJSON) + "  ", "added    " + trust.Key(added) + "  (absent) -> ", "acme: 2 changes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report lacks %q:\n%s", want, out)
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
	prev := humanonly.Confirm
	asked := ""
	humanonly.Confirm = func(p string) (bool, error) { asked = p; return false, nil }
	_, err = runTq(t, "hooks", "verify", "--accept", "acme")
	humanonly.Confirm = prev
	if !errors.Is(err, humanonly.ErrAborted) || asked == "" {
		t.Fatalf("--accept asks the human first: %v %q", err, asked)
	}
	if _, err := runTq(t, "hooks", "verify", "acme"); err == nil {
		t.Fatal("nothing was signed without a yes")
	}
	out, err = runTq(t, "hooks", "verify", "--accept", "acme")
	if err != nil || !strings.Contains(out, "acme: 2 changes") || !strings.Contains(out, "Trusted acme") || !strings.Contains(out, "tq hooks verify --accept: about to") {
		t.Fatalf("a human accepts after the report: %v %s", err, out)
	}
	if out, err := runTq(t, "hooks", "verify", "acme"); err != nil || !strings.Contains(out, "acme: integrity ok") {
		t.Fatalf("%v %s", err, out)
	}
	chdir(t, root)
	if out, err := runTq(t, "hooks", "verify"); err != nil || !strings.Contains(out, "acme: integrity ok") {
		t.Fatalf("the current folder's: %v %s", err, out)
	}
	chdir(t, t.TempDir())
	if _, err := runTq(t, "hooks", "verify"); err == nil || !strings.Contains(err.Error(), "not in a workspace") {
		t.Fatalf("%v", err)
	}
	// Revoked: tq allow is the fix.
	if _, err := runTq(t, "deny", "acme"); err != nil {
		t.Fatal(err)
	}
	out, err = runTq(t, "hooks", "verify", "acme")
	if err == nil || !strings.Contains(err.Error(), "trust revoked") || !strings.Contains(err.Error(), "tq allow acme") || !strings.Contains(out, "trust revoked by tq deny") {
		t.Fatalf("%v %s", err, out)
	}
}

// A new plugin version folder is summarised ahead of the per-file lines.
func TestHooksVerify_SummarisesAPluginUpgrade(t *testing.T) {
	_, hooksJSON := mkIntegrityWS(t, "acme")
	next := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(hooksJSON))), "0.17.0")
	mustWrite(t, filepath.Join(next, ".claude-plugin", "plugin.json"), `{"name":"tentaqles","version":"0.17.0"}`)
	mustWrite(t, filepath.Join(next, "hooks", "hooks.json"), `{"hooks":{}}`)
	mustWrite(t, filepath.Join(next, "scripts", "tq_hook.sh"), "#!/usr/bin/env bash\nexit 1\n")
	out, err := runTq(t, "hooks", "verify", "acme")
	if err == nil {
		t.Fatal("an upgrade is a change")
	}
	// plugin.json and tq_hook.sh differ between the folders, hooks.json does
	// not; the old folder is still in the cache, unchanged.
	summary := "  plugin tentaqles 0.16.0 -> 0.17.0 (2 hook files changed)\n"
	i, j := strings.Index(out, summary), strings.Index(out, "  added ")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("the summary comes ahead of the per-file lines:\n%s", out)
	}
}

// The session-start preamble names the mismatch, untrusted with it.
func TestSessionStart_PrintsIntegrityMismatch(t *testing.T) {
	root, hooksJSON := mkIntegrityWS(t, "acme")
	appendTo(t, hooksJSON, "\n")
	code, out, errOut := runHook(t, []string{"claude-hook", "session-start"}, hookPayloadFor(t, root, ""))
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, "Identity: acme") || !strings.Contains(out, "[error] integrity:") ||
		!strings.Contains(out, "changed "+trust.Key(hooksJSON)) || !strings.Contains(out, "tq hooks verify acme") || !strings.Contains(out, "[warn] untrusted") {
		t.Fatalf("preamble:\n%s", out)
	}
}

// mkNeverTrusted is a protected-layout workspace with claude and codex
// identities that nobody ever signed, on a machine with no key yet.
func mkNeverTrusted(t *testing.T, name string) string {
	t.Helper()
	isolateHome(t)
	clearAgentMarkers(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, name)
	mustWrite(t, filepath.Join(root, manifest.FileName), string(manifest.MarkerBytes(name)))
	mustWrite(t, paths.ManifestPath(name), "schema: tentaqles-client-v2\nclient: "+name+"\ngit: { email: tester@example.com }\nidentities: { claude: {}, codex: {} }\n")
	if err := os.MkdirAll(paths.IdentityDir(name, "claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

// tq allow renders the identity files first, for a workspace never trusted
// too, and signs once, after: the rendered settings.json and Codex files
// are what the record covers.
func TestAllow_RendersThenSignsANeverTrustedWorkspace(t *testing.T) {
	mkNeverTrusted(t, "acme")
	claude := paths.IdentityDir("acme", "claude")
	if _, err := os.Stat(filepath.Join(claude, "settings.json")); err == nil {
		t.Fatal("precondition: nothing rendered yet")
	}
	out, err := runTq(t, "allow", "acme")
	if err != nil || !strings.Contains(out, "Trusted acme") {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(claude, "settings.json")); err != nil {
		t.Fatal("tq allow renders settings.json for a workspace not yet trusted")
	}
	if _, err := os.Stat(filepath.Join(paths.IdentityDir("acme", "codex"), "hooks.json")); err != nil {
		t.Fatal("tq allow renders the Codex hook file")
	}
	h, _ := trust.HashFile(paths.ManifestPath("acme"))
	rep, err := trust.Verify("acme")
	if err != nil || !rep.Trusted(h) {
		t.Fatalf("the record covers what was rendered: %+v %v", rep.Changes, err)
	}
}

// tq settings render signs the record again for a human after a manifest
// change it acts on.
func TestSettingsRender_ResignsForAHuman(t *testing.T) {
	_, mp := mkLaunchWSLayout(t, "acme", "", true)
	clearAgentMarkers(t)
	if out, err := runTq(t, "allow", "acme"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	mustWrite(t, mp, "schema: tentaqles-client-v2\nclient: acme\ngit: { email: tester@example.com }\nidentities: { claude: {}, codex: {} }\nclaude: { permission_mode: acceptEdits }\n")
	h, _ := trust.HashFile(mp)
	if err := trust.Allow("acme", h); err != nil {
		t.Fatal(err)
	}
	out, err := runTq(t, "settings", "render", "acme")
	if err != nil || !strings.Contains(out, "updated") {
		t.Fatalf("%v %s", err, out)
	}
	if !trust.IsTrusted("acme", h) {
		t.Fatalf("a human's render signs again: %s", out)
	}
}

// harnessTree makes the process-tree detector see Claude Code (running as
// node) between this process and the terminal; the environment carries no
// marker, as with `env -u CLAUDECODE tq ...` typed by an agent.
func harnessTree(t *testing.T) {
	t.Helper()
	clearAgentMarkers(t)
	prev := humanonly.ParentOf
	t.Cleanup(func() { humanonly.ParentOf = prev })
	self := os.Getpid()
	humanonly.ParentOf = func(pid int) (humanonly.Parent, bool) {
		switch pid {
		case self:
			return humanonly.Parent{PID: 1000, Name: "bash.exe", Cmdline: "bash", CmdlineKnown: true}, true
		case 1000:
			return humanonly.Parent{PID: 2000, Name: "node.exe", Cmdline: `"C:\Program Files\nodejs\node.exe" C:\npm\node_modules\@anthropic-ai\claude-code\cli.js`, CmdlineKnown: true}, true
		case 2000:
			return humanonly.Parent{PID: 3000, Name: "WindowsTerminal.exe", Cmdline: "wt", CmdlineKnown: true}, true
		}
		return humanonly.Parent{}, false
	}
}

// Every path to a signature takes the gate of tq allow (the three
// detectors): under a harness process tree with no marker in the
// environment, tq add, the scaffold's Trust, trust.Allow, the re-sign
// after a render and tq hooks verify --accept sign nothing.
func TestSigningRefusesUnderAHarnessProcessTree(t *testing.T) {
	root, hooksJSON := mkIntegrityWS(t, "acme")
	base := filepath.Dir(root)
	before, _ := os.ReadFile(trust.SignedRecordPath("acme"))
	harnessTree(t)

	prevRunGit := addRunGit
	addRunGit = func(...string) (string, error) { return "", nil }
	t.Cleanup(func() { addRunGit = prevRunGit })
	if _, err := runTq(t, "add", "evil", "--base", base, "--git-email", "x@example.com", "--identities", "gh"); !errors.Is(err, errHumanOnly) {
		t.Fatalf("tq add: %v", err)
	}
	if _, err := os.Stat(trust.SignedRecordPath("evil")); err == nil {
		t.Fatal("tq add signed under a harness")
	}

	_, err := workspace.Add(workspace.AddOptions{Name: "evil2", Base: base, GitEmail: "x@example.com", Identities: []string{"gh"}, Trust: true,
		RunGit: func(...string) (string, error) { return "", nil }})
	if !errors.Is(err, humanonly.ErrHumanOnly) {
		t.Fatalf("the scaffold's Trust: %v", err)
	}
	if _, err := os.Stat(trust.SignedRecordPath("evil2")); err == nil {
		t.Fatal("the scaffold signed under a harness")
	}

	h := manifestHash(t, root)
	if err := trust.Allow("acme", h); !errors.Is(err, humanonly.ErrHumanOnly) {
		t.Fatalf("trust.Allow: %v", err)
	}
	appendTo(t, hooksJSON, "\n")
	ws, err := findWorkspace("acme")
	if err != nil {
		t.Fatal(err)
	}
	c := &cobra.Command{}
	var errOut bytes.Buffer
	c.SetErr(&errOut)
	resignCovered(c, ws)
	if !strings.Contains(errOut.String(), "not signed again") || !strings.Contains(errOut.String(), "tq hooks verify --accept acme") {
		t.Fatalf("the re-sign is refused and says what to run: %q", errOut.String())
	}
	if _, err := runTq(t, "hooks", "verify", "--accept", "acme"); !errors.Is(err, errHumanOnly) {
		t.Fatalf("--accept: %v", err)
	}
	after, _ := os.ReadFile(trust.SignedRecordPath("acme"))
	if !bytes.Equal(before, after) {
		t.Fatal("the record was rewritten under a harness")
	}
	if trust.IsTrusted("acme", h) {
		t.Fatal("the tampered hooks.json stays untrusted")
	}
}
