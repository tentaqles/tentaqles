package migrate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// mfEnv is a machine with a tq home and one base, no git needed.
type mfEnv struct {
	t    *testing.T
	base string
	cfg  *registry.Config
}

func newMfEnv(t *testing.T) *mfEnv {
	t.Helper()
	root := stTmpDir(t)
	home := filepath.Join(root, "home")
	stMkdir(t, home, filepath.Join(root, "tqhome"))
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	t.Setenv("TQ_HOME", filepath.Join(root, "tqhome"))
	e := &mfEnv{t: t, base: filepath.Join(home, "repos")}
	stMkdir(t, e.base)
	e.cfg = &registry.Config{Bases: []string{stNormalize(t, e.base)}}
	return e
}

// oldLayout writes a full manifest at <base>/<name>/.tentaqles.yaml and
// returns its path and hash.
func (e *mfEnv) oldLayout(name string, trusted bool) (string, string) {
	e.t.Helper()
	mp := filepath.Join(e.base, name, manifest.FileName)
	stWrite(e.t, mp, "# tentaqles workspace manifest — names only, never secrets\nschema: tentaqles-client-v2\nclient: "+name+"\ngit:\n  email: "+name+"@x.io\nidentities:\n  claude: {}\n")
	h, err := trust.HashFile(mp)
	if err != nil {
		e.t.Fatal(err)
	}
	if trusted {
		if err := trust.Allow(name, h); err != nil {
			e.t.Fatal(err)
		}
	}
	return mp, h
}

// protected writes a protected-layout workspace and returns the protected path.
func (e *mfEnv) protected(name string) string {
	e.t.Helper()
	stWrite(e.t, filepath.Join(e.base, name, manifest.FileName), string(manifest.MarkerBytes(name)))
	pp := paths.ManifestPath(name)
	stMkdir(e.t, filepath.Dir(pp))
	stWrite(e.t, pp, "schema: tentaqles-client-v2\nclient: "+name+"\ngit:\n  email: "+name+"@x.io\n")
	h, _ := trust.HashFile(pp)
	trust.Allow(name, h)
	return pp
}

// legacyTrust writes the hash-only record an older tq wrote.
func (e *mfEnv) legacyTrust(hash string) {
	e.t.Helper()
	stMkdir(e.t, paths.TrustDir())
	stWrite(e.t, trust.LegacyRecordPath(hash), "")
}

func (e *mfEnv) deps() Deps { return Deps{Cfg: e.cfg, Env: os.LookupEnv} }

func (e *mfEnv) journal() *Journal {
	e.t.Helper()
	j, err := Open("20260830T000000Z")
	if err != nil {
		e.t.Fatal(err)
	}
	return j
}

func TestManifestsStep_IsKnownAndNotDefault(t *testing.T) {
	known := KnownSteps()
	if known[len(known)-1] != "manifests" {
		t.Fatalf("manifests must be a known step, last in order: %v", known)
	}
	steps, err := Steps([]string{"manifests", "identity"})
	if err != nil || len(steps) != 2 || steps[0].Name() != "identity" || steps[1].Name() != "manifests" {
		t.Fatalf("%v %v", steps, err)
	}
}

