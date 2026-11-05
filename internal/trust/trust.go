// Package trust records what the user has explicitly allowed, per
// workspace: a signed integrity record (trust/<name>.json) over the
// manifest and every file that enforces the identity: the root marker, the
// identity's Claude settings, the installed plugin's hooks and scripts, the
// Codex hook files, the shims and the running tq binary (PRD v2 §8.3,
// ADR-0004). A workspace is trusted when its record exists, its HMAC
// signature verifies under the key in the OS keychain, its manifest hash is
// the one resolve read, and every covered file still hashes as recorded.
// A human signs the record with `tq allow` and re-signs it with
// `tq hooks verify --accept`; nothing else writes it.
//
// Two older record forms still count for a workspace until `tq migrate
// manifests` or `tq allow` replaces them with a signed record: the
// name-keyed marker trust/<name>/<hash> of the previous tq, and the
// hash-only marker trust/<hash> of the one before (ADR-0002). Deny removes
// every form.
package trust

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// RecordPath is the name-keyed marker of the previous tq, trust/<name>/<hash>:
// read as a legacy record, never written.
func RecordPath(name, hash string) string { return filepath.Join(paths.TrustDir(), name, hash) }

// LegacyRecordPath is the hash-only marker of an older tq, trust/<hash>:
// read as a legacy record, never written.
func LegacyRecordPath(hash string) string { return filepath.Join(paths.TrustDir(), hash) }

