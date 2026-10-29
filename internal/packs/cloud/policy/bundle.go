package policy

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The Rego bundle for Conftest (spec D §7.4), written fresh in Rego v1.
// The *_test.rego files are its `opa test` fixtures; they are not extracted.
//
//go:embed rego/*.rego
var bundleFS embed.FS

// Namespaces of the bundle, by the input they read.
const (
	NamespaceTerraform = "tq.terraform"
	NamespaceARM       = "tq.arm"
	NamespaceCFN       = "tq.cfn"
)

// BundleFiles returns the policy files of the bundle (no tests), by name.
func BundleFiles() map[string][]byte {
	out := map[string][]byte{}
	ents, _ := fs.ReadDir(bundleFS, "rego")
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".rego") || strings.HasSuffix(n, "_test.rego") {
			continue
		}
		b, err := bundleFS.ReadFile("rego/" + n)
		if err == nil {
			out[n] = b
		}
	}
	return out
}

// BundleHash identifies the bundle's content (it names the extraction
// directory, so a new tq never runs an older bundle).
func BundleHash() string {
	files := BundleFiles()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write([]byte{0})
		h.Write(files[n])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ExtractBundle writes the bundle to <stateDir>/policy/<hash>/ (0700
// directories, 0600 files) unless it is already there with the same bytes,
// and returns the directory.
func ExtractBundle(stateDir string) (string, error) {
	if stateDir == "" {
		return "", errors.New("no pack state directory (not in a workspace)")
	}
	dir := filepath.Join(stateDir, "policy", BundleHash())
	files := BundleFiles()
	if bundleIntact(dir, files) {
		return dir, nil
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(parent, ".extract-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	for n, b := range files {
		if err := os.WriteFile(filepath.Join(tmp, n), b, 0o600); err != nil {
			return "", err
		}
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		if bundleIntact(dir, files) { // another run won the race
			return dir, nil
		}
		return "", err
	}
	return dir, nil
}

func bundleIntact(dir string, files map[string][]byte) bool {
	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) != len(files) {
		return false
	}
	for n, b := range files {
		got, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil || string(got) != string(b) {
			return false
		}
	}
	return true
}
