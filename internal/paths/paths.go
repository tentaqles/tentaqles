// Package paths resolves tq's on-disk state locations.
package paths

import (
	"os"
	"path/filepath"
)

// Home is ~/.tentaqles or $TQ_HOME.
func Home() string {
	if h := os.Getenv("TQ_HOME"); h != "" {
		return h
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return ".tentaqles"
	}
	return filepath.Join(u, ".tentaqles")
}

func Config() string                    { return filepath.Join(Home(), "config.yaml") }
func TrustDir() string                  { return filepath.Join(Home(), "trust") }
func Audit() string                     { return filepath.Join(Home(), "audit.jsonl") }
func IdentitiesRoot() string            { return filepath.Join(Home(), "identities") }
func IdentityDir(ws, cli string) string { return filepath.Join(IdentitiesRoot(), ws, cli) }

func BundlesDir() string { return filepath.Join(Home(), "bundles") }
func Catalog() string    { return filepath.Join(BundlesDir(), "catalog.yaml") }
