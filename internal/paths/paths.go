// Package paths resolves tq's on-disk state locations.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/markers"
)

// Under an agent marker (internal/markers, the one list the human-only
// detector reads too) TQ_HOME is ignored: the hook inherits the harness
// environment, and a workspace's agent-writable Claude settings can set
// TQ_HOME to a forged state folder with its own trust records.

// AllowHomeOverride lets TQ_HOME apply under an agent marker. The tests set
// it (they run under Claude Code with TQ_HOME pointing at a temp dir);
// nothing else does.
var AllowHomeOverride = false

func agentSession() bool {
	_, ok := markers.Set()
	return ok
}

// Home is $TQ_HOME, or ~/.tentaqles when TQ_HOME is unset or the process
// runs under an agent marker (markers.Markers).
func Home() string {
	if h := os.Getenv("TQ_HOME"); h != "" && (AllowHomeOverride || !agentSession()) {
		return h
	}
	return DefaultHome()
}

// DefaultHome is ~/.tentaqles whatever TQ_HOME says.
func DefaultHome() string {
	u, err := os.UserHomeDir()
	if err != nil {
		return ".tentaqles"
	}
	return filepath.Join(u, ".tentaqles")
}

// HomeOverride returns TQ_HOME when it is set to something other than
// DefaultHome, for tq doctor; "" otherwise.
func HomeOverride() string {
	h := strings.TrimSpace(os.Getenv("TQ_HOME"))
	if h == "" {
		return ""
	}
	a, b := filepath.Clean(h), filepath.Clean(DefaultHome())
	if runtime.GOOS == "windows" {
		if strings.EqualFold(a, b) {
			return ""
		}
	} else if a == b {
		return ""
	}
	return h
}

func Config() string                    { return filepath.Join(Home(), "config.yaml") }
func TrustDir() string                  { return filepath.Join(Home(), "trust") }
func Audit() string                     { return filepath.Join(Home(), "audit.jsonl") }
func IdentitiesRoot() string            { return filepath.Join(Home(), "identities") }
func IdentityDir(ws, cli string) string { return filepath.Join(IdentitiesRoot(), ws, cli) }

// ShimsRoot is $TQ_HOME/shims: the programs tq puts on an identity's PATH (ADR-0016), one
// folder per identity under it, every file of the identity's folder covered by its integrity
// record.
func ShimsRoot() string { return filepath.Join(Home(), "shims") }

// ShimsDir is identity ws's folder of shims: $TQ_HOME/shims/<ws>.
func ShimsDir(ws string) string { return filepath.Join(ShimsRoot(), ws) }

// BrowserDir is the folder under identity ws that holds the user data of the browser tq starts
// for it where it cannot use the browser's own profile (Playwright, ADR-0016):
// $TQ_HOME/identities/<ws>/browser.
func BrowserDir(ws string) string { return filepath.Join(IdentitiesRoot(), ws, "browser") }

// ManifestPath is the protected manifest of workspace ws,
// ~/.tentaqles/identities/<ws>/manifest.yaml (ADR-0002). The identity root
// keeps only a name-only marker at <root>/.tentaqles.yaml.
func ManifestPath(ws string) string { return filepath.Join(IdentitiesRoot(), ws, "manifest.yaml") }

// ACLRecord is where tq records the one icacls run that restricts Home() to
// the current user on Windows (internal/protect); tq doctor reads it.
func ACLRecord() string { return filepath.Join(Home(), "protected-acl.json") }

func BundlesDir() string { return filepath.Join(Home(), "bundles") }
func Catalog() string    { return filepath.Join(BundlesDir(), "catalog.yaml") }
