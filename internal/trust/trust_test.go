package trust

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/secret"
	"github.com/zalando/go-keyring"
)

// fixture is a workspace with everything the integrity record covers: the
// marker and the protected manifest, a Claude identity with settings.json
// and an installed plugin. The OS
// keychain is go-keyring's mock, emptied for each fixture.
type fixture struct {
	t                                *testing.T
	name, home, base, root, mp, hash string
	claude, plugin, marker           string
	settings, hooksJSON, hookSH      string
}

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T, name string) *fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	keyring.MockInit()
	invalidate()
	f := &fixture{t: t, name: name, home: home, base: filepath.Join(home, "repos")}
	f.root = filepath.Join(f.base, name)
	if err := os.MkdirAll(f.root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(f.base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	f.marker = filepath.Join(f.root, manifest.FileName)
	write(t, f.marker, string(manifest.MarkerBytes(name)))
	f.mp = paths.ManifestPath(name)
	write(t, f.mp, "schema: tentaqles-client-v2\nclient: "+name+"\ngit: { email: "+name+"@x.io }\nidentities: { claude: {}, codex: {} }\n")
	f.claude = paths.IdentityDir(name, "claude")
	f.settings = filepath.Join(f.claude, "settings.json")
	write(t, f.settings, `{"theme":"dark","hooks":{"PreToolUse":[]}}`)
	f.plugin = filepath.Join(f.claude, "plugins", "cache", "tentaqles", "tentaqles", "0.16.0")
	write(t, filepath.Join(f.plugin, ".claude-plugin", "plugin.json"), `{"name":"tentaqles","version":"0.16.0"}`)
	f.hooksJSON = filepath.Join(f.plugin, "hooks", "hooks.json")
	write(t, f.hooksJSON, `{"hooks":{}}`)
	f.hookSH = filepath.Join(f.plugin, "scripts", "tq_hook.sh")
	write(t, f.hookSH, "#!/usr/bin/env bash\nexit 0\n")
	write(t, filepath.Join(f.plugin, "skills", "x", "SKILL.md"), "not covered\n")
	h, err := HashFile(f.mp)
	if err != nil {
		t.Fatal(err)
	}
	f.hash = h
	return f
}

func (f *fixture) allow() {
	f.t.Helper()
	if err := Allow(f.name, f.hash); err != nil {
		f.t.Fatal(err)
	}
	if v := Check(f.name, f.hash); !v.OK || v.Kind != KindTrusted {
		f.t.Fatalf("Allow must trust the state it signed: %+v", v)
	}
}

func (f *fixture) verify() Report {
	f.t.Helper()
	rep, err := Verify(f.name)
	if err != nil {
		f.t.Fatal(err)
	}
	return rep
}

func (f *fixture) append(p, extra string) {
	f.t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		f.t.Fatal(err)
	}
	write(f.t, p, string(b)+extra)
}

// changeFor returns the change naming path (its key), or fails.
func changeFor(t *testing.T, rep Report, p string) Change {
	t.Helper()
	k := Key(p)
	for _, c := range rep.Changes {
		if c.Path == k {
			return c
		}
	}
	t.Fatalf("no change names %s; changes: %v", k, rep.Changes)
	return Change{}
}

func readRec(t *testing.T, name string) Record {
	t.Helper()
	rec, ok, err := readRecord(name)
	if !ok || err != nil {
		t.Fatalf("record of %s: %v %v", name, ok, err)
	}
	return rec
}

func TestKeyServiceMatchesSecret(t *testing.T) {
	if Service != secret.Service || KeyAccount != "integrity-key" {
		t.Fatalf("%s %s", Service, KeyAccount)
	}
}

// A signed record round-trips: Allow writes trust/<name>.json with the
// schema, the workspace, the manifest hash, the covered keys, the plugin,
// the key id, a time and a signature; Verify reads it back as trusted.
func TestSignedRecordRoundTrips(t *testing.T) {
	f := newFixture(t, "acme")
	if IsTrusted("acme", f.hash) {
		t.Fatal("starts untrusted")
	}
	f.allow()
	raw, err := os.ReadFile(SignedRecordPath("acme"))
	if err != nil {
		t.Fatalf("the record is trust/<name>.json: %v", err)
	}
	var rec Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Schema != Schema || rec.Workspace != "acme" || rec.ManifestHash != f.hash || rec.SignedAt == "" || len(rec.Signature) != 64 ||
		len(rec.KeyID) != 12 || rec.Plugin != Key(f.plugin) || rec.Revoked {
		t.Fatalf("%+v", rec)
	}
	for _, p := range []string{f.mp, f.marker, f.settings + blockSuffix, f.hooksJSON, f.hookSH, filepath.Join(f.plugin, ".claude-plugin", "plugin.json")} {
		if rec.Files[Key(p)] == "" {
			t.Errorf("%s must be covered and present", Key(p))
		}
	}
	if _, ok := rec.Files[Key(filepath.Join(f.plugin, "skills", "x", "SKILL.md"))]; ok {
		t.Error("skills are not covered")
	}
	rep := f.verify()
	if !rep.Exists || rep.Invalid != "" || !rep.Trusted(f.hash) || rep.Files != len(rec.Files) || rep.SignedAt != rec.SignedAt || rep.KeyID != rec.KeyID {
		t.Fatalf("%+v", rep)
	}
	if !HasRecord("acme", f.hash) || HasRecord("acme", strings.Repeat("0", 64)) {
		t.Fatal("HasRecord names the manifest hash")
	}
	if fi, _ := os.Stat(SignedRecordPath("acme")); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("record mode %o", fi.Mode().Perm())
	}
}

