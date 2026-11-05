package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/protect"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func init() { register(manifestsStep{}) }

// Change.Kind values the manifests step emits; see stepChangeKinds.
const (
	kindWriteProtectedManifest = "write-protected-manifest"
	kindWriteMarker            = "write-marker"
	kindRekeyTrust             = "rekey-trust"
	kindSignIntegrity          = "sign-integrity"
)

// manifestsStep moves each old-layout manifest out of its identity root into
// the protected location (ADR-0002): the file at <root>/.tentaqles.yaml is
// copied byte for byte to $TQ_HOME/identities/<ws>/manifest.yaml, then the
// root file is rewritten as the name-only marker. A full manifest left in
// the root next to a protected copy (StaleRoot) only gets the marker rewrite.
//
// The manifest's bytes do not change, so neither does its hash: a trusted
// manifest stays trusted after the move and an untrusted one stays
// untrusted. A workspace with a signed integrity record (ADR-0004) gets it
// signed again after the move, since the marker and the protected copy are
// covered files (sign-integrity, the old record backed up). A workspace
// trusted through a marker of an older tq (trust/<hash> or
// trust/<name>/<hash>) gets a signed record instead (rekey-trust), in either
// layout. Every write is journaled before it happens, so `tq uninstall
// --restore` puts the manifest back in the root, removes the protected copy
// and restores the old trust record.
//
// The step is never in the default --steps: an identity changes layout only
// when the human asks (PRD v2 §13, "behind a flag").
type manifestsStep struct{}

func (manifestsStep) Name() string { return "manifests" }

// mfAction is one workspace to migrate.
type mfAction struct {
	ws, rootFile, protected, hash string
	trusted                       bool
	// copy says the root file is the manifest and must be copied to the
	// protected location first; false means the protected copy already
	// exists and only the root file is rewritten as the marker.
	copy bool
	// resign says the workspace has a signed integrity record, to sign
	// again once the files it covers have moved.
	resign bool
}

// rekeyAction is one workspace trusted through legacy markers (the paths
// in legacy) that gets a signed integrity record instead.
type rekeyAction struct {
	ws, hash string
	legacy   []string
}

// mfPlan is what planManifests derives: the plan and the typed actions
// behind it.
type mfPlan struct {
	plan   Plan
	acts   []mfAction
	rekeys []rekeyAction
}

func (manifestsStep) Plan(d Deps) (Plan, error) {
	mp, err := planManifests(d)
	return mp.plan, err
}

func (manifestsStep) Apply(d Deps, p Plan, j *Journal) error {
	// Re-derive rather than trust the plan's strings, like the other steps.
	fresh, err := planManifests(d)
	if err != nil {
		return err
	}
	if err := sameChanges("manifests", p, fresh.plan); err != nil {
		return err
	}
	if j == nil {
		return fmt.Errorf("manifests: no journal")
	}
	for _, a := range fresh.acts {
		if err := applyManifest(a, j); err != nil {
			return fmt.Errorf("manifests %s: %w", a.ws, err)
		}
	}
	// The records are signed after every move: they cover the marker and
	// the protected copy as they are now.
	for _, a := range fresh.acts {
		if !a.resign {
			continue
		}
		if err := applyResign(a, j); err != nil {
			return fmt.Errorf("manifests %s: %w", a.ws, err)
		}
	}
	for _, r := range fresh.rekeys {
		if err := applyRekey(r, j); err != nil {
			return fmt.Errorf("manifests %s: %w", r.ws, err)
		}
	}
	return nil
}

func planManifests(d Deps) (mfPlan, error) {
	var mp mfPlan
	p := &mp.plan
	if d.Cfg == nil {
		return mp, fmt.Errorf("manifests: no registry config")
	}
	all, errs := resolve.ListWorkspaces(d.Cfg)
	for _, e := range errs {
		p.Warnings = append(p.Warnings, "workspace scan: "+e.Error())
	}
	for _, w := range all {
		pp := paths.ManifestPath(w.Name)
		signed := trust.HasRecord(w.Name, w.Hash)
		switch {
		case w.StaleRoot != "":
			mp.acts = append(mp.acts, mfAction{ws: w.Name, rootFile: w.StaleRoot, protected: pp, hash: w.Hash, resign: signed})
		case !w.Protected:
			mp.acts = append(mp.acts, mfAction{ws: w.Name, rootFile: w.ManifestPath, protected: pp, hash: w.Hash, trusted: trust.IsTrusted(w.Name, w.Hash), copy: true, resign: signed})
		default:
			p.Skipped = append(p.Skipped, fmt.Sprintf("workspace %s: manifest already protected (%s)", w.Name, w.ManifestPath))
		}
		if !signed {
			var legacy []string
			if trust.HasLegacyRecord(w.Hash) {
				legacy = append(legacy, trust.LegacyRecordPath(w.Hash))
			}
			if trust.HasLegacyNameRecord(w.Name, w.Hash) {
				legacy = append(legacy, trust.RecordPath(w.Name, w.Hash))
			}
			if len(legacy) > 0 {
				mp.rekeys = append(mp.rekeys, rekeyAction{ws: w.Name, hash: w.Hash, legacy: legacy})
			}
		}
	}
	for _, a := range mp.acts {
		if a.copy {
			detail := "copy of " + a.rootFile + " (untrusted: stays untrusted until tq allow " + a.ws + ")"
			if a.trusted {
				detail = "copy of " + a.rootFile + " (trusted: same bytes, same hash, trust carries over)"
			}
			p.Changes = append(p.Changes, Change{Step: "manifests", Kind: kindWriteProtectedManifest, Path: a.protected, Detail: detail})
		}
		p.Changes = append(p.Changes, Change{Step: "manifests", Kind: kindWriteMarker, Path: a.rootFile,
			Detail: "replace the manifest with the name-only marker (client: " + a.ws + ")", Danger: true})
	}
	for _, a := range mp.acts {
		if a.resign {
			p.Changes = append(p.Changes, Change{Step: "manifests", Kind: kindSignIntegrity, Path: trust.SignedRecordPath(a.ws),
				Detail: "sign the integrity record again after the move (the marker and the protected copy are covered files)"})
		}
	}
	for _, r := range mp.rekeys {
		for _, legacy := range r.legacy {
			p.Changes = append(p.Changes, Change{Step: "manifests", Kind: kindRekeyTrust, Path: legacy,
				Detail: "replaced by the signed integrity record " + trust.SignedRecordPath(r.ws)})
		}
	}
	return mp, nil
}

