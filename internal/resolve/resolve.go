// Package resolve maps a directory to exactly one trusted workspace, or neutral.
package resolve

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/trust"
)

type Workspace struct {
	Name, Root, Base, ManifestPath, Hash string
	Manifest                             *manifest.Manifest
	// Protected reports that ManifestPath is the protected copy under
	// paths.ManifestPath(Name), with a name-only marker at
	// <Root>/.tentaqles.yaml (ADR-0002). False is the old layout: the full
	// manifest at the root, read as before until `tq migrate manifests`.
	Protected bool
	// StaleRoot names a full manifest still sitting at <Root>/.tentaqles.yaml
	// while the protected copy exists and is the one read; "" otherwise.
	// Nothing reads it; tq doctor reports it.
	StaleRoot string
}

type Result struct {
	Workspace *Workspace
	Reason    string
	// Untrusted is set (with Workspace still nil) when cwd maps to a workspace
	// whose manifest loads and hashes fine but is not trusted. It exists so
	// callers can name the workspace without parsing Reason; every fail-closed
	// caller that only looks at Workspace is unaffected.
	Untrusted *Workspace
	// Mismatch is set with Untrusted when the workspace's signed integrity
	// record exists but does not verify or match (a covered file changed):
	// the fix is `tq hooks verify`, not `tq allow`, and Reason names the
	// first changed file.
	Mismatch bool
	// Legacy is set with Workspace when it is trusted through a marker of
	// an older tq, which covers the manifest's bytes but no hook file:
	// `tq allow` signs a record that does.
	Legacy bool
	// Name is the workspace folder cwd maps to under a base, "" outside
	// every base and at a base root; set whatever the outcome.
	Name string
	// Unreadable is set (with Workspace and Untrusted nil) when the folder
	// carries tq's marker or manifest but tq cannot read a usable manifest
	// for it: a marker without its protected copy, a file that is not a
	// regular file, a manifest that does not parse or hash. The guard still
	// treats it as neutral; the hook denies every call there (PRD v2 §6.3,
	// fail closed on a missing manifest). A folder with no marker, a marker
	// that names another workspace and a manifest whose client is another
	// folder are not unreadable: they are neutral.
	Unreadable bool
}

func norm(p string) string {
	n, err := registry.Normalize(p)
	if err != nil {
		n = filepath.Clean(p)
	}
	if runtime.GOOS == "windows" {
		n = strings.ToLower(n)
	}
	return n
}

// Resolve is fail-closed: any doubt returns a nil Workspace with a Reason.
//
// A neutral result is Unreadable too when tq's own evidence says the folder
// is an identity whose manifest is gone (ADR-0006): a folder under a base
// with no marker or manifest left while identity <name> still has a signed
// record or a protected manifest, and a folder outside every base (a
// config.yaml that is missing or lost the base) that a signed record names
// as its root.
func Resolve(cwd string, cfg *registry.Config) Result {
	r := resolveCwd(cwd, cfg)
	if r.Workspace != nil || r.Untrusted != nil || r.Unreadable {
		return r
	}
	if r.Name != "" {
		if r.Reason == "no manifest" && (trust.HasSignedRecord(r.Name) || regularFile(paths.ManifestPath(r.Name))) {
			r.Unreadable = true
			r.Reason = fmt.Sprintf("the folder has no marker or manifest, but identity %s has a signed record or a protected manifest", r.Name)
		}
		return r
	}
	key := trust.Key(cwd)
	for root, name := range trust.SignedRoots() {
		if key == root || strings.HasPrefix(key, root+"/") {
			return Result{Name: name, Unreadable: true, Reason: fmt.Sprintf(
				"the folder is under no registered base (%s), but identity %s's signed record names it as its root", r.Reason, name)}
		}
	}
	return r
}

func regularFile(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode().IsRegular()
}

func resolveCwd(cwd string, cfg *registry.Config) Result {
	c := norm(cwd)
	// Case-preserving (but symlink-resolved) form of cwd, used only to recover the
	// on-disk-cased workspace name/root — never for the containment comparison.
	cOrig, err := registry.Normalize(cwd)
	if err != nil {
		cOrig = filepath.Clean(cwd)
	}
	for _, base := range cfg.Bases {
		// Normalize the base exactly once. cfg.Bases normally holds normalized
		// paths (AddBase normalizes), but a hand-edited config.yaml can hold a
		// raw or trailing-separator path, and the raw length must never be used
		// to slice cOrig.
		nb, err := registry.Normalize(base)
		if err != nil {
			nb = filepath.Clean(base)
		}
		b := nb
		if runtime.GOOS == "windows" {
			b = strings.ToLower(b)
		}
		if c == b {
			return Result{Reason: "at base root"}
		}
		if !strings.HasPrefix(c, b+string(filepath.Separator)) {
			continue
		}
		// c and cOrig differ only by case, so they have the same length; slice
		// cOrig by the normalized base length to recover on-disk casing.
		if len(cOrig) < len(nb) {
			continue
		}
		rel := strings.TrimPrefix(cOrig[len(nb):], string(filepath.Separator))
		name := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		root := filepath.Join(nb, name)
		r := load(nb, root, name)
		r.Name = name
		return r
	}
	return Result{Reason: "outside any base"}
}