// The tq binary is not covered (PRD §8.3 names hooks, shims and manifest).
func TestBinaryIsNotCovered(t *testing.T) {
	newFixture(t, "acme")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range Covered("acme") {
		if k == Key(exe) {
			t.Fatalf("the running binary is covered: %s", k)
		}
	}
}

// A flipped byte in a signed value fails the signature.
func TestFlippedByteFailsVerification(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	p := SignedRecordPath("acme")
	raw, _ := os.ReadFile(p)
	i := strings.Index(string(raw), f.hash)
	if i < 0 {
		t.Fatal("the record holds the manifest hash")
	}
	flip := byte('0')
	if raw[i] == '0' {
		flip = '1'
	}
	raw[i] = flip
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	rep := f.verify()
	if !rep.Exists || rep.Invalid != "integrity record signature invalid" || rep.Trusted(f.hash) {
		t.Fatalf("%+v", rep)
	}
	v := Check("acme", f.hash)
	if v.OK || !v.Mismatch || v.Kind != KindMismatch || !strings.Contains(v.Reason, "integrity record signature invalid") || !strings.Contains(v.Reason, "tq hooks verify acme") {
		t.Fatalf("%+v", v)
	}
}

// Whitespace is not signed: a re-indented record still verifies; a record
// whose signature is edited does not; one that is not JSON is unreadable.
func TestOnlyValuesAreSigned(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	p := SignedRecordPath("acme")
	rec := readRec(t, "acme")
	compact, _ := json.Marshal(rec)
	if err := os.WriteFile(p, compact, 0o600); err != nil {
		t.Fatal(err)
	}
	if rep := f.verify(); !rep.Trusted(f.hash) {
		t.Fatalf("re-serialized record must verify: %+v", rep)
	}
	rec.Signature = strings.Repeat("ab", 32)
	b, _ := json.Marshal(rec)
	os.WriteFile(p, b, 0o600)
	if rep := f.verify(); rep.Invalid != "integrity record signature invalid" {
		t.Fatalf("%+v", rep)
	}
	rec.Signature = ""
	rec.Revoked = true
	b, _ = json.Marshal(rec)
	os.WriteFile(p, b, 0o600)
	if rep := f.verify(); rep.Invalid != "integrity record signature invalid" || rep.Revoked {
		t.Fatalf("an unsigned revocation is not one: %+v", rep)
	}
	os.WriteFile(p, []byte("{not json"), 0o600)
	if rep := f.verify(); !rep.Exists || !strings.HasPrefix(rep.Invalid, "integrity record unreadable") {
		t.Fatalf("%+v", rep)
	}
}