// applyManifest performs one workspace's move: record, write the protected
// copy; back up, record, rewrite the root file as the marker. The journal
// reverses them newest first, so a restore puts the root file back before
// it deletes the protected copy.
func applyManifest(a mfAction, j *Journal) error {
	// Lstat: the plan only lists regular root files (resolve refuses a
	// symlink), and nothing may be written through one swapped in since.
	fi, err := os.Lstat(a.rootFile)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file; refusing to read or write through it", a.rootFile)
	}
	if a.copy {
		raw, err := os.ReadFile(a.rootFile)
		if err != nil {
			return err
		}
		if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != a.hash {
			return fmt.Errorf("%s changed since the plan was computed; re-run tq migrate", a.rootFile)
		}
		if _, err := os.Lstat(a.protected); err == nil {
			return fmt.Errorf("%s already exists; refusing to overwrite it", a.protected)
		}
		if err := j.RecordWriteFile("manifests", a.protected, Backup{}); err != nil {
			return err
		}
		// 0700 / 0600: the protected area is private to the user, the
		// identity folder included when an older tq created it wider.
		if err := protect.MkdirPrivate(filepath.Dir(a.protected)); err != nil {
			return err
		}
		if err := writeFileSync(a.protected, raw, 0o600); err != nil {
			return err
		}
	}
	b, err := j.BackupFile(a.rootFile)
	if err != nil {
		return err
	}
	if b.Rel == "" {
		return fmt.Errorf("%s vanished since the plan was computed; re-run tq migrate", a.rootFile)
	}
	if err := j.RecordWriteFile("manifests", a.rootFile, b); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(a.rootFile); err == nil {
		mode = fi.Mode().Perm()
	}
	return writeFileSync(a.rootFile, manifest.MarkerBytes(a.ws), mode)
}

// applyResign signs a workspace's integrity record again, after its files
// moved: the old record is backed up and recorded, then the new one is
// written. Reversed, a restore puts the old record back, which matches the
// restored layout.
func applyResign(a mfAction, j *Journal) error {
	signed := trust.SignedRecordPath(a.ws)
	b, err := j.BackupFile(signed)
	if err != nil {
		return err
	}
	if b.Rel == "" {
		return fmt.Errorf("%s vanished since the plan was computed; re-run tq migrate", signed)
	}
	raw, err := trust.SignRecord(a.ws, a.hash)
	if err != nil {
		return err
	}
	if err := j.RecordWriteFile("manifests", signed, b); err != nil {
		return err
	}
	return writeFileSync(signed, raw, 0o600)
}

// applyRekey turns legacy trust markers into a signed integrity record:
// record and write trust/<name>.json, then back up, record and delete each
// marker. Reversed newest first, a restore brings the markers back and
// removes the signed record.
func applyRekey(r rekeyAction, j *Journal) error {
	signed := trust.SignedRecordPath(r.ws)
	if _, err := os.Lstat(signed); err == nil {
		return fmt.Errorf("%s already exists; re-run tq migrate", signed)
	}
	raw, err := trust.SignRecord(r.ws, r.hash)
	if err != nil {
		return err
	}
	if err := j.RecordWriteFile("manifests", signed, Backup{}); err != nil {
		return err
	}
	if err := protect.MkdirPrivate(filepath.Dir(signed)); err != nil {
		return err
	}
	if err := writeFileSync(signed, raw, 0o600); err != nil {
		return err
	}
	for _, legacy := range r.legacy {
		b, err := j.BackupFile(legacy)
		if err != nil {
			return err
		}
		if b.Rel == "" {
			return fmt.Errorf("%s vanished since the plan was computed; re-run tq migrate", legacy)
		}
		if err := j.RecordDeleteFile("manifests", legacy, b); err != nil {
			return err
		}
		if err := os.Remove(legacy); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