func TestManifests_MovesJournalsAndKeepsTrust(t *testing.T) {
	e := newMfEnv(t)
	alphaRoot, alphaHash := e.oldLayout("alpha", true)
	betaRoot, betaHash := e.oldLayout("beta", false)
	gammaPP := e.protected("gamma")
	alphaBytes := stRead(t, alphaRoot)
	betaBytes := stRead(t, betaRoot)
	// An older tq created alpha's identity folder world-readable.
	if err := os.MkdirAll(filepath.Dir(paths.ManifestPath("alpha")), 0o755); err != nil {
		t.Fatal(err)
	}

	st := stepRegistry["manifests"]
	p, err := st.Plan(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 4 {
		t.Fatalf("two changes per old-layout workspace, got %+v", p.Changes)
	}
	if p.Changes[0].Kind != kindWriteProtectedManifest || p.Changes[0].Path != paths.ManifestPath("alpha") || !strings.Contains(p.Changes[0].Detail, "trust carries over") {
		t.Fatalf("%+v", p.Changes[0])
	}
	if p.Changes[1].Kind != kindWriteMarker || p.Changes[1].Path != alphaRoot || !p.Changes[1].Danger {
		t.Fatalf("%+v", p.Changes[1])
	}
	if !strings.Contains(p.Changes[2].Detail, "stays untrusted until tq allow beta") {
		t.Fatalf("%+v", p.Changes[2])
	}
	if len(p.Skipped) != 1 || !strings.Contains(p.Skipped[0], "gamma") || !strings.Contains(p.Skipped[0], "already protected") {
		t.Fatalf("%v", p.Skipped)
	}

	j := e.journal()
	if err := st.Apply(e.deps(), p, j); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// The root files are markers now; the manifests moved byte for byte.
	for _, c := range []struct{ name, root, body string }{{"alpha", alphaRoot, alphaBytes}, {"beta", betaRoot, betaBytes}} {
		if got := stRead(t, c.root); got != string(manifest.MarkerBytes(c.name)) {
			t.Fatalf("%s root is not the marker:\n%s", c.name, got)
		}
		if got := stRead(t, paths.ManifestPath(c.name)); got != c.body {
			t.Fatalf("%s protected copy differs from the original", c.name)
		}
	}
	if got := stRead(t, gammaPP); !strings.Contains(got, "gamma@x.io") {
		t.Fatal("an already protected manifest must be left alone")
	}
	// Trust carried over exactly: alpha trusted, beta untrusted.
	ra := resolve.Resolve(filepath.Join(e.base, "alpha"), e.cfg)
	if ra.Workspace == nil || !ra.Workspace.Protected || ra.Workspace.Hash != alphaHash {
		t.Fatalf("alpha: %+v", ra)
	}
	rb := resolve.Resolve(filepath.Join(e.base, "beta"), e.cfg)
	if rb.Workspace != nil || rb.Untrusted == nil || rb.Untrusted.Hash != betaHash || !rb.Untrusted.Protected {
		t.Fatalf("beta: %+v", rb)
	}
	// Journaled: one write-file per protected copy, one per root rewrite.
	if len(j.Entries) != 4 {
		t.Fatalf("entries = %+v", j.Entries)
	}
	for i, ent := range j.Entries {
		if ent.Op != OpWriteFile || ent.Step != "manifests" {
			t.Fatalf("entry %d: %+v", i, ent)
		}
		if i%2 == 0 && ent.Args["Backup"] != "" {
			t.Fatalf("the protected copy did not exist before: %+v", ent)
		}
		if i%2 == 1 && ent.Args["Backup"] == "" {
			t.Fatalf("the root file must be backed up: %+v", ent)
		}
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(paths.ManifestPath("alpha")); fi.Mode().Perm() != 0o600 {
			t.Fatalf("protected manifest mode %o, want 0600", fi.Mode().Perm())
		}
		if fi, _ := os.Stat(filepath.Dir(paths.ManifestPath("alpha"))); fi.Mode().Perm() != 0o700 {
			t.Fatalf("identities/alpha mode %o, want 0700 (it existed as 0755 before the move)", fi.Mode().Perm())
		}
	}
}

// A hash-only trust record of an older tq is rekeyed by workspace name, for
// a workspace in either layout, and carries over only for that name.
func TestManifests_RekeysLegacyTrustRecords(t *testing.T) {
	e := newMfEnv(t)
	alphaRoot, alphaHash := e.oldLayout("alpha", false)
	e.legacyTrust(alphaHash)
	gammaPP := e.protected("gamma") // keyed already by trust.Allow
	gammaHash, _ := trust.HashFile(gammaPP)
	deltaPP := e.protected("delta")
	deltaHash, _ := trust.HashFile(deltaPP)
	os.Remove(trust.RecordPath("delta", deltaHash))
	e.legacyTrust(deltaHash)
	if !trust.IsTrusted("alpha", alphaHash) || !trust.IsTrusted("evil", alphaHash) {
		t.Fatal("precondition: a legacy record counts for any name until rekeyed")
	}

	st := stepRegistry["manifests"]
	p, err := st.Plan(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	var rekeys []Change
	for _, c := range p.Changes {
		if c.Kind == kindRekeyTrust {
			rekeys = append(rekeys, c)
		}
	}
	if len(rekeys) != 2 || rekeys[0].Path != trust.LegacyRecordPath(alphaHash) || !strings.Contains(rekeys[0].Detail, trust.RecordPath("alpha", alphaHash)) ||
		rekeys[1].Path != trust.LegacyRecordPath(deltaHash) {
		t.Fatalf("rekeys = %+v (gamma is keyed already)", rekeys)
	}
	j := e.journal()
	if err := st.Apply(e.deps(), p, j); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, hash string }{{"alpha", alphaHash}, {"delta", deltaHash}, {"gamma", gammaHash}} {
		if !trust.HasRecord(c.name, c.hash) || trust.HasLegacyRecord(c.hash) {
			t.Fatalf("%s: keyed record wanted, legacy record gone", c.name)
		}
		if trust.IsTrusted("evil", c.hash) {
			t.Fatalf("%s's bytes must not be trusted under another name", c.name)
		}
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.Dir(trust.RecordPath("alpha", alphaHash))); fi.Mode().Perm() != 0o700 {
			t.Fatalf("trust/alpha mode %o", fi.Mode().Perm())
		}
	}
	// alpha moved too and is still trusted, from the keyed record.
	if r := resolve.Resolve(filepath.Join(e.base, "alpha"), e.cfg); r.Workspace == nil || !r.Workspace.Protected {
		t.Fatalf("%+v", r)
	}
	_ = alphaRoot

	// Restore: the legacy records come back, the keyed ones go.
	if lines, err := j.Restore(Runner{}); err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(lines, "\n"))
	}
	for _, c := range []struct{ name, hash string }{{"alpha", alphaHash}, {"delta", deltaHash}} {
		if trust.HasRecord(c.name, c.hash) || !trust.HasLegacyRecord(c.hash) {
			t.Fatalf("%s: restore must put the legacy record back", c.name)
		}
	}
	if !trust.HasRecord("gamma", gammaHash) {
		t.Fatal("gamma's keyed record was never touched")
	}
}