// A record signed with another key reports "integrity key changed", naming
// both key ids, not an opaque signature failure; a human re-signs.
func TestKeyChangeIsReportedByID(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	old := readRec(t, "acme").KeyID
	if KeyFromFile() {
		t.Fatal("the keychain worked: no fallback file")
	}
	keyring.MockInit() // a fresh keychain: no key
	invalidate()
	if rep := f.verify(); rep.Invalid != "integrity key missing (the record was signed with key "+old+")" {
		t.Fatalf("%+v", rep)
	}
	if err := keyring.Set(Service, KeyAccount, strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	invalidate()
	rep := f.verify()
	want := "integrity key changed (the record was signed with key " + old + ", the key now is " + rep.KeyID + ")"
	if rep.Invalid != want || rep.KeyID == old || IsTrusted("acme", f.hash) {
		t.Fatalf("%+v want %q", rep, want)
	}
	f.allow() // re-signed under the new key
	if readRec(t, "acme").KeyID != rep.KeyID {
		t.Fatal("the record carries the new key id")
	}
}

// The record is for one workspace: copied under another name it is invalid,
// and the same manifest bytes trusted for acme are not trusted for evil.
func TestTrustIsKeyedByWorkspaceName(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	if IsTrusted("evil", f.hash) || HasRecord("evil", f.hash) {
		t.Fatal("the record belongs to acme alone")
	}
	raw, _ := os.ReadFile(SignedRecordPath("acme"))
	write(t, SignedRecordPath("evil"), string(raw))
	rep, err := Verify("evil")
	if err != nil || !rep.Exists || !strings.Contains(rep.Invalid, `names workspace "acme"`) || IsTrusted("evil", f.hash) {
		t.Fatalf("%+v %v", rep, err)
	}
}

// Editing the protected manifest makes the workspace untrusted, with the
// manifest named as the changed file.
func TestEditedManifestIsNamed(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	f.append(f.mp, "language: en\n")
	h2, _ := HashFile(f.mp)
	v := Check("acme", h2)
	if v.OK || !v.Mismatch || !strings.Contains(v.Reason, "changed "+Key(f.mp)) || !strings.Contains(v.Reason, "tq hooks verify acme") {
		t.Fatalf("%+v", v)
	}
	if IsTrusted("acme", f.hash) {
		t.Fatal("the old hash is not trusted either: the file changed")
	}
	c := changeFor(t, f.verify(), f.mp)
	if c.Kind != "changed" || c.Old != f.hash || c.New != h2 {
		t.Fatalf("%+v", c)
	}
	f.hash = h2
	f.allow()
	if rep := f.verify(); len(rep.Changes) != 0 {
		t.Fatalf("%v", rep.Changes)
	}
}

// Editing hooks/hooks.json or scripts/tq_hook.sh of the installed plugin
// makes the workspace untrusted with that file named; a new file under
// hooks/ is named as added; tq allow re-signs.
func TestPluginHookChangesAreNamed(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	f.append(f.hooksJSON, "\n")
	v := Check("acme", f.hash)
	if v.OK || !strings.Contains(v.Reason, "changed "+Key(f.hooksJSON)) {
		t.Fatalf("%+v", v)
	}
	f.allow()

	f.append(f.hookSH, "echo tampered\n")
	if c := changeFor(t, f.verify(), f.hookSH); c.Kind != "changed" || IsTrusted("acme", f.hash) {
		t.Fatalf("%+v", c)
	}
	f.allow()

	added := filepath.Join(f.plugin, "hooks", "new.sh")
	write(t, added, "#!/bin/sh\n")
	if c := changeFor(t, f.verify(), added); c.Kind != "added" || c.Old != "" || c.New == "" {
		t.Fatalf("%+v", c)
	}
	if v := Check("acme", f.hash); v.OK || !strings.Contains(v.Reason, "added "+Key(added)) {
		t.Fatalf("%+v", v)
	}
	f.allow()
	os.Remove(added)
	if c := changeFor(t, f.verify(), added); c.Kind != "removed" || c.New != "" {
		t.Fatalf("%+v", c)
	}
	f.allow()
}

// A new plugin version folder is summarised by version and the number of
// hook files that differ between the two folders.
func TestPluginVersionChangeIsSummarised(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	next := filepath.Join(f.claude, "plugins", "cache", "tentaqles", "tentaqles", "0.17.0")
	write(t, filepath.Join(next, ".claude-plugin", "plugin.json"), `{"name":"tentaqles","version":"0.17.0"}`)
	write(t, filepath.Join(next, "hooks", "hooks.json"), `{"hooks":{}}`)            // same
	write(t, filepath.Join(next, "scripts", "tq_hook.sh"), "#!/usr/bin/env bash\n") // differs
	write(t, filepath.Join(next, "scripts", "new.py"), "print()\n")                 // new
	rep := f.verify()
	if rep.PluginFrom != "0.16.0" || rep.PluginTo != "0.17.0" || rep.PluginFiles != 3 {
		t.Fatalf("plugin.json, tq_hook.sh and new.py differ: %+v", rep)
	}
	if rep.Trusted(f.hash) {
		t.Fatal("a new plugin version is a change")
	}
}

// A covered file absent when signed is recorded as absent; it appearing
// later is a change. A file swapped for a directory is a change too.
func TestAbsentCoveredFileAppearingIsAChange(t *testing.T) {
	f := newFixture(t, "acme")
	marker := f.marker
	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(marker)
	f.allow()
	if h, ok := readRec(t, "acme").Files[Key(marker)]; !ok || h != "" {
		t.Fatalf("an absent marker is recorded as absent: %q %v", h, ok)
	}
	write(t, marker, string(body))
	if c := changeFor(t, f.verify(), marker); c.Kind != "added" {
		t.Fatalf("%+v", c)
	}
	f.allow()
	os.Remove(f.hooksJSON)
	if err := os.Mkdir(f.hooksJSON, 0o700); err != nil {
		t.Fatal(err)
	}
	if c := changeFor(t, f.verify(), f.hooksJSON); c.Kind != "removed" && c.New != irregular {
		t.Fatalf("%+v", c)
	}
}

// settings.json is covered through its enforcing keys only: Claude's own
// rewrite of the theme or the model is not a change; a hook, a plugin
// switch, an env entry or a status line is; a broken file is.
func TestSettingsEnforcingKeysOnly(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	write(t, f.settings, "{\n  \"model\": \"opus\",\n  \"hooks\": {\"PreToolUse\": []},\n  \"theme\": \"light\"\n}\n")
	if rep := f.verify(); !rep.Trusted(f.hash) {
		t.Fatalf("theme, model and formatting are not covered: %v", rep.Changes)
	}
	for _, edit := range []string{
		`{"hooks":{}}`,
		`{"hooks":{"PreToolUse":[]},"enabledPlugins":{"tentaqles@tentaqles":false}}`,
		`{"hooks":{"PreToolUse":[]},"env":{"TQ_HOME":"/forged"}}`,
		`{"hooks":{"PreToolUse":[]},"statusLine":{"type":"command","command":"x"}}`,
		`{"hooks":{"PreToolUse":[]},"disableAllMcpServers":false}`,
		`{"hooks":{"PreToolUse":[]},"enabledMcpjsonServers":["evil"]}`,
		`not json`,
	} {
		write(t, f.settings, edit)
		if c := changeFor(t, f.verify(), f.settings+blockSuffix); c.Kind != "changed" {
			t.Fatalf("%s: %+v", edit, c)
		}
	}
	os.Remove(f.settings)
	if c := changeFor(t, f.verify(), f.settings+blockSuffix); c.Kind != "changed" {
		t.Fatalf("a deleted settings.json drops the hooks: %+v", c)
	}
}

// The switch that turns every hook off, the credential and telemetry
// helpers, the AWS hooks, the login method, and any key ending in Helper or
// Command are covered; permissions (which tq never touches) is not.
func TestSettingsCoversHelpersAndTheHookSwitch(t *testing.T) {
	f := newFixture(t, "acme")
	base := `"hooks":{"PreToolUse":[]}`
	write(t, f.settings, "{"+base+"}")
	f.allow()
	write(t, f.settings, "{"+base+`,"permissions":{"allow":["Bash(ls)"],"defaultMode":"acceptEdits"}}`)
	if rep := f.verify(); !rep.Trusted(f.hash) {
		t.Fatalf("permissions is not covered: %v", rep.Changes)
	}
	for _, kv := range []string{
		`"disableAllHooks":true`,
		`"apiKeyHelper":"/tmp/key.sh"`,
		`"otelHeadersHelper":"/tmp/h.sh"`,
		`"awsAuthRefresh":"aws sso login"`,
		`"awsCredentialExport":"/tmp/creds.sh"`,
		`"forceLoginMethod":"console"`,
		`"someFutureHelper":"/tmp/x"`,
		`"fileSuggestionCommand":"/tmp/y"`,
	} {
		write(t, f.settings, "{"+base+","+kv+"}")
		if c := changeFor(t, f.verify(), f.settings+blockSuffix); c.Kind != "changed" {
			t.Fatalf("%s must be covered: %+v", kv, c)
		}
	}
}

// The whole loaded plugin folder is covered (bin/, the Python package, the
// plugin's settings.json) except skills/, agents/, templates/, tests/ and
// __pycache__ at any depth.
func TestPluginRootIsCoveredButPromptsAndCaches(t *testing.T) {
	f := newFixture(t, "acme")
	write(t, filepath.Join(f.plugin, "tentaqles", "memory", "store.py"), "x = 1\n")
	write(t, filepath.Join(f.plugin, "tentaqles", "memory", "__pycache__", "store.cpython-312.pyc"), "pyc")
	f.allow()
	planted := filepath.Join(f.plugin, "bin", "tq")
	write(t, planted, "#!/bin/sh\nexit 0\n")
	if c := changeFor(t, f.verify(), planted); c.Kind != "added" {
		t.Fatalf("a planted bin/tq is reported: %+v", c)
	}
	os.Remove(planted)
	for _, p := range []string{
		filepath.Join(f.plugin, "tentaqles", "memory", "store.py"),
		filepath.Join(f.plugin, "settings.json"),
		filepath.Join(f.plugin, "README.md"),
	} {
		write(t, p, "changed\n")
		if rep := f.verify(); len(rep.Changes) != 1 || rep.Changes[0].Path != Key(p) {
			t.Fatalf("%s is covered: %v", p, rep.Changes)
		}
		f.allow()
	}
	for _, p := range []string{
		filepath.Join(f.plugin, "skills", "x", "SKILL.md"),
		filepath.Join(f.plugin, "agents", "critic.md"),
		filepath.Join(f.plugin, "templates", "adr.md"),
		filepath.Join(f.plugin, "tests", "test_x.py"),
		filepath.Join(f.plugin, "tentaqles", "memory", "__pycache__", "store.cpython-312.pyc"),
		filepath.Join(f.plugin, "__pycache__", "x.pyc"),
	} {
		write(t, p, "changed\n")
		if rep := f.verify(); !rep.Trusted(f.hash) {
			t.Fatalf("%s is not covered: %v", p, rep.Changes)
		}
	}
}

// installed_plugins.json decides which folder is covered (the user-scope
// tentaqles entry whose installPath is a plugin folder, before the highest
// version), and its tentaqles installPath and version are covered: an
// entry pointed at another folder is a change, a timestamp is not.
func TestInstalledPluginsDecidesAndIsCovered(t *testing.T) {
	f := newFixture(t, "acme")
	higher := filepath.Join(f.claude, "plugins", "cache", "tentaqles", "tentaqles", "0.99.0")
	write(t, filepath.Join(higher, ".claude-plugin", "plugin.json"), `{"name":"tentaqles","version":"0.99.0"}`)
	installed := filepath.Join(f.claude, "plugins", "installed_plugins.json")
	doc := func(path, stamp string) string {
		b, _ := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{
			"tentaqles@tentaqles": []any{
				map[string]any{"scope": "project", "installPath": higher, "version": "0.99.0", "lastUpdated": stamp},
				map[string]any{"scope": "user", "installPath": path, "version": "0.16.0", "installedAt": stamp, "lastUpdated": stamp},
			},
			"other@market": []any{map[string]any{"scope": "user", "installPath": "/x", "version": "1", "lastUpdated": stamp}},
		}})
		return string(b)
	}
	write(t, installed, doc(f.plugin, "2026-11-05T00:00:00Z"))
	if got := PluginRoot(f.claude); got != filepath.Clean(f.plugin) {
		t.Fatalf("the user-scope installPath wins over the highest version: %s", got)
	}
	f.allow()
	if readRec(t, "acme").Plugin != Key(f.plugin) {
		t.Fatal("the record names the installed folder")
	}
	write(t, installed, doc(f.plugin, "2026-12-01T00:00:00Z"))
	if rep := f.verify(); !rep.Trusted(f.hash) {
		t.Fatalf("timestamps and other plugins are not covered: %v", rep.Changes)
	}
	write(t, installed, doc(higher, "2026-12-01T00:00:00Z"))
	rep := f.verify()
	if c := changeFor(t, rep, installed+blockSuffix); c.Kind != "changed" {
		t.Fatalf("%+v", c)
	}
	if rep.PluginFrom != "0.16.0" || rep.PluginTo != "0.99.0" {
		t.Fatalf("the installPath moved the covered folder: %+v", rep)
	}
	write(t, installed, "not json")
	if got := PluginRoot(f.claude); got != filepath.Clean(higher) {
		t.Fatalf("an unreadable file falls back to the highest version: %s", got)
	}
}