// sameName compares a marker's client with the folder name the way the
// filesystem compares names: case-insensitively on Windows.
func sameName(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// location is where a workspace's manifest is read from.
type location struct {
	// path is the file to Load: the protected copy or the old root file.
	path      string
	protected bool
	// staleRoot is a full manifest left at the root next to a protected copy.
	staleRoot string
	// unreadable is set with a reason when the root carries tq's file but
	// no usable manifest can be read (Result.Unreadable).
	unreadable bool
}

// locate decides which file is the manifest of the workspace at root, from
// what sits at <root>/.tentaqles.yaml (ADR-0002):
//
//   - a marker (tentaqles-marker-v1): its client must equal the folder name,
//     and the protected copy paths.ManifestPath(name) must exist; the
//     protected copy is read. Anything else resolves neutral with the reason;
//   - a full manifest (tentaqles-client-v1|v2): the old layout, read from the
//     root, unless a protected copy exists too, in which case the protected
//     one wins and the root file is reported as stale;
//   - anything else is handed to manifest.Load for its exact error.
//
// The absent root file is "no manifest", as before: an unmarked folder under
// a base is neutral.
func locate(root, name string) (location, string) {
	rf := filepath.Join(root, manifest.FileName)
	// Lstat: a symlink at the root file would read (and let the migration
	// write through to) a file elsewhere; only a regular file counts.
	fi, err := os.Lstat(rf)
	if err != nil {
		return location{}, "no manifest"
	}
	if !fi.Mode().IsRegular() {
		return location{unreadable: true}, "marker is not a regular file"
	}
	kind, client, err := manifest.Peek(rf)
	if err != nil {
		return location{unreadable: true}, fmt.Sprintf("manifest invalid: %v", err)
	}
	pp := paths.ManifestPath(name)
	pfi, perr := os.Lstat(pp)
	switch kind {
	case manifest.KindMarker:
		if !sameName(client, name) {
			return location{}, fmt.Sprintf("marker names workspace %q but the folder is %q", client, name)
		}
		if perr != nil {
			return location{unreadable: true}, fmt.Sprintf("marker without a protected manifest (expected %s)", pp)
		}
		if !pfi.Mode().IsRegular() {
			return location{unreadable: true}, "protected manifest is not a regular file"
		}
		return location{path: pp, protected: true}, ""
	case manifest.KindManifest:
		if perr == nil {
			if !pfi.Mode().IsRegular() {
				return location{unreadable: true}, "protected manifest is not a regular file"
			}
			return location{path: pp, protected: true, staleRoot: rf}, ""
		}
	}
	return location{path: rf}, ""
}

// clientMismatch is the reason a manifest whose client is not the folder
// name resolves neutral: a manifest copied from another workspace (a replay
// of its trusted bytes) must not lend that workspace's identity to the folder.
func clientMismatch(m *manifest.Manifest, name string) string {
	if sameName(m.Client, name) {
		return ""
	}
	return fmt.Sprintf("manifest names client %q but the folder is %q", m.Client, name)
}

// ManifestFile is the file tq reads as the manifest of the workspace at
// root (the protected copy or the old root file), with protected set for
// the former; reason is non-empty, and path empty, when root has no usable
// manifest. Callers that only need the file, not the workspace, use it
// instead of spelling the layout themselves.
func ManifestFile(root string) (path string, protected bool, reason string) {
	loc, reason := locate(root, filepath.Base(root))
	if reason != "" {
		return "", false, reason
	}
	return loc.path, loc.protected, ""
}

func load(base, root, name string) Result {
	loc, reason := locate(root, name)
	if reason != "" {
		return Result{Reason: reason, Unreadable: loc.unreadable}
	}
	m, err := manifest.Load(loc.path)
	if err != nil {
		return Result{Reason: fmt.Sprintf("manifest invalid: %v", err), Unreadable: true}
	}
	if reason := clientMismatch(m, name); reason != "" {
		return Result{Reason: reason}
	}
	h, err := trust.HashFile(loc.path)
	if err != nil {
		return Result{Reason: fmt.Sprintf("manifest invalid: %v", err), Unreadable: true}
	}
	ws := &Workspace{Name: name, Root: root, Base: base, ManifestPath: loc.path, Hash: h, Manifest: m,
		Protected: loc.protected, StaleRoot: loc.staleRoot}
	// The signed integrity record decides (trust.Check): the manifest hash,
	// the hook files and the shims must be as a human signed them.
	v := trust.Check(name, h)
	if !v.OK {
		return Result{Reason: v.Reason, Untrusted: ws, Mismatch: v.Mismatch}
	}
	return Result{Workspace: ws, Legacy: v.Kind == trust.KindLegacy}
}

// ListWorkspaces returns every first-level child of every base that has a
// manifest (either layout), trusted or not, sorted by name. Invalid manifests
// and markers that resolve to nothing are returned as errors.
func ListWorkspaces(cfg *registry.Config) ([]Workspace, []error) {
	var out []Workspace
	var errs []error
	for _, base := range cfg.Bases {
		entries, err := os.ReadDir(base)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		nb, err := registry.Normalize(base)
		if err != nil {
			nb = filepath.Clean(base)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			root := filepath.Join(nb, e.Name())
			if _, err := os.Stat(filepath.Join(root, manifest.FileName)); err != nil {
				continue
			}
			loc, reason := locate(root, e.Name())
			if reason != "" {
				errs = append(errs, fmt.Errorf("%s: %s", root, reason))
				continue
			}
			m, err := manifest.Load(loc.path)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if reason := clientMismatch(m, e.Name()); reason != "" {
				errs = append(errs, fmt.Errorf("%s: %s", root, reason))
				continue
			}
			h, _ := trust.HashFile(loc.path)
			out = append(out, Workspace{Name: e.Name(), Root: root, Base: base, ManifestPath: loc.path, Hash: h, Manifest: m,
				Protected: loc.protected, StaleRoot: loc.staleRoot})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, errs
}