// A symlink at a root file is never read or written: the plan leaves the
// workspace out with a warning, and a symlink swapped in after the plan
// makes the apply refuse.
func TestManifests_RefusesASymlinkedRootFile(t *testing.T) {
	e := newMfEnv(t)
	alphaRoot, _ := e.oldLayout("alpha", true)
	pp := e.protected("gamma")
	evil := filepath.Join(e.base, "evil")
	stMkdir(t, evil)
	if err := os.Symlink(pp, filepath.Join(evil, manifest.FileName)); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("this process cannot create a symlink (developer mode or SeCreateSymbolicLinkPrivilege needed): %v", err)
		}
		t.Fatal(err)
	}
	st := stepRegistry["manifests"]
	p, err := st.Plan(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Changes {
		if strings.Contains(c.Path, evil) {
			t.Fatalf("a symlinked root file must not be planned: %+v", c)
		}
	}
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w, evil) && strings.Contains(w, "not a regular file") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the plan must say why evil was left out: %v", p.Warnings)
	}
	// Swap alpha's root file for a symlink between the plan and the apply.
	alphaBody := stRead(t, alphaRoot)
	os.Remove(alphaRoot)
	if err := os.Symlink(pp, alphaRoot); err != nil {
		t.Fatal(err)
	}
	err = st.Apply(e.deps(), p, e.journal())
	if err == nil || !strings.Contains(err.Error(), "machine changed") {
		t.Fatalf("apply must refuse: %v", err)
	}
	if _, err := os.Lstat(paths.ManifestPath("alpha")); err == nil {
		t.Fatal("nothing may be written")
	}
	if stRead(t, pp) != stRead(t, pp) || !strings.Contains(stRead(t, pp), "gamma@x.io") {
		t.Fatal("the symlink target must be untouched")
	}
	_ = alphaBody
	// And applyManifest itself refuses a non-regular root file.
	if err := applyManifest(mfAction{ws: "alpha", rootFile: alphaRoot, protected: paths.ManifestPath("alpha"), copy: true}, e.journal()); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("applyManifest must refuse a symlink: %v", err)
	}
}