// PluginBump says where the changes are (the two plugin folders and
// installed_plugins.json#tq), never what they hold: a new version folder
// whose hooks.json was emptied is still a bump, and only the release
// comparison refuses it. A change beside the bump makes it no bump.
func TestPluginBumpSaysWhereNotWhat(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	publish(t, f.claude, releaseFiles())
	files := releaseFiles()
	files["hooks/hooks.json"] = `{"hooks":{}}`
	install(t, f, files)
	os.RemoveAll(f.plugin) // Claude Code may drop the old version folder
	rep := f.verify()
	if !rep.PluginBump || rep.PluginFrom != "0.16.0" || rep.PluginTo != "0.17.0" {
		t.Fatalf("%+v", rep)
	}
	rel, err := CheckRelease("acme", rep)
	if err != nil || len(rel.Mismatches) != 1 || rel.Mismatches[0] != "hooks/hooks.json: differs from the release" {
		t.Fatalf("an emptied hooks.json is not the release: %+v %v", rel, err)
	}
	write(t, f.settings, `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[]}]}}`)
	if rep := f.verify(); rep.PluginBump {
		t.Fatalf("a settings change beside the bump: %+v", rep)
	}
	write(t, f.settings, `{"theme":"dark","hooks":{"PreToolUse":[]}}`)
	f.allow()
	if rep := f.verify(); rep.PluginBump || rep.PluginFrom != "" {
		t.Fatalf("no bump: %+v", rep)
	}
}

