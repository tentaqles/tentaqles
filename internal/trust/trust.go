// Package trust records which manifest contents the user has explicitly
// allowed, per workspace. A record is keyed by the workspace name and the
// sha256 of the manifest's bytes (trust/<name>/<hash>), so the same bytes
// copied under another folder name are not trusted there (ADR-0002), the
// way direnv keys on path plus content. A record of an older tq
// (trust/<hash>, no name) still counts, for the name resolve has already
// checked against the manifest's own client, until `tq migrate manifests`
// rekeys it.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

// RecordPath is the record of hash for workspace name: trust/<name>/<hash>.
func RecordPath(name, hash string) string { return filepath.Join(paths.TrustDir(), name, hash) }

// LegacyRecordPath is the record an older tq wrote, trust/<hash>, keyed by
// the hash alone.
func LegacyRecordPath(hash string) string { return filepath.Join(paths.TrustDir(), hash) }

// validKey rejects a name or hash that could name a path outside the trust
// folder (a separator, "." or "..").
func validKey(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`) && filepath.Base(s) == s
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// IsTrusted reports whether the manifest with this hash was allowed for
// workspace name. The hash is of the file's bytes, so an edited manifest is
// untrusted until `tq allow` runs again, and the name is part of the key, so
// the same bytes are untrusted under another folder name. A legacy
// hash-only record counts for whatever name is asked: resolve only asks for
// the folder name the manifest's own client was checked against.
func IsTrusted(name, hash string) bool {
	if !validKey(name) || !validKey(hash) {
		return false
	}
	return exists(RecordPath(name, hash)) || exists(LegacyRecordPath(hash))
}

// HasRecord reports a name-keyed record for hash.
func HasRecord(name, hash string) bool {
	return validKey(name) && validKey(hash) && exists(RecordPath(name, hash))
}

// HasLegacyRecord reports a hash-only record, the key of an older tq.
func HasLegacyRecord(hash string) bool { return validKey(hash) && exists(LegacyRecordPath(hash)) }

func touch(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte{}, 0o600)
}

// Allow records hash as trusted for workspace name.
func Allow(name, hash string) error {
	if !validKey(name) || !validKey(hash) {
		return fmt.Errorf("trust: invalid workspace name %q or hash %q", name, hash)
	}
	return touch(RecordPath(name, hash))
}

// Deny removes name's record for hash, and the legacy record of that hash,
// so a denied manifest is not still trusted through the older key.
func Deny(name, hash string) error {
	if !validKey(name) || !validKey(hash) {
		return fmt.Errorf("trust: invalid workspace name %q or hash %q", name, hash)
	}
	for _, p := range []string{RecordPath(name, hash), LegacyRecordPath(hash)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
