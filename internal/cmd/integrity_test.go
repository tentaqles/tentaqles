package cmd

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
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
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, root, `git commit -m x`))
	if code != 2 || !strings.Contains(errOut, "not trusted") || !strings.Contains(errOut, "integrity mismatch: run tq hooks verify acme") || strings.Contains(errOut, "tq allow") {
		t.Fatalf("a local commit is refused as untrusted, pointing at tq hooks verify: %d %q", code, errOut)
	}
	if _, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, root, "git push origin main")); !strings.Contains(errOut, "integrity mismatch: run tq hooks verify acme") || strings.Contains(errOut, "tq allow") {
		t.Fatalf("the remote deny points at tq hooks verify: %q", errOut)
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

// disableAllHooks in the identity's settings.json, which turns tq's hook
// off, is a change tq hooks verify reports; so is a tq planted in the
// plugin's bin/, the first place the hook bridge looks for tq.
func TestHooksVerify_ReportsTheHookSwitchAndAPlantedPluginBinary(t *testing.T) {
	_, hooksJSON := mkIntegrityWS(t, "acme")
	settings := filepath.Join(paths.IdentityDir("acme", "claude"), "settings.json")
	mustWrite(t, settings, `{"hooks":{"PreToolUse":[]},"disableAllHooks":true}`)
	out, err := runTq(t, "hooks", "verify", "acme")
	if err == nil || !strings.Contains(out, "changed  "+trust.Key(settings+"#tq")) {
		t.Fatalf("disableAllHooks must be reported: %v\n%s", err, out)
	}
	mustWrite(t, settings, `{"hooks":{"PreToolUse":[]}}`)
	planted := filepath.Join(filepath.Dir(filepath.Dir(hooksJSON)), "bin", "tq")
	mustWrite(t, planted, "#!/bin/sh\nexit 0\n")
	out, err = runTq(t, "hooks", "verify", "acme")
	if err == nil || !strings.Contains(out, "added    "+trust.Key(planted)+"  (absent) -> ") {
		t.Fatalf("a planted plugin bin/tq must be reported: %v\n%s", err, out)
	}
}

// addSignedWS adds a signed protected-layout workspace with the plugin in
// its Claude config dir to the registered base of the current home.
func addSignedWS(t *testing.T, base, name string) (pluginRoot string) {
	t.Helper()
	root := filepath.Join(base, name)
	mustWrite(t, filepath.Join(root, manifest.FileName), string(manifest.MarkerBytes(name)))
	mp := paths.ManifestPath(name)
	mustWrite(t, mp, "schema: tentaqles-client-v2\nclient: "+name+"\ngit: { email: tester@example.com }\nidentities: { claude: {} }\n")
	claude := paths.IdentityDir(name, "claude")
	pluginRoot = filepath.Join(claude, "plugins", "cache", "tentaqles", "tentaqles", "0.16.0")
	mustWrite(t, filepath.Join(claude, "settings.json"), `{"hooks":{"PreToolUse":[]}}`)
	mustWrite(t, filepath.Join(pluginRoot, ".claude-plugin", "plugin.json"), `{"name":"tentaqles","version":"0.16.0"}`)
	mustWrite(t, filepath.Join(pluginRoot, "scripts", "tq_hook.sh"), "#!/usr/bin/env bash\nexit 0\n")
	h, _ := trust.HashFile(mp)
	if err := trust.Allow(name, h); err != nil {
		t.Fatal(err)
	}
	return pluginRoot
}

// gitFixture runs git in dir for a test fixture, with neither the system
// nor the user's git config and a fixed author.
func gitFixture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.com")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// release017 is the plugin folder of the published release 0.17.0.
func release017() map[string]string {
	return map[string]string{
		".claude-plugin/plugin.json": `{"name":"tentaqles","version":"0.17.0"}`,
		"hooks/hooks.json":           `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"bash tq_hook.sh pre-tool-use"}]}]}}`,
		"scripts/tq_hook.sh":         "#!/usr/bin/env bash\nexec tq claude-hook \"$1\"\n",
	}
}

// publishRelease writes the tentaqles marketplace clone of ws's Claude
// identity, one commit holding files under plugin/ pinned by its
// marketplace.json, and returns the sha.
func publishRelease(t *testing.T, files map[string]string) (repo, sha string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	repo = filepath.Join(testutil.TempDir(t), "tentaqles")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, repo, "init", "-q")
	sha = commitRelease(t, repo, files, "release")
	// The commit this tq was built from (cmd.Commit in a release build).
	prev := trust.ReleaseCommit
	trust.ReleaseCommit = sha
	t.Cleanup(func() { trust.ReleaseCommit = prev })
	return repo, sha
}

// commitRelease commits files under plugin/ of repo and returns the commit.
func commitRelease(t *testing.T, repo string, files map[string]string, msg string) string {
	t.Helper()
	for rel, body := range files {
		mustWrite(t, filepath.Join(repo, "plugin", filepath.FromSlash(rel)), body)
	}
	gitFixture(t, repo, "add", "-A")
	gitFixture(t, repo, "commit", "-q", "-m", msg)
	return strings.TrimSpace(gitFixture(t, repo, "rev-parse", "HEAD"))
}

