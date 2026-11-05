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
// and an installed plugin, a Codex identity with its hook files, and a
// stand-in for the tq binary. The OS keychain is go-keyring's mock.
type fixture struct {
	t                                   *testing.T
	name, home, base, root, mp, hash    string
	claude, codex, plugin, exe, marker  string
	settings, hooksJSON, hookSH, cfgTOM string
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

const codexBlock = "# >>> managed by tq: edit with tq settings render, not by hand\napproval_policy = \"on-request\"\nsandbox_mode = \"workspace-write\"\n# <<< managed by tq\n"

func newFixture(t *testing.T, name string) *fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	keyring.MockInit()
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
	write(t, f.settings, "{}\n")
	f.plugin = filepath.Join(f.claude, "plugins", "cache", "tentaqles", "tentaqles", "0.16.0")
	write(t, filepath.Join(f.plugin, ".claude-plugin", "plugin.json"), `{"name":"tentaqles","version":"0.16.0"}`)
	f.hooksJSON = filepath.Join(f.plugin, "hooks", "hooks.json")
	write(t, f.hooksJSON, `{"hooks":{}}`)
	f.hookSH = filepath.Join(f.plugin, "scripts", "tq_hook.sh")
	write(t, f.hookSH, "#!/usr/bin/env bash\nexit 0\n")
	write(t, filepath.Join(f.plugin, "skills", "x", "SKILL.md"), "not covered\n")
	f.codex = paths.IdentityDir(name, "codex")
	write(t, filepath.Join(f.codex, "hooks.json"), `{"hooks":{}}`)
	f.cfgTOM = filepath.Join(f.codex, "config.toml")
	write(t, f.cfgTOM, "model = \"one\"\n"+codexBlock)
	f.exe = filepath.Join(home, "bin", "tq.exe")
	write(t, f.exe, "the tq binary\n")
	prev := Executable
	Executable = func() (string, error) { return f.exe, nil }
	t.Cleanup(func() { Executable = prev })
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
	if !IsTrusted(f.name, f.hash) {
		f.t.Fatal("Allow must trust the state it signed")
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

// changeFor returns the change naming path, or fails.
func changeFor(t *testing.T, rep Report, path string) Change {
	t.Helper()
	for _, c := range rep.Changes {
		if c.Path == path {
			return c
		}
	}
	t.Fatalf("no change names %s; changes: %v", path, rep.Changes)
	return Change{}
}

func TestKeyServiceMatchesSecret(t *testing.T) {
	if Service != secret.Service || KeyAccount != "integrity-key" {
		t.Fatalf("%s %s", Service, KeyAccount)
	}
}

// A signed record round-trips: Allow writes trust/<name>.json with the
// schema, the workspace, the manifest hash, the covered files, a time and
// a signature; Verify reads it back as trusted.
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
	if rec.Schema != Schema || rec.Workspace != "acme" || rec.ManifestHash != f.hash || rec.SignedAt == "" || len(rec.Signature) != 64 {
		t.Fatalf("%+v", rec)
	}
	for _, p := range []string{f.mp, f.marker, f.settings, f.hooksJSON, f.hookSH, filepath.Join(f.plugin, ".claude-plugin", "plugin.json"),
		filepath.Join(f.codex, "hooks.json"), f.cfgTOM + blockSuffix, f.exe} {
		if rec.Files[p] == "" {
			t.Errorf("%s must be covered and present", p)
		}
	}
	if _, ok := rec.Files[filepath.Join(f.plugin, "skills", "x", "SKILL.md")]; ok {
		t.Error("skills are not covered")
	}
	if h := rec.Files[filepath.Join(f.home, ".tentaqles", "shims")]; h != "" {
		t.Error("no shims folder: nothing under it is covered")
	}
	rep := f.verify()
	if !rep.Exists || rep.Invalid != "" || !rep.Trusted(f.hash) || rep.Files != len(rec.Files) || rep.SignedAt != rec.SignedAt {
		t.Fatalf("%+v", rep)
	}
	if v := Check("acme", f.hash); !v.OK || v.Reason != "" {
		t.Fatalf("%+v", v)
	}
	if !HasRecord("acme", f.hash) || HasRecord("acme", strings.Repeat("0", 64)) {
		t.Fatal("HasRecord names the manifest hash")
	}
	if fi, _ := os.Stat(SignedRecordPath("acme")); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("record mode %o", fi.Mode().Perm())
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
	if v.OK || !v.Mismatch || !strings.Contains(v.Reason, "integrity record signature invalid") || !strings.Contains(v.Reason, "tq hooks verify acme") {
		t.Fatalf("%+v", v)
	}
}

// Whitespace is not signed: a re-indented record still verifies; a record
// whose signature is edited does not.
func TestOnlyValuesAreSigned(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	p := SignedRecordPath("acme")
	raw, _ := os.ReadFile(p)
	var rec Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
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
	os.WriteFile(p, []byte("{not json"), 0o600)
	if rep := f.verify(); !rep.Exists || !strings.HasPrefix(rep.Invalid, "integrity record unreadable") {
		t.Fatalf("%+v", rep)
	}
}

// A record signed with another key fails: the mock keychain is emptied, a
// new key is generated, and the old record no longer verifies.
func TestRecordSignedWithAnotherKeyFails(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	if KeyFromFile() {
		t.Fatal("the keychain worked: no fallback file")
	}
	keyring.MockInit() // a fresh keychain: no key, so the next use generates one
	rep := f.verify()
	if rep.Invalid != "integrity record signature invalid" || IsTrusted("acme", f.hash) {
		t.Fatalf("%+v", rep)
	}
	f.allow() // re-signed under the new key
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
	if v.OK || !v.Mismatch || !strings.Contains(v.Reason, "changed "+f.mp) || !strings.Contains(v.Reason, "tq hooks verify acme") {
		t.Fatalf("%+v", v)
	}
	if IsTrusted("acme", f.hash) {
		t.Fatal("the old hash is not trusted either: the file changed")
	}
	c := changeFor(t, f.verify(), f.mp)
	if c.Kind != "changed" || c.Old != f.hash || c.New != h2 {
		t.Fatalf("%+v", c)
	}
	// tq allow re-signs the state as it is now and trust returns.
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
	if v.OK || !strings.Contains(v.Reason, "changed "+f.hooksJSON) {
		t.Fatalf("%+v", v)
	}
	if c := changeFor(t, f.verify(), f.hooksJSON); c.Kind != "changed" {
		t.Fatalf("%+v", c)
	}
	f.allow()

	f.append(f.hookSH, "echo tampered\n")
	if c := changeFor(t, f.verify(), f.hookSH); c.Kind != "changed" || IsTrusted("acme", f.hash) {
		t.Fatalf("%+v", c)
	}
	f.allow()

	added := filepath.Join(f.plugin, "hooks", "new.sh")
	write(t, added, "#!/bin/sh\n")
	rep := f.verify()
	if c := changeFor(t, rep, added); c.Kind != "added" || c.Old != "" || c.New == "" {
		t.Fatalf("%+v", c)
	}
	if v := Check("acme", f.hash); v.OK || !strings.Contains(v.Reason, "added "+added) {
		t.Fatalf("%+v", v)
	}
	f.allow()
	os.Remove(added)
	if c := changeFor(t, f.verify(), added); c.Kind != "removed" || c.New != "" {
		t.Fatalf("%+v", c)
	}
	f.allow()
	if rep := f.verify(); !rep.Trusted(f.hash) {
		t.Fatalf("%+v", rep)
	}
}

// A covered file that is absent when signed is recorded as absent; it
// appearing later is a change. A file swapped for a directory or link is
// a change too.
func TestAbsentCoveredFileAppearingIsAChange(t *testing.T) {
	f := newFixture(t, "acme")
	os.Remove(f.settings)
	f.allow()
	rec, _, _ := readRecord("acme")
	if h, ok := rec.Files[f.settings]; !ok || h != "" {
		t.Fatalf("absent settings.json is recorded as absent: %q %v", h, ok)
	}
	write(t, f.settings, "{}\n")
	if c := changeFor(t, f.verify(), f.settings); c.Kind != "added" {
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

// The running tq binary is covered: another binary enforcing is a change.
func TestBinaryChangeIsNamed(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	write(t, f.exe, "another binary\n")
	if c := changeFor(t, f.verify(), f.exe); c.Kind != "changed" {
		t.Fatalf("%+v", c)
	}
	other := filepath.Join(f.home, "elsewhere", "tq")
	write(t, other, "a third binary\n")
	Executable = func() (string, error) { return other, nil }
	rep := f.verify()
	if c := changeFor(t, rep, other); c.Kind != "added" {
		t.Fatalf("%+v", c)
	}
}

// The tq-owned block of the Codex config.toml is covered, the rest of the
// file is the user's.
func TestCodexBlockIsCoveredNotTheRest(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	write(t, f.cfgTOM, "model = \"two\"\n"+codexBlock)
	if rep := f.verify(); !rep.Trusted(f.hash) {
		t.Fatalf("the user's keys are not covered: %v", rep.Changes)
	}
	write(t, f.cfgTOM, "model = \"two\"\n"+strings.Replace(codexBlock, "on-request", "never", 1))
	if c := changeFor(t, f.verify(), f.cfgTOM+blockSuffix); c.Kind != "changed" {
		t.Fatalf("%+v", c)
	}
	os.Remove(f.cfgTOM)
	if c := changeFor(t, f.verify(), f.cfgTOM+blockSuffix); c.Kind != "removed" {
		t.Fatalf("%+v", c)
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
	if rep := f.verify(); !rep.Trusted(f.hash) {
		t.Fatalf("%+v", rep)
	}
	keyring.MockInit() // the keychain works again, but holds no key
	if rep := f.verify(); !rep.Trusted(f.hash) {
		t.Fatalf("the file key still signs: %+v", rep)
	}
	if _, err := keyring.Get(Service, KeyAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("no key is moved into the keychain behind the user's back")
	}
}

// The keychain key wins when both exist; a malformed key is an error that
// names the fix and never a value.
func TestKeySources(t *testing.T) {
	f := newFixture(t, "acme")
	if err := keyring.Set(Service, KeyAccount, "zz"); err != nil {
		t.Fatal(err)
	}
	err := Allow("acme", f.hash)
	if err == nil || !strings.Contains(err.Error(), "malformed") || strings.Contains(err.Error(), "zz") {
		t.Fatalf("%v", err)
	}
	keyring.MockInit()
	f.allow()
	k, _ := keyring.Get(Service, KeyAccount)
	if len(k) != 64 {
		t.Fatalf("the generated key is 32 bytes hex in the keychain")
	}
	if _, err := Verify("acme"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{SignedRecordPath("acme")} {
		raw, _ := os.ReadFile(s)
		if strings.Contains(string(raw), k) {
			t.Fatal("the record never holds the key")
		}
	}
}

// A legacy marker of an older tq (trust/<name>/<hash> or trust/<hash>)
// counts until a signed record replaces it; Allow writes the signed record
// and removes the markers for that hash; Deny removes every form.
func TestLegacyRecordsCountUntilResigned(t *testing.T) {
	f := newFixture(t, "acme")
	write(t, LegacyRecordPath(f.hash), "")
	if !HasLegacyRecord(f.hash) || HasRecord("acme", f.hash) || !IsTrusted("acme", f.hash) {
		t.Fatal("a hash-only marker counts for the name resolve checked")
	}
	if rep := f.verify(); rep.Exists {
		t.Fatal("no signed record yet")
	}
	if err := Deny("acme", f.hash); err != nil {
		t.Fatal(err)
	}
	if HasLegacyRecord(f.hash) || IsTrusted("acme", f.hash) {
		t.Fatal("Deny removes the hash-only marker")
	}
	write(t, RecordPath("acme", f.hash), "")
	if !HasLegacyNameRecord("acme", f.hash) || !IsTrusted("acme", f.hash) || IsTrusted("evil", f.hash) {
		t.Fatal("a name-keyed marker counts for its name")
	}
	f.allow()
	if HasLegacyNameRecord("acme", f.hash) || !HasRecord("acme", f.hash) {
		t.Fatal("Allow replaces the marker with the signed record")
	}
	if _, err := os.Stat(filepath.Join(paths.TrustDir(), "acme")); err == nil {
		t.Fatal("the empty per-name folder goes")
	}
	// A signed record that fails is not rescued by a marker.
	write(t, LegacyRecordPath(f.hash), "")
	f.append(f.hooksJSON, " ")
	if IsTrusted("acme", f.hash) {
		t.Fatal("the signed record decides once it exists")
	}
	if err := Deny("acme", f.hash); err != nil {
		t.Fatal(err)
	}
	if IsTrusted("acme", f.hash) || HasLegacyRecord(f.hash) || HasRecord("acme", f.hash) {
		t.Fatal("deny must clear every form")
	}
	if err := Deny("acme", f.hash); err != nil {
		t.Fatalf("a second deny is not an error: %v", err)
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

// Covered is sorted and names each path once; the old layout's manifest in
// the root is the marker path.
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
			if g == p {
				return true
			}
		}
		return false
	}
	for _, p := range []string{f.mp, f.marker, f.settings, f.hooksJSON, f.hookSH, f.cfgTOM + blockSuffix, filepath.Join(f.codex, "hooks.json"), f.exe} {
		if !has(p) {
			t.Errorf("%s not covered: %v", p, got)
		}
	}
}

func TestChangeString(t *testing.T) {
	c := Change{Path: "p", Kind: "changed", Old: strings.Repeat("a", 64), New: ""}
	if c.String() != "changed p (aaaaaaaaaaaa -> absent)" {
		t.Fatal(c.String())
	}
}