func TestManifests_RestorePutsTheManifestBack(t *testing.T) {
	e := newMfEnv(t)
	alphaRoot, alphaHash := e.oldLayout("alpha", true)
	before := stRead(t, alphaRoot)
	st := stepRegistry["manifests"]
	p, err := st.Plan(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	j := e.journal()
	if err := st.Apply(e.deps(), p, j); err != nil {
		t.Fatal(err)
	}
	lines, err := j.Restore(Runner{})
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, strings.Join(lines, "\n"))
	}
	if got := stRead(t, alphaRoot); got != before {
		t.Fatalf("root manifest not restored:\n%s", got)
	}
	if _, err := os.Lstat(paths.ManifestPath("alpha")); err == nil {
		t.Fatal("the protected copy must be removed by the restore")
	}
	r := resolve.Resolve(filepath.Join(e.base, "alpha"), e.cfg)
	if r.Workspace == nil || r.Workspace.Protected || r.Workspace.Hash != alphaHash {
		t.Fatalf("after restore alpha must be the trusted old layout: %+v", r)
	}
}

func TestManifests_StaleRootOnlyGetsTheMarker(t *testing.T) {
	e := newMfEnv(t)
	pp := e.protected("acme")
	protectedBody := stRead(t, pp)
	// A full manifest left in the root next to the protected copy.
	rootFile := filepath.Join(e.base, "acme", manifest.FileName)
	stWrite(t, rootFile, "schema: tentaqles-client-v2\nclient: acme\ngit:\n  email: stale@x.io\n")
	staleBody := stRead(t, rootFile)

	st := stepRegistry["manifests"]
	p, err := st.Plan(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 1 || p.Changes[0].Kind != kindWriteMarker || p.Changes[0].Path != rootFile {
		t.Fatalf("%+v", p.Changes)
	}
	j := e.journal()
	if err := st.Apply(e.deps(), p, j); err != nil {
		t.Fatal(err)
	}
	if stRead(t, rootFile) != string(manifest.MarkerBytes("acme")) || stRead(t, pp) != protectedBody {
		t.Fatal("only the root file changes")
	}
	if _, err := j.Restore(Runner{}); err != nil {
		t.Fatal(err)
	}
	if stRead(t, rootFile) != staleBody || stRead(t, pp) != protectedBody {
		t.Fatal("restore must put the stale root back and leave the protected copy")
	}
}

func TestManifests_ApplyRefusesAChangedMachine(t *testing.T) {
	e := newMfEnv(t)
	e.oldLayout("alpha", true)
	st := stepRegistry["manifests"]
	p, err := st.Plan(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	e.oldLayout("beta", true)
	err = st.Apply(e.deps(), p, e.journal())
	if err == nil || !strings.Contains(err.Error(), "machine changed") {
		t.Fatalf("expected the changed-machine refusal, got %v", err)
	}
	if _, err := os.Lstat(paths.ManifestPath("alpha")); err == nil {
		t.Fatal("nothing may be written when the plan no longer matches")
	}
}

func TestManifests_RunIsDryByDefault(t *testing.T) {
	e := newMfEnv(t)
	alphaRoot, _ := e.oldLayout("alpha", true)
	before := stRead(t, alphaRoot)
	steps, _ := Steps([]string{"manifests"})
	plans, err := Run(e.deps(), steps, false, nil)
	if err != nil || len(plans["manifests"].Changes) != 2 {
		t.Fatalf("%v %+v", err, plans)
	}
	if stRead(t, alphaRoot) != before {
		t.Fatal("a dry run changed the root file")
	}
	if _, err := os.Lstat(paths.ManifestPath("alpha")); err == nil {
		t.Fatal("a dry run wrote the protected copy")
	}
}
