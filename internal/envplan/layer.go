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
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/scope"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

// The PATH and environment layer of an identity, built from its compiled scope (scope.For): an
// out-of-scope provider's CLI does not resolve by name on the identity PATH, and its variables
// are unset in the identity environment.

// MaskDirName is the per-identity folder, under the identity's folder in the tq home, that
// holds one stub per out-of-scope executable name: it goes first on the identity
// PATH when a PATH folder that tq cannot drop (one shared with other tools) holds an
// out-of-scope CLI. A stub prints "<name>: command not found" and exits 127.
const MaskDirName = "path-mask"

// MaskDir is identity ws's mask folder: ~/.tentaqles/identities/<ws>/path-mask.
func MaskDir(ws string) string { return filepath.Join(paths.IdentitiesRoot(), ws, MaskDirName) }

// Layer is one identity's compiled PATH and environment layer.
type Layer struct {
	Identity string
	scope    *scope.Scope
	exes     map[string]string // out-of-scope executable name -> provider id
	maskDir  string
	shims    string // the identity's shims folder when it lists a browser, else ""
	ready    *bool  // memoized MaskReady
}

// LayerFor is ws's layer, or nil with no workspace. ws is a trusted workspace (resolve hands an untrusted one out apart).
func LayerFor(ws *resolve.Workspace) *Layer {
	if ws == nil || ws.Manifest == nil {
		return nil
	}
	s := scope.For(ws.Name, ws.Manifest.Scope())
	exes := s.OutOfScopeExecutables()
	for n := range exes {
		if providers.AmbiguousExecutable(n) {
			delete(exes, n) // never dropped nor masked: another program may own the name
		}
	}
	l := &Layer{Identity: ws.Name, scope: s, exes: exes, maskDir: MaskDir(ws.Name)}
	if HasBrowsers(ws) {
		l.shims = ShimsDir(ws.Name)
	}
	return l
}

// IsMasked reports whether a file or command name is one the layer hides: an out-of-scope
// executable that is not an ambiguous word (a path, a Windows suffix and the case ignored).
func (l *Layer) IsMasked(name string) bool {
	_, ok := l.scope.OutOfScopeExecutable(name)
	return ok && !providers.AmbiguousExecutable(filepath.Base(name))
}

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

// RemoveMask removes identity ws's mask folder (nothing to mask). It
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
