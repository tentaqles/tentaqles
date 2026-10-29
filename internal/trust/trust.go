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

func file(hash, suffix string) string { return filepath.Join(paths.TrustDir(), hash+suffix) }

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func IsTrusted(hash string) bool { return exists(file(hash, "")) }

// IsBypassAllowed reports whether hash has bypass trust: a legacy .bypass
// file or a mode grant (spec E: tq allow --bypass is an alias of tq allow
// --mode <ws> relaxed, so a grant allows what bypass trust allowed).
func IsBypassAllowed(hash string) bool {
	return exists(file(hash, ".bypass")) || exists(file(hash, GrantSuffix))
}

func touch(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte{}, 0o600)
}

func Allow(hash string) error       { return touch(file(hash, "")) }
func AllowBypass(hash string) error { return touch(file(hash, ".bypass")) }

// Deny removes hash's trust, its bypass trust and its mode grant.
func Deny(hash string) error {
	for _, s := range []string{"", ".bypass", GrantSuffix} {
		if err := os.Remove(file(hash, s)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