// The critic's sequence: after a signature, delete the record, the legacy
// snapshot and the key (file or keychain), plant a marker: the machine
// marker trust/.signed remains, so the marker does not count.
func TestDeletingKeySnapshotAndRecordDoesNotRestoreLegacy(t *testing.T) {
	for _, mode := range []string{"key-file", "keychain"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, "acme")
			if mode == "key-file" {
				keyring.MockInitWithError(errors.New("no keychain"))
				t.Cleanup(keyring.MockInit)
			}
			f.allow()
			if !exists(SignedMarkerPath()) {
				t.Fatal("the first signature writes trust/.signed")
			}
			os.Remove(SignedRecordPath("acme"))
			os.Remove(LegacyListPath())
			if mode == "key-file" {
				os.Remove(KeyFile())
			} else {
				keyring.MockInit()
			}
			write(t, LegacyRecordPath(f.hash), "")
			write(t, RecordPath("acme", f.hash), "")
			invalidate()
			if v := Check("acme", f.hash); v.OK {
				t.Fatalf("a planted marker must not count after a signature: %+v", v)
			}
			if len(LegacyMarkers("acme", f.hash)) != 0 {
				t.Fatal("no marker counts")
			}
		})
	}
}

// Files under $TQ_HOME/shims are covered once the folder exists.
func TestShimsAreCovered(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	shim := filepath.Join(f.home, ".tentaqles", "shims", "claude")
	write(t, shim, "#!/bin/sh\n")
	if c := changeFor(t, f.verify(), shim); c.Kind != "added" {
		t.Fatalf("%+v", c)
	}
	f.allow()
	f.append(shim, "exec evil\n")
	if c := changeFor(t, f.verify(), shim); c.Kind != "changed" {
		t.Fatalf("%+v", c)
	}
}

// The shims of ADR-0016 are one folder per identity: an identity's record covers its own folder
// and not another's, so writing one client's shims does not make another client untrusted.
func TestShimsAreCoveredPerIdentity(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	own := filepath.Join(f.home, ".tentaqles", "shims", "acme", "open")
	other := filepath.Join(f.home, ".tentaqles", "shims", "beta", "open")
	write(t, other, "#!/bin/sh\n")
	if rep := f.verify(); !rep.Trusted(f.hash) {
		t.Fatalf("another identity's shim changed this record: %v", rep.Changes)
	}
	write(t, own, "#!/bin/sh\n")
	if c := changeFor(t, f.verify(), own); c.Kind != "added" {
		t.Fatalf("%+v", c)
	}
}

