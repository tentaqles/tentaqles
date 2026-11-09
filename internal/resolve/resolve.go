// Package resolve maps a directory to exactly one client workspace, or neutral.
package resolve

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
)

// Workspace is one client: its folder (Root) under a clients folder (Base), and its manifest,
// read from ManifestPath (paths.ManifestPath(Name)).
type Workspace struct {
	Name, Root, Base, ManifestPath string
	Manifest                       *manifest.Manifest
}

// Result is what a folder resolves to: a Workspace, or none with a Reason.
type Result struct {
	Workspace *Workspace
	Reason    string
	// Name is the client folder cwd is in under a base, "" outside every base and at a base
	// root; set whatever the outcome.
	Name string
	// Unreadable is set (with Workspace nil) when the folder carries tq's marker but tq cannot
	// read a usable manifest for it.
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

// Resolve maps cwd to the client folder it is in, or neutral with a Reason.
func Resolve(cwd string, cfg *registry.Config) Result {
	nb, root, name, reason := folder(cwd, cfg)
	if name == "" {
		return Result{Reason: reason}
	}
	r := load(cfg, nb, root, name)
	r.Name = name
	return r
}

// folder is the client folder cwd is in under a base: nb is the base (normalized), root the
// folder and name its name. name is "" when cwd is in no folder, with reason saying why.
func folder(cwd string, cfg *registry.Config) (nb, root, name, reason string) {
	c := norm(cwd)
	// The case-preserving form of cwd recovers the on-disk casing of the name; the comparison
	// uses the normalized form.
	cOrig, err := registry.Normalize(cwd)
	if err != nil {
		cOrig = filepath.Clean(cwd)
	}
	for _, base := range cfg.Bases {
		b0, err := registry.Normalize(base)
		if err != nil {
			b0 = filepath.Clean(base)
		}
		b := b0
		if runtime.GOOS == "windows" {
			b = strings.ToLower(b)
		}
		if c == b {
			return "", "", "", "at base root"
		}
		if !strings.HasPrefix(c, b+string(filepath.Separator)) || len(cOrig) < len(b0) {
			continue
		}
		rel := strings.TrimPrefix(cOrig[len(b0):], string(filepath.Separator))
		n := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		return b0, filepath.Join(b0, n), n, ""
	}
	return "", "", "", "outside any base"
}

// sameName compares a marker's client with the folder name the way the filesystem compares
// names: case-insensitively on Windows.
func sameName(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// locate checks the marker at <root>/.tentaqles.yaml: it must name the folder, and the manifest
// paths.ManifestPath(name) must exist. A folder with no marker is neutral, not unreadable.
func locate(root, name string) (path string, unreadable bool, reason string) {
	rf := filepath.Join(root, manifest.FileName)
	fi, err := os.Lstat(rf)
	if err != nil {
		return "", false, "no marker"
	}
	if !fi.Mode().IsRegular() {
		return "", true, "marker is not a regular file"
	}
	kind, client, err := manifest.Peek(rf)
	if err != nil || kind != manifest.KindMarker {
		return "", true, fmt.Sprintf("%s is not a tentaqles marker", rf)
	}
	if !sameName(client, name) {
		return "", false, fmt.Sprintf("marker names client %q but the folder is %q", client, name)
	}
	pp := paths.ManifestPath(name)
	pfi, err := os.Lstat(pp)
	if err != nil || !pfi.Mode().IsRegular() {
		return "", true, fmt.Sprintf("marker without a manifest (expected %s)", pp)
	}
	return pp, false, ""
}

func load(cfg *registry.Config, base, root, name string) Result {
	p, unreadable, reason := locate(root, name)
	if reason != "" {
		return Result{Reason: reason, Unreadable: unreadable}
	}
	m, err := manifest.Load(p)
	if err != nil {
		return Result{Reason: fmt.Sprintf("manifest invalid: %v", err), Unreadable: true}
	}
	if !sameName(m.Client, name) {
		return Result{Reason: fmt.Sprintf("manifest names client %q but the folder is %q", m.Client, name)}
	}
	if reason := bound(cfg, root, m.Client); reason != "" {
		return Result{Reason: reason, Unreadable: true}
	}
	return Result{Workspace: &Workspace{Name: name, Root: root, Base: base, ManifestPath: p, Manifest: m}}
}

// bound checks root against the folder the registry records for client. A recorded client is
// that folder and no other: a copy of it elsewhere is refused. A client with no record was
// created before the record existed; it resolves as before unless a second folder under a
// registered base claims the same name, and then both are refused until tentaqles bind picks
// one. It returns "" when root is the client's folder, or the reason it is not.
func bound(cfg *registry.Config, root, client string) string {
	if rec, ok := cfg.Folder(client); ok {
		if norm(rec) == norm(root) {
			return ""
		}
		return fmt.Sprintf("client %q lives in %s; this folder is a copy of it (remove its %s, or run tentaqles bind here if the client moved)", client, rec, manifest.FileName)
	}
	for _, b := range cfg.Bases {
		nb, err := registry.Normalize(b)
		if err != nil {
			nb = filepath.Clean(b)
		}
		other := filepath.Join(nb, client)
		if norm(other) == norm(root) {
			continue
		}
		kind, c, err := manifest.Peek(filepath.Join(other, manifest.FileName))
		if err == nil && kind == manifest.KindMarker && sameName(c, client) {
			return fmt.Sprintf("two folders claim client %q (%s and %s); run tentaqles bind in the one that is the client", client, root, other)
		}
	}
	return ""
}

// ListWorkspaces returns every first-level child of every base that is a client, sorted by name.
// A folder with a marker that resolves to nothing is returned as an error.
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
			r := load(cfg, nb, root, e.Name())
			if r.Workspace == nil {
				errs = append(errs, fmt.Errorf("%s: %s", root, r.Reason))
				continue
			}
			out = append(out, *r.Workspace)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, errs
}

// Find is the client named name, or an error naming why not.
func Find(cfg *registry.Config, name string) (*Workspace, error) {
	all, errs := ListWorkspaces(cfg)
	for _, w := range all {
		if w.Name == name {
			return &w, nil
		}
	}
	return nil, fmt.Errorf("no client %q (errors: %v)", name, errs)
}

// Claim is the client folder cwd is in whose marker names it and whose manifest loads, whatever
// folder the registry records for that client. tentaqles bind uses it to bind a client that
// moved or has no record. It returns the client's name and the folder.
func Claim(cwd string, cfg *registry.Config) (name, root string, err error) {
	_, root, name, reason := folder(cwd, cfg)
	if name == "" {
		return "", "", errors.New(reason)
	}
	p, _, reason := locate(root, name)
	if reason != "" {
		return "", "", errors.New(reason)
	}
	m, err := manifest.Load(p)
	if err != nil {
		return "", "", fmt.Errorf("manifest invalid: %v", err)
	}
	if !sameName(m.Client, name) {
		return "", "", fmt.Errorf("manifest names client %q but the folder is %q", m.Client, name)
	}
	return m.Client, root, nil
}
