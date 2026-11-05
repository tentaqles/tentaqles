package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func init() { register(manifestsStep{}) }

// Change.Kind values the manifests step emits; see stepChangeKinds.
const (
	kindWriteProtectedManifest = "write-protected-manifest"
	kindWriteMarker            = "write-marker"
)

// manifestsStep moves each old-layout manifest out of its identity root into
// the protected location (ADR-0002): the file at <root>/.tentaqles.yaml is
// copied byte for byte to $TQ_HOME/identities/<ws>/manifest.yaml, then the
// root file is rewritten as the name-only marker. A full manifest left in
// the root next to a protected copy (StaleRoot) only gets the marker rewrite.
//
// Trust is the hash of the manifest's bytes, and the bytes do not change, so
// a trusted manifest stays trusted after the move and an untrusted one stays
// untrusted; nothing is re-recorded. Both writes are journaled before they
// happen, so `tq uninstall --restore` puts the manifest back in the root and
// removes the protected copy.
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
}

func (manifestsStep) Plan(d Deps) (Plan, error) {
	p, _, err := planManifests(d)
	return p, err
}

func (manifestsStep) Apply(d Deps, p Plan, j *Journal) error {
	// Re-derive rather than trust the plan's strings, like the other steps.
	fresh, acts, err := planManifests(d)
	if err != nil {
		return err
	}
	if err := sameChanges("manifests", p, fresh); err != nil {
		return err
	}
	if j == nil {
		return fmt.Errorf("manifests: no journal")
	}
	for _, a := range acts {
		if err := applyManifest(a, j); err != nil {
			return fmt.Errorf("manifests %s: %w", a.ws, err)
		}
	}
	return nil
}

func planManifests(d Deps) (Plan, []mfAction, error) {
	var p Plan
	if d.Cfg == nil {
		return p, nil, fmt.Errorf("manifests: no registry config")
	}
	all, errs := resolve.ListWorkspaces(d.Cfg)
	for _, e := range errs {
		p.Warnings = append(p.Warnings, "workspace scan: "+e.Error())
	}
	var acts []mfAction
	for _, w := range all {
		pp := paths.ManifestPath(w.Name)
		switch {
		case w.StaleRoot != "":
			acts = append(acts, mfAction{ws: w.Name, rootFile: w.StaleRoot, protected: pp})
		case !w.Protected:
			acts = append(acts, mfAction{ws: w.Name, rootFile: w.ManifestPath, protected: pp, hash: w.Hash, trusted: trust.IsTrusted(w.Hash), copy: true})
		default:
			p.Skipped = append(p.Skipped, fmt.Sprintf("workspace %s: manifest already protected (%s)", w.Name, w.ManifestPath))
		}
	}
	for _, a := range acts {
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
	return p, acts, nil
}

// applyManifest performs one workspace's move: record, write the protected
// copy; back up, record, rewrite the root file as the marker. The journal
// reverses them newest first, so a restore puts the root file back before
// it deletes the protected copy.
func applyManifest(a mfAction, j *Journal) error {
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
		// 0700 / 0600: the protected area is private to the user.
		if err := os.MkdirAll(filepath.Dir(a.protected), 0o700); err != nil {
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