// Keys are canonical: a record signed from a shell whose TQ_HOME is spelled
// with forward slashes, or in another case on Windows, verifies from the
// usual spelling with no phantom change, and the reverse.
func TestKeysAreCanonical(t *testing.T) {
	f := newFixture(t, "acme")
	home := os.Getenv("TQ_HOME")
	spellings := []string{filepath.ToSlash(home)}
	if runtime.GOOS == "windows" {
		spellings = append(spellings, strings.ToUpper(home), strings.ReplaceAll(strings.ToLower(home), `\`, "/"))
	}
	for _, s := range spellings {
		t.Setenv("TQ_HOME", s)
		invalidate()
		f.allow()
		t.Setenv("TQ_HOME", home)
		invalidate()
		if rep := f.verify(); !rep.Trusted(f.hash) {
			t.Fatalf("signed with TQ_HOME=%q: phantom changes %v", s, rep.Changes)
		}
	}
	if runtime.GOOS == "windows" {
		if Key(`C:\x\Y`) != Key("c:/x/y") || Key(`C:\x\settings.json#tq`) != "c:/x/settings.json#tq" {
			t.Fatalf("%s %s", Key(`C:\x\Y`), Key(`C:\x\settings.json#tq`))
		}
	}
}

// A root reached through a link (an identity dir moved elsewhere and linked
// back, doctor's identity-dir-linked) gets the key Key gives it: a symlink
// is resolved to where the folder lives; a Windows junction, which Go
// treats as a folder (Lstat and EvalSymlinks do not follow mount points),
// keeps its own path. Either way the derived key and Key agree, and a
// change behind the link is seen.
func TestLinkedRootIsKeyedAsKeySays(t *testing.T) {
	f := newFixture(t, "acme")
	target := filepath.Join(t.TempDir(), "claude-elsewhere")
	if err := os.Rename(f.claude, target); err != nil {
		t.Fatal(err)
	}
	if err := makeDirLink(target, f.claude); err != nil {
		t.Skipf("cannot create a directory link here: %v", err)
	}
	invalidate()
	want := Key(f.settings + blockSuffix)
	found := false
	for _, k := range Covered("acme") {
		if k == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("Covered must use Key's key %s: %v", want, Covered("acme"))
	}
	f.allow()
	write(t, filepath.Join(target, "settings.json"), `{"hooks":{}}`)
	if c := changeFor(t, f.verify(), f.settings+blockSuffix); c.Kind != "changed" {
		t.Fatalf("%+v", c)
	}
}

// The plugin covered is the highest installed version, by number.
func TestPluginRootPicksHighestVersion(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"0.9.0", "0.16.0", "0.10.0"} {
		write(t, filepath.Join(dir, "plugins", "cache", "tentaqles", "tentaqles", v, ".claude-plugin", "plugin.json"), "{}")
	}
	write(t, filepath.Join(dir, "plugins", "cache", "tentaqles", "tentaqles", "9.9.9", "hooks", "hooks.json"), "{}") // no plugin.json: not an install
	want := filepath.Join(dir, "plugins", "cache", "tentaqles", "tentaqles", "0.16.0")
	if got := PluginRoot(dir); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if PluginRoot(t.TempDir()) != "" {
		t.Fatal("no plugin: no root")
	}
}

// When the OS keychain is unavailable the key falls back to
// $TQ_HOME/integrity.key (0600); records sign and verify with it, and the
// file keeps being the key once the keychain is back, so nothing changes
// under the user.
func TestKeychainUnavailableFallsBackToFile(t *testing.T) {
	f := newFixture(t, "acme")
	keyring.MockInitWithError(errors.New("no keychain on this box"))
	t.Cleanup(keyring.MockInit)
	f.allow()
	if !KeyFromFile() {
		t.Fatal("the fallback file must hold the key")
	}
	if fi, _ := os.Stat(KeyFile()); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %o", fi.Mode().Perm())
	}
	raw, _ := os.ReadFile(KeyFile())
	if len(strings.TrimSpace(string(raw))) != 64 {
		t.Fatalf("the key file holds the hex key")
	}
	keyring.MockInit() // the keychain works again, but holds no key
	invalidate()
	if rep := f.verify(); !rep.Trusted(f.hash) {
		t.Fatalf("the file key still signs: %+v", rep)
	}
	if _, err := keyring.Get(Service, KeyAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("no key is moved into the keychain behind the user's back")
	}
}

