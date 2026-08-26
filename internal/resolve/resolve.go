// Package resolve maps a directory to exactly one trusted workspace, or neutral.
package resolve

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/trust"
)

type Workspace struct {
	Name, Root, Base, ManifestPath, Hash string
	Manifest                             *manifest.Manifest
}

type Result struct {
	Workspace *Workspace
	Reason    string
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
func Resolve(cwd string, cfg *registry.Config) Result {
	c := norm(cwd)
	// Case-preserving (but symlink-resolved) form of cwd, used only to recover the
	// on-disk-cased workspace name/root — never for the containment comparison.
	cOrig, err := registry.Normalize(cwd)
	if err != nil {
		cOrig = filepath.Clean(cwd)
	}
	for _, base := range cfg.Bases {
		b := norm(base)
		if c == b {
			return Result{Reason: "at base root"}
		}
		if !strings.HasPrefix(c, b+string(filepath.Separator)) {
			continue
		}
		// base is already case-preserving (registry.Normalize, not lowercased), and
		// cOrig has the same length as c modulo case, so slice by length rather than
		// TrimPrefix to avoid mixing lowercase/original casing.
		rel := strings.TrimPrefix(cOrig[len(base):], string(filepath.Separator))
		name := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		root := filepath.Join(base, name)
		return load(base, root, name)
	}
	return Result{Reason: "outside any base"}
}

func load(base, root, name string) Result {
	mp := filepath.Join(root, manifest.FileName)
	if _, err := os.Stat(mp); err != nil {
		return Result{Reason: "no manifest"}
	}
	m, err := manifest.Load(mp)
	if err != nil {
		return Result{Reason: fmt.Sprintf("manifest invalid: %v", err)}
	}
	h, err := trust.HashFile(mp)
	if err != nil {
		return Result{Reason: fmt.Sprintf("manifest invalid: %v", err)}
	}
	ws := &Workspace{Name: name, Root: root, Base: base, ManifestPath: mp, Hash: h, Manifest: m}
	if !trust.IsTrusted(h) {
		return Result{Reason: fmt.Sprintf("untrusted (run: tq allow %s)", name)}
	}
	return Result{Workspace: ws}
}

// ListWorkspaces returns every first-level child of every base that has a manifest,
// trusted or not, sorted by name. Invalid manifests are returned as errors.
func ListWorkspaces(cfg *registry.Config) ([]Workspace, []error) {
	var out []Workspace
	var errs []error
	for _, base := range cfg.Bases {
		entries, err := os.ReadDir(base)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			root := filepath.Join(base, e.Name())
			mp := filepath.Join(root, manifest.FileName)
			if _, err := os.Stat(mp); err != nil {
				continue
			}
			m, err := manifest.Load(mp)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			h, _ := trust.HashFile(mp)
			out = append(out, Workspace{Name: e.Name(), Root: root, Base: base, ManifestPath: mp, Hash: h, Manifest: m})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, errs
}