// validKey rejects a name or hash that could name a path outside the trust
// folder (a separator, "." or "..").
func validKey(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`) && filepath.Base(s) == s
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// HasLegacyNameRecord reports the name-keyed marker trust/<name>/<hash>.
func HasLegacyNameRecord(name, hash string) bool {
	return validKey(name) && validKey(hash) && exists(RecordPath(name, hash))
}

// HasLegacyRecord reports the hash-only marker trust/<hash>.
func HasLegacyRecord(hash string) bool { return validKey(hash) && exists(LegacyRecordPath(hash)) }

// HasRecord reports a signed record for name that names hash as the
// manifest hash. It reads the record without verifying it: Verify does.
func HasRecord(name, hash string) bool {
	if !validKey(name) || !validKey(hash) {
		return false
	}
	rec, ok, err := readRecord(name)
	return ok && err == nil && rec.ManifestHash == hash
}

// Change is one covered path whose hash is not what the record holds.
type Change struct {
	// Path is the covered path; "<dir>/config.toml#tq" is the tq-owned
	// block of a Codex config.toml.
	Path string
	// Kind is "changed", "added" (absent when signed, present now) or
	// "removed" (present when signed, absent now).
	Kind string
	// Old and New are the recorded and the current hash; "" is absent.
	Old, New string
}

// String is "<kind> <path> (<old12> -> <new12>)".
func (c Change) String() string {
	return fmt.Sprintf("%s %s (%s -> %s)", c.Kind, c.Path, short(c.Old), short(c.New))
}

func short(h string) string {
	switch {
	case h == "":
		return "absent"
	case h == irregular:
		return "not a regular file"
	case len(h) > 12:
		return h[:12]
	}
	return h
}

// Report is what Verify found for a workspace.
type Report struct {
	Name string
	// Exists reports a signed record (any state). False means none: the
	// workspace is trusted only if a legacy marker counts for it.
	Exists bool
	// Invalid names why the record cannot be trusted at all: unreadable,
	// for another workspace, or its signature does not verify
	// ("integrity record signature invalid"). "" when it verifies.
	Invalid string
	// ManifestHash and SignedAt are the record's, when it verifies.
	ManifestHash string
	SignedAt     string
	// Files is the number of covered paths the record holds.
	Files int
	// Changes lists every covered path that differs, sorted by path.
	Changes []Change
}

// Trusted reports whether the record verifies, names hash as the manifest
// hash and matches every covered file.
func (r Report) Trusted(hash string) bool {
	return r.Exists && r.Invalid == "" && r.ManifestHash == hash && len(r.Changes) == 0
}

// Verify checks name's signed record against the key and the covered files
// (Covered) as they are now. A missing record is Report{Exists: false}, not
// an error; an error is a problem reading a covered file or the key.
func Verify(name string) (Report, error) {
	rep := Report{Name: name}
	if !validKey(name) {
		return rep, fmt.Errorf("trust: invalid workspace name %q", name)
	}
	rec, ok, err := readRecord(name)
	if !ok {
		return rep, nil
	}
	rep.Exists = true
	switch {
	case err != nil:
		rep.Invalid = "integrity record unreadable: " + err.Error()
		return rep, nil
	case rec.Schema != Schema:
		rep.Invalid = fmt.Sprintf("integrity record schema %q is not %s", rec.Schema, Schema)
		return rep, nil
	case rec.Workspace != name:
		rep.Invalid = fmt.Sprintf("integrity record names workspace %q", rec.Workspace)
		return rep, nil
	}
	key, err := loadKey()
	if err != nil {
		return rep, err
	}
	if !rec.verify(key) {
		rep.Invalid = "integrity record signature invalid"
		return rep, nil
	}
	rep.ManifestHash, rep.SignedAt, rep.Files = rec.ManifestHash, rec.SignedAt, len(rec.Files)
	seen := map[string]bool{}
	var all []string
	for p := range rec.Files {
		if !seen[p] {
			seen[p] = true
			all = append(all, p)
		}
	}
	for _, p := range Covered(name) {
		if !seen[p] {
			seen[p] = true
			all = append(all, p)
		}
	}
	sort.Strings(all)
	for _, p := range all {
		cur, err := currentHash(p)
		if err != nil {
			return rep, fmt.Errorf("trust: hashing %s: %w", p, err)
		}
		old := rec.Files[p]
		if old == cur {
			continue
		}
		kind := "changed"
		switch {
		case old == "":
			kind = "added"
		case cur == "":
			kind = "removed"
		}
		rep.Changes = append(rep.Changes, Change{Path: p, Kind: kind, Old: old, New: cur})
	}
	return rep, nil
}

// Verdict is Check's answer.
type Verdict struct {
	OK bool
	// Mismatch is set when a signed record exists but does not verify or
	// match: the fix is `tq hooks verify`, not `tq allow`.
	Mismatch bool
	// Reason is "" when OK, else why the workspace is untrusted, ending in
	// the command that fixes it.
	Reason string
}

// Check decides whether workspace name, whose manifest hashes to hash, is
// trusted, with the reason when it is not. With a signed record the record
// decides (Verify); with none, a legacy marker of an older tq counts.
func Check(name, hash string) Verdict {
	if !validKey(name) || !validKey(hash) {
		return Verdict{Reason: "untrusted (invalid workspace name or hash)"}
	}
	rep, err := Verify(name)
	if err != nil {
		return Verdict{Mismatch: true, Reason: fmt.Sprintf("untrusted: %v (run: tq hooks verify %s)", err, name)}
	}
	if !rep.Exists {
		if HasLegacyNameRecord(name, hash) || HasLegacyRecord(hash) {
			return Verdict{OK: true}
		}
		return Verdict{Reason: fmt.Sprintf("untrusted (run: tq allow %s)", name)}
	}
	if rep.Invalid != "" {
		return Verdict{Mismatch: true, Reason: fmt.Sprintf("untrusted: %s (run: tq hooks verify %s)", rep.Invalid, name)}
	}
	if len(rep.Changes) > 0 {
		more := ""
		if n := len(rep.Changes) - 1; n > 0 {
			more = fmt.Sprintf(" and %d more", n)
		}
		return Verdict{Mismatch: true, Reason: fmt.Sprintf("untrusted: integrity mismatch, %s %s%s (run: tq hooks verify %s)",
			rep.Changes[0].Kind, rep.Changes[0].Path, more, name)}
	}
	if rep.ManifestHash != hash {
		return Verdict{Mismatch: true, Reason: fmt.Sprintf("untrusted: integrity mismatch, the manifest is not the one signed (run: tq hooks verify %s)", name)}
	}
	return Verdict{OK: true}
}

// IsTrusted is Check without the reason: the record exists, verifies,
// names hash and every covered file is as recorded (or a legacy marker
// counts).
func IsTrusted(name, hash string) bool { return Check(name, hash).OK }

// Allow signs the current state of workspace name as trusted: the manifest
// hash and every covered file as they are now (SignRecord), written to
// SignedRecordPath. A legacy marker for the same name and hash is removed,
// since the signed record now holds that trust. A human runs it (tq allow,
// tq hooks verify --accept).
func Allow(name, hash string) error {
	b, err := SignRecord(name, hash)
	if err != nil {
		return err
	}
	p := SignedRecordPath(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if err := writeAtomic(p, b); err != nil {
		return err
	}
	return removeLegacy(name, hash)
}

// Deny removes name's signed record and every legacy marker for the hash,
// so a denied workspace is not still trusted through an older form.
func Deny(name, hash string) error {
	if !validKey(name) || !validKey(hash) {
		return fmt.Errorf("trust: invalid workspace name %q or hash %q", name, hash)
	}
	if err := os.Remove(SignedRecordPath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return removeLegacy(name, hash)
}

func removeLegacy(name, hash string) error {
	for _, p := range []string{RecordPath(name, hash), LegacyRecordPath(hash)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	// The per-name folder of the previous layout, once empty.
	_ = os.Remove(filepath.Join(paths.TrustDir(), name))
	return nil
}