// Verifying never creates a key; a malformed key is an error that names
// the fix and never a value; the record never holds the key.
func TestKeySources(t *testing.T) {
	f := newFixture(t, "acme")
	if Check("acme", f.hash).OK {
		t.Fatal("nothing signed")
	}
	if _, err := keyring.Get(Service, KeyAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("a check must not create a key")
	}
	if err := keyring.Set(Service, KeyAccount, "zz"); err != nil {
		t.Fatal(err)
	}
	invalidate()
	err := Allow("acme", f.hash)
	if err == nil || !strings.Contains(err.Error(), "malformed") || strings.Contains(err.Error(), "zz") {
		t.Fatalf("%v", err)
	}
	keyring.MockInit()
	invalidate()
	f.allow()
	k, _ := keyring.Get(Service, KeyAccount)
	if len(k) != 64 {
		t.Fatalf("the generated key is 32 bytes hex in the keychain")
	}
	raw, _ := os.ReadFile(SignedRecordPath("acme"))
	if strings.Contains(string(raw), k) {
		t.Fatal("the record never holds the key")
	}
}

// Signing is human-only at the lowest layer: with HumanCheck refusing,
// Allow, Deny and the legacy list refuse, write nothing and create no key.
func TestSigningRefusesWithoutAHuman(t *testing.T) {
	f := newFixture(t, "acme")
	refusal := errors.New("human-only: refused")
	prev := HumanCheck
	var asked []string
	HumanCheck = func(what string) error { asked = append(asked, what); return refusal }
	t.Cleanup(func() { HumanCheck = prev })
	if err := Allow("acme", f.hash); !errors.Is(err, refusal) {
		t.Fatalf("Allow: %v", err)
	}
	if _, err := SignRecord("acme", f.hash); !errors.Is(err, refusal) {
		t.Fatalf("SignRecord: %v", err)
	}
	if err := Deny("acme", f.hash); !errors.Is(err, refusal) {
		t.Fatalf("Deny: %v", err)
	}
	if _, _, err := LegacyListWithout(nil); !errors.Is(err, refusal) {
		t.Fatalf("LegacyListWithout: %v", err)
	}
	if exists(SignedRecordPath("acme")) || exists(LegacyListPath()) || KeyFromFile() {
		t.Fatal("nothing may be written")
	}
	if _, err := keyring.Get(Service, KeyAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("no key may be created")
	}
	if len(asked) != 4 || asked[0] != "tq: sign the integrity record of acme" {
		t.Fatalf("%q", asked)
	}
}

// Deny writes a signed tombstone that trusts nothing and is never removed:
// deleting it does not bring a legacy marker back.
func TestDenyLeavesASignedTombstone(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	if err := Deny("acme", f.hash); err != nil {
		t.Fatal(err)
	}
	rec := readRec(t, "acme")
	if !rec.Revoked || rec.ManifestHash != "" || len(rec.Files) != 0 || rec.Signature == "" {
		t.Fatalf("%+v", rec)
	}
	rep := f.verify()
	if !rep.Exists || !rep.Revoked || rep.Invalid != "" || rep.Trusted(f.hash) {
		t.Fatalf("%+v", rep)
	}
	v := Check("acme", f.hash)
	if v.OK || v.Mismatch || v.Kind != KindUntrusted || !strings.Contains(v.Reason, "revoked") || !strings.Contains(v.Reason, "tq allow acme") {
		t.Fatalf("%+v", v)
	}
	if HasRecord("acme", f.hash) {
		t.Fatal("a tombstone is not a record for the hash")
	}
	if err := Deny("acme", f.hash); err != nil {
		t.Fatalf("a second deny is not an error: %v", err)
	}
	// Deleting the tombstone and planting a marker: still untrusted.
	os.Remove(SignedRecordPath("acme"))
	write(t, LegacyRecordPath(f.hash), "")
	write(t, RecordPath("acme", f.hash), "")
	invalidate()
	if v := Check("acme", f.hash); v.OK {
		t.Fatalf("a planted marker after a signature must not count: %+v", v)
	}
	f.allow()
}

// Legacy markers: before anything is signed they count, as for the tq that
// wrote them (hash-only for any name, name-keyed for its name). The first
// signature snapshots them; a listed marker keeps counting for its
// workspace, a marker created after the snapshot never counts, and a
// missing or forged snapshot makes none count. Allow replaces a marker
// with the signed record and drops it from the snapshot.
func TestLegacyMarkersCountOnlyFromTheSnapshot(t *testing.T) {
	f := newFixture(t, "acme")
	hb := strings.Repeat("b", 64)
	write(t, LegacyRecordPath(f.hash), "")
	write(t, RecordPath("beta", hb), "")
	if v := Check("acme", f.hash); !v.OK || v.Kind != KindLegacy {
		t.Fatalf("pre-snapshot hash-only marker: %+v", v)
	}
	if !IsTrusted("beta", hb) || IsTrusted("evil", hb) || !IsTrusted("evil", f.hash) {
		t.Fatal("name-keyed for its name, hash-only for any name")
	}
	if exists(LegacyListPath()) {
		t.Fatal("a check writes nothing")
	}

	// The first signature (another workspace) takes the snapshot.
	g := newFixtureIn(t, f, "gamma")
	g.allow()
	if !exists(LegacyListPath()) {
		t.Fatal("the first signature writes trust/legacy.json")
	}
	invalidate()
	if v := Check("acme", f.hash); !v.OK || v.Kind != KindLegacy {
		t.Fatalf("a listed marker still counts: %+v", v)
	}
	if !IsTrusted("beta", hb) {
		t.Fatal("beta is listed")
	}
	planted := strings.Repeat("c", 64)
	write(t, LegacyRecordPath(planted), "")
	write(t, RecordPath("delta", planted), "")
	invalidate()
	if IsTrusted("delta", planted) || len(LegacyMarkers("delta", planted)) != 0 {
		t.Fatal("a marker created after the snapshot never counts")
	}
	if !HasLegacyRecord(planted) || !HasLegacyNameRecord("delta", planted) {
		t.Fatal("the files are there; they do not count")
	}

	// Allow replaces acme's marker and drops it from the snapshot.
	f.allow()
	if HasLegacyRecord(f.hash) {
		t.Fatal("Allow removes the marker")
	}
	l, ok := verifiedLegacy(readKey().key)
	if !ok || strings.Contains(strings.Join(l.Markers, ","), f.hash) || !strings.Contains(strings.Join(l.Markers, ","), "beta/"+hb) {
		t.Fatalf("snapshot %+v %v", l, ok)
	}

	// A forged or deleted snapshot: no marker counts.
	raw, _ := os.ReadFile(LegacyListPath())
	write(t, LegacyListPath(), strings.Replace(string(raw), `"markers": [`, `"markers": [`+"\n    \""+planted+"\",", 1))
	invalidate()
	if IsTrusted("beta", hb) || IsTrusted("delta", planted) {
		t.Fatal("a snapshot that does not verify allows nothing")
	}
	os.Remove(LegacyListPath())
	invalidate()
	if IsTrusted("beta", hb) {
		t.Fatal("a deleted snapshot allows nothing once a key exists")
	}
}

