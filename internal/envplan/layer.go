package envplan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

// The PATH and environment layer of an identity whose manifest sets guard: v2 (PRD v2 §6.1
// step 2, §6.2 rows PATH and Environment; ADR-0007). It is built from the same compiled scope
// the hook judges by (policy.ScopeFor): an out-of-scope provider's CLI does not resolve by name
// on the identity PATH, and its variables are unset in the identity environment. Without the
// flag nothing here runs.

// MaskDirName is the per-identity folder, under the identity's folder in the tq home, that
// holds one stub per out-of-scope executable name (ADR-0007): it goes first on the identity
// PATH when a PATH folder that tq cannot drop (one shared with other tools) holds an
// out-of-scope CLI. A stub prints "<name>: command not found" and exits 127.
const MaskDirName = "path-mask"

// MaskDir is identity ws's mask folder: ~/.tentaqles/identities/<ws>/path-mask.
func MaskDir(ws string) string { return filepath.Join(paths.IdentitiesRoot(), ws, MaskDirName) }

// Layer is one identity's compiled PATH and environment layer.
type Layer struct {
	Identity string
	scope    *policy.Scope
	exes     map[string]string // out-of-scope executable name -> provider id
	maskDir  string
	ready    *bool // memoized MaskReady
}

// LayerFor is ws's layer, or nil when the v1 behaviour applies: no workspace, or a manifest
// without guard: v2. ws is a trusted workspace (resolve hands an untrusted one out apart).
func LayerFor(ws *resolve.Workspace) *Layer {
	if ws == nil || ws.Manifest == nil || !ws.Manifest.GuardV2() {
		return nil
	}
	s := policy.ScopeFor(ws.Name, ws.Manifest.Scope(), ws.Manifest.BundleMCP(), ws.Manifest.MCPAllow())
	exes := s.OutOfScopeExecutables()
	for n := range exes {
		if providers.AmbiguousExecutable(n) {
			delete(exes, n) // never dropped nor masked: another program may own the name
		}
	}
	return &Layer{Identity: ws.Name, scope: s, exes: exes, maskDir: MaskDir(ws.Name)}
}

// IsMasked reports whether a file or command name is one the layer hides: an out-of-scope
// executable that is not an ambiguous word (a path, a Windows suffix and the case ignored).
func (l *Layer) IsMasked(name string) bool {
	_, ok := l.scope.OutOfScopeExecutable(name)
	return ok && !providers.AmbiguousExecutable(filepath.Base(name))
}

// Scope is the compiled scope the layer was built from.
func (l *Layer) Scope() *policy.Scope { return l.scope }

// MaskDir is the layer's mask folder.
func (l *Layer) MaskDir() string { return l.maskDir }

// Masked is the sorted list of executable names the layer hides.
func (l *Layer) Masked() []string {
	out := make([]string, 0, len(l.exes))
	for n := range l.exes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// UnsetNames returns the variables, out of names, the identity environment unsets: an
// out-of-scope provider's variables no in-scope provider claims and the identity does not set
// itself (desired).
func (l *Layer) UnsetNames(names []string, desired map[string]string) []string {
	var out []string
	for _, n := range l.scope.OutOfScopeEnv(names) {
		if _, set := desired[n]; set {
			continue
		}
		if _, set := desired[envKey(n)]; set {
			continue
		}
		out = append(out, n)
	}
	return out
}

// RemoveMask removes identity ws's mask folder (guard: v2 turned off, or nothing to mask). It
// reports whether there was one.
func RemoveMask(ws string) (bool, error) {
	if !workspace.NameRe.MatchString(ws) {
		return false, fmt.Errorf("invalid workspace name %q", ws)
	}
	dir := MaskDir(ws)
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return true, os.RemoveAll(dir)
}

// isMaskEntry reports whether an OS-spelled PATH entry is the layer's mask folder.
func (l *Layer) isMaskEntry(p string) bool {
	return p != "" && samePath(filepath.Clean(p), filepath.Clean(l.maskDir))
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
