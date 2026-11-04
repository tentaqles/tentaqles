// Package trust records which manifest contents the user has explicitly allowed.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	"github.com/tentaqles/tentaqles/internal/paths"
)

func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func file(hash string) string { return filepath.Join(paths.TrustDir(), hash) }

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// IsTrusted reports whether the manifest with this hash was allowed. The
// hash is of the file's bytes, so an edited manifest is untrusted until
// `tq allow` runs again.
func IsTrusted(hash string) bool { return exists(file(hash)) }

func touch(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte{}, 0o600)
}

// Allow records hash as trusted.
func Allow(hash string) error { return touch(file(hash)) }

// Deny removes hash's trust.
func Deny(hash string) error {
	if err := os.Remove(file(hash)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