// newFixtureIn adds workspace name next to f's, in the same home.
func newFixtureIn(t *testing.T, f *fixture, name string) *fixture {
	t.Helper()
	g := &fixture{t: t, name: name, home: f.home, base: f.base, root: filepath.Join(f.base, name)}
	g.marker = filepath.Join(g.root, manifest.FileName)
	write(t, g.marker, string(manifest.MarkerBytes(name)))
	g.mp = paths.ManifestPath(name)
	write(t, g.mp, "schema: tentaqles-client-v2\nclient: "+name+"\ngit: { email: "+name+"@x.io }\n")
	g.hash, _ = HashFile(g.mp)
	return g
}

// Within a memo scope a workspace is verified once however often it is
// asked; a signature clears the memo; outside a scope nothing is cached.
func TestVerifyOncePerScope(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	end := MemoScope()
	before := Verifications()
	for i := 0; i < 3; i++ {
		if !IsTrusted("acme", f.hash) {
			t.Fatal("trusted")
		}
		f.verify()
	}
	if n := Verifications() - before; n != 1 {
		t.Fatalf("%d verifications in one scope", n)
	}
	f.allow() // a signature clears the memo
	f.verify()
	if n := Verifications() - before; n != 2 {
		t.Fatalf("%d verifications", n)
	}
	end()
	f.append(f.hooksJSON, " ")
	if IsTrusted("acme", f.hash) {
		t.Fatal("outside a scope nothing is cached")
	}
}

func TestInvalidKeysAreNeverTrusted(t *testing.T) {
	newFixture(t, "acme")
	h := strings.Repeat("0123456789abcdef", 4)
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "../acme"} {
		if err := Allow(name, h); err == nil {
			t.Fatalf("Allow(%q) must refuse", name)
		}
		if IsTrusted(name, h) {
			t.Fatalf("IsTrusted(%q) must be false", name)
		}
		if _, err := Verify(name); err == nil {
			t.Fatalf("Verify(%q) must refuse", name)
		}
	}
	if err := Allow("acme", "../"+h); err == nil {
		t.Fatal("a hash with a separator must be refused")
	}
	if err := Deny("acme", "../"+h); err == nil {
		t.Fatal("Deny refuses it too")
	}
}

func TestHashChangesWhenFileChanges(t *testing.T) {
	f := filepath.Join(t.TempDir(), "m.yaml")
	os.WriteFile(f, []byte("a: 1\n"), 0o600)
	h1, _ := HashFile(f)
	os.WriteFile(f, []byte("a: 2\n"), 0o600)
	h2, _ := HashFile(f)
	if h1 == h2 {
		t.Fatal("hash must change")
	}
}

// Covered is sorted, canonical and names each key once.
func TestCoveredList(t *testing.T) {
	f := newFixture(t, "acme")
	got := Covered("acme")
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("not sorted/unique at %d: %v", i, got)
		}
	}
	has := func(p string) bool {
		for _, g := range got {
			if g == Key(p) {
				return true
			}
		}
		return false
	}
	for _, p := range []string{f.mp, f.marker, f.settings + blockSuffix, f.hooksJSON, f.hookSH} {
		if !has(p) {
			t.Errorf("%s not covered: %v", Key(p), got)
		}
	}
	for _, g := range got {
		if strings.Contains(g, `\`) {
			t.Fatalf("keys use forward slashes: %s", g)
		}
	}
}

func TestChangeString(t *testing.T) {
	c := Change{Path: "p", Kind: "changed", Old: strings.Repeat("a", 64), New: ""}
	if c.String() != "changed p (aaaaaaaaaaaa -> absent)" {
		t.Fatal(c.String())
	}
}