// cloneRelease clones repo as the tentaqles marketplace clone of ws's Claude
// identity, its origin the tentaqles repository, its marketplace.json pinning
// pinned (which tq never reads). It returns the clone.
func cloneRelease(t *testing.T, repo, ws, pinned string) string {
	t.Helper()
	clone := trust.MarketplaceClone(paths.IdentityDir(ws, "claude"))
	if err := os.MkdirAll(filepath.Dir(clone), 0o700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, filepath.Dir(clone), "clone", "-q", repo, clone)
	gitFixture(t, clone, "remote", "set-url", "origin", "https://github.com/tentaqles/tentaqles.git")
	mustWrite(t, filepath.Join(clone, ".claude-plugin", "marketplace.json"),
		`{"name":"tentaqles","plugins":[{"name":"tentaqles","source":{"source":"git-subdir","url":"https://github.com/tentaqles/tentaqles.git","path":"plugin","ref":"v0.9.1","sha":"`+pinned+`"}}]}`)
	return clone
}

// installVersion puts files as the cached plugin folder 0.17.0 of ws.
func installVersion(t *testing.T, ws string, files map[string]string) {
	t.Helper()
	next := filepath.Join(paths.IdentityDir(ws, "claude"), "plugins", "cache", "tentaqles", "tentaqles", "0.17.0")
	for rel, body := range files {
		mustWrite(t, filepath.Join(next, filepath.FromSlash(rel)), body)
	}
}

// tq hooks verify --accept --all signs again, after one confirmation, the
// workspaces whose only change is a new plugin version that is the
// published release byte for byte, listed first under the release; every
// other workspace is not accepted and named, with why: beta's new folder
// has an emptied hooks.json (the critic's case: a bump by location alone
// was accepted before), gamma has a hook edit beside the bump, delta has no
// marketplace clone to compare with.
func TestHooksVerify_AcceptAllSignsThePublishedReleaseOnly(t *testing.T) {
	root, _ := mkIntegrityWS(t, "acme")
	base := filepath.Dir(root)
	for _, n := range []string{"beta", "gamma", "delta", "zeta"} {
		addSignedWS(t, base, n)
	}
	repo, sha := publishRelease(t, release017())
	for _, n := range []string{"acme", "beta", "gamma"} {
		cloneRelease(t, repo, n, sha)
	}
	installVersion(t, "acme", release017())
	emptied := release017()
	emptied["hooks/hooks.json"] = `{"hooks":{}}`
	installVersion(t, "beta", emptied)
	installVersion(t, "gamma", release017())
	mustWrite(t, filepath.Join(paths.IdentityDir("gamma", "claude"), "settings.json"), `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[]}]}}`)
	installVersion(t, "delta", release017())
	// zeta: the critic's re-pinned clone. It forged a commit (an emptied
	// hooks.json, a planted bin/tq) on top of tq's release, pins it, and the
	// cache holds it.
	zeta := cloneRelease(t, repo, "zeta", sha)
	forged := release017()
	forged["hooks/hooks.json"] = `{"hooks":{}}`
	forged["bin/tq"] = "#!/bin/sh\nexit 0\n"
	forgedSHA := commitRelease(t, zeta, forged, "forged")
	mustWrite(t, filepath.Join(zeta, ".claude-plugin", "marketplace.json"),
		`{"name":"tentaqles","plugins":[{"name":"tentaqles","source":{"source":"git-subdir","url":"https://github.com/tentaqles/tentaqles.git","path":"plugin","ref":"v0.9.1","sha":"`+forgedSHA+`"}}]}`)
	installVersion(t, "zeta", forged)

	if _, err := runTq(t, "hooks", "verify", "--all"); err == nil || !strings.Contains(err.Error(), "5 workspace(s) do not verify") {
		t.Fatalf("--all without --accept reports and exits non-zero: %v", err)
	}
	prev := humanonly.Confirm
	asked := 0
	humanonly.Confirm = func(string) (bool, error) { asked++; return true, nil }
	t.Cleanup(func() { humanonly.Confirm = prev })
	out, err := runTq(t, "hooks", "verify", "--accept", "--all")
	if asked != 1 {
		t.Fatalf("one confirmation for all, got %d:\n%s", asked, out)
	}
	label := "Matches the published release tentaqles 0.17.0 (" + sha[:12] + "):\n  acme (plugin tentaqles 0.16.0 -> 0.17.0, "
	list := strings.Index(out, label)
	gate := strings.Index(out, "tq hooks verify --accept --all: about to")
	if list < 0 || gate < 0 || list > gate {
		t.Fatalf("acme is listed under the release, before the confirmation:\n%s", out)
	}
	for _, n := range []string{"beta", "gamma", "delta", "zeta"} {
		if strings.Contains(out[list:gate], n) {
			t.Fatalf("%s must not be listed under the release:\n%s", n, out)
		}
	}
	if strings.Contains(out, "Only a new plugin version changed") {
		t.Fatal("the old label is gone")
	}
	for n, reason := range map[string]string{
		"beta":  "  hooks/hooks.json: differs from the release",
		"gamma": "  changes outside the plugin folder",
		"delta": "  no tentaqles marketplace clone",
		"zeta":  "  bin/tq: not in the release\n  hooks/hooks.json: differs from the release",
	} {
		if !strings.Contains(out, "not accepted: "+n+" has changes that are not in the published release\n"+reason) {
			t.Fatalf("%s is not accepted, with why (%q):\n%s", n, reason, out)
		}
	}
	if !strings.Contains(out, "Only the plugin release this tq was built from ("+sha[:12]+") is accepted in batch; a plugin release newer than this tq needs tq hooks verify --accept <ws> until tq is upgraded.") {
		t.Fatalf("the output says why a newer release waits:\n%s", out)
	}
	if err == nil || !strings.Contains(err.Error(), "4 workspace(s) not accepted: beta, delta, gamma, zeta") {
		t.Fatalf("the exit names the four: %v", err)
	}
	if out, err := runTq(t, "hooks", "verify", "acme"); err != nil || !strings.Contains(out, "acme: integrity ok") {
		t.Fatalf("acme signed again: %v %s", err, out)
	}
	for _, n := range []string{"beta", "gamma", "delta", "zeta"} {
		if _, err := runTq(t, "hooks", "verify", n); err == nil {
			t.Fatalf("%s stays untrusted", n)
		}
	}
	// A dev build of tq (no release commit) accepts nothing in batch.
	trust.ReleaseCommit = ""
	asked = 0
	out, err = runTq(t, "hooks", "verify", "--accept", "--all")
	if asked != 0 || err == nil || !strings.Contains(out, "this tq build has no release commit") || !strings.Contains(out, "This tq build has no release commit, so nothing is accepted in batch.") {
		t.Fatalf("a dev build: asked %d, %v\n%s", asked, err, out)
	}
	// An agent never accepts all.
	t.Setenv("CLAUDECODE", "1")
	if _, err := runTq(t, "hooks", "verify", "--accept", "--all"); !errors.Is(err, errHumanOnly) {
		t.Fatalf("%v", err)
	}
}

