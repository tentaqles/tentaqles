// Package paths resolves tq's on-disk state locations.
package paths

import (
	"os"
	"path/filepath"
)

// Home is $TQ_HOME, or ~/.tentaqles when TQ_HOME is unset.
func Home() string {
	if h := os.Getenv("TQ_HOME"); h != "" {
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

func Config() string                    { return filepath.Join(Home(), "config.yaml") }
func IdentitiesRoot() string            { return filepath.Join(Home(), "identities") }
func IdentityDir(ws, cli string) string { return filepath.Join(IdentitiesRoot(), ws, cli) }

// ShimsRoot is $TQ_HOME/shims: the programs tq puts on an identity's PATH, one folder per
// identity under it.
func ShimsRoot() string { return filepath.Join(Home(), "shims") }

// ShimsDir is identity ws's folder of shims: $TQ_HOME/shims/<ws>.
func ShimsDir(ws string) string { return filepath.Join(ShimsRoot(), ws) }

// ManifestPath is the manifest of workspace ws, ~/.tentaqles/identities/<ws>/manifest.yaml. The
// client's folder keeps only a name-only marker at <root>/.tentaqles.yaml.
func ManifestPath(ws string) string { return filepath.Join(IdentitiesRoot(), ws, "manifest.yaml") }

// LobeDir is the folder of client ws's memory, $TQ_HOME/identities/<ws>/lobe. It is the path
// IdentityDir(ws, "lobe") would give, which is why no provider may have the id "lobe".
func LobeDir(ws string) string { return filepath.Join(IdentitiesRoot(), ws, "lobe") }