// The signed integrity record, the machine marker trust/.signed and the
// legacy snapshot sit in the state folder P1.3 protects: the hook denies a
// session every write to them (protected-path), and they stay as signed.
func TestProtectedPaths_SignedRecordAndMachineMarker(t *testing.T) {
	f := setupProtected(t, "")
	record := trust.SignedRecordPath("acme")
	marker := trust.SignedMarkerPath()
	before := map[string][]byte{}
	for _, p := range []string{record, marker} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("precondition: %s exists after the signature: %v", p, err)
		}
		before[p] = b
	}
	for _, tc := range []struct{ name, tool, input string }{
		{"Write trust/acme.json", "Write", `{"file_path":` + jsonPathLit(record) + `,"content":"{}"}`},
		{"Edit trust/acme.json", "Edit", `{"file_path":` + jsonPathLit(record) + `,"old_string":"a","new_string":"b"}`},
		{"Write trust/.signed", "Write", `{"file_path":` + jsonPathLit(marker) + `,"content":""}`},
		{"Write trust/legacy.json", "Write", pathIn("file_path", trust.LegacyListPath())},
		{"rm the record by its ~ path", "Bash", cmdIn("rm -f ~/.tentaqles/trust/acme.json")},
		{"rm trust/.signed", "Bash", cmdIn("rm -f " + filepath.ToSlash(marker))},
		{"Remove-Item trust/.signed", "PowerShell", cmdIn("Remove-Item -Force " + psUnderHome(".tentaqles/trust/.signed"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, category, rule, errOut := hookDecision(t, f.root, tc.tool, tc.input)
			if code != 2 || rule != policy.ProtectedPathID {
				t.Fatalf("want a protected-path deny, code=%d category=%q rule=%q %s", code, category, rule, errOut)
			}
		})
	}
	for p, b := range before {
		if got, _ := os.ReadFile(p); string(got) != string(b) {
			t.Fatalf("%s changed", p)
		}
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
	mustWrite(t, mp, "schema: tentaqles-client-v2\nclient: acme\ngit: { email: tester@example.com }\nidentities: { claude: {}, codex: {} }\nclaude: { env: { FOO: bar } }\n")
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

	if _, err := runTq(t, "reach", filepath.Join(base, "evil")); !errors.Is(err, errHumanOnly) {
		t.Fatalf("tentaqles reach: %v", err)
	}
	if _, err := os.Stat(trust.SignedRecordPath("evil")); err == nil {
		t.Fatal("tentaqles reach signed under a harness")
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
