// Package trust records what the user has explicitly allowed, per
// workspace: a signed integrity record (trust/<name>.json) over the
// manifest and the files that make the hook run: the root marker, the
// enforcing keys of the identity's Claude settings, the installed plugin's
// hooks, scripts and manifest, and the shims (PRD v2
// §8.3, ADR-0004). A workspace is trusted when its record exists, is not
// revoked, was signed with the key in the OS keychain and its HMAC
// verifies, its manifest hash is the one resolve read, and every covered
// file still hashes as recorded. A human signs the record with `tq allow`
// and again with `tq hooks verify --accept`; `tq deny` turns it into a
// signed tombstone. Every signature passes HumanCheck.
//
// The markers of older versions (trust/<hash>, trust/<name>/<hash>) count
// only for a workspace that never had a signed record, and only while the
// signed snapshot taken at the first signature lists them (legacy.go).
package trust

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// RecordPath is the name-keyed marker of the previous tq, trust/<name>/<hash>:
// read as a legacy marker, never written.
func RecordPath(name, hash string) string { return filepath.Join(paths.TrustDir(), name, hash) }

// LegacyRecordPath is the hash-only marker of an older tq, trust/<hash>:
// read as a legacy marker, never written.
func LegacyRecordPath(hash string) string { return filepath.Join(paths.TrustDir(), hash) }

// validKey rejects a name or hash that could name a path outside the trust
// folder (a separator, "." or "..").
func validKey(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`) && filepath.Base(s) == s
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// HasLegacyNameRecord reports the name-keyed marker trust/<name>/<hash> on
// disk, whether or not it counts (LegacyMarkers).
func HasLegacyNameRecord(name, hash string) bool {
	return validKey(name) && validKey(hash) && exists(RecordPath(name, hash))
}

// HasLegacyRecord reports the hash-only marker trust/<hash> on disk,
// whether or not it counts (LegacyMarkers).
func HasLegacyRecord(hash string) bool { return validKey(hash) && exists(LegacyRecordPath(hash)) }

// HasRecord reports a signed, unrevoked record for name that names hash as
// the manifest hash. It reads the record without verifying it: Verify does.
func HasRecord(name, hash string) bool {
	if !validKey(name) || !validKey(hash) {
		return false
	}
	rec, ok, err := readRecord(name)
	return ok && err == nil && !rec.Revoked && rec.ManifestHash == hash
}

// Change is one covered key whose hash is not what the record holds.
type Change struct {
	// Path is the covered key (Key); "<dir>/settings.json#tq" is the part of
	// that file tq covers.
	Path string
	// Kind is "changed", "added" (absent when signed, present now) or
	// "removed" (present when signed, absent now).
	Kind string
	// Old and New are the recorded and the current hash; "" is absent.
	Old, New string
}

// String is "<kind> <path> (<old12> -> <new12>)".
func (c Change) String() string {
	return fmt.Sprintf("%s %s (%s -> %s)", c.Kind, c.Path, Short(c.Old), Short(c.New))
}

// Short is a hash as reports print it: 12 hex digits, "absent", or what a
// marker hash stands for.
func Short(h string) string {
	switch {
	case h == "":
		return "absent"
	case h == irregular:
		return "not a regular file"
	case h == invalidJSON:
		return "not valid JSON"
	case len(h) > 12:
		return h[:12]
	}
	return h
}

// Report is what Verify found for a workspace.
type Report struct {
	Name string
	// Exists reports a signed record file (any state). False means none:
	// the workspace is trusted only if a legacy marker counts for it.
	Exists bool
	// Revoked reports a verified tombstone (tq deny).
	Revoked bool
	// Invalid names why the record cannot be trusted at all: unreadable,
	// for another workspace, the key is missing or changed ("integrity key
	// changed"), or the signature does not verify ("integrity record
	// signature invalid"). "" when it verifies.
	Invalid string
	// KeyID is the id of the key in use ("" when there is none).
	KeyID string
	// ManifestHash and SignedAt are the record's, when it verifies.
	ManifestHash string
	SignedAt     string
	// Files is the number of covered keys the record holds.
	Files int
	// Changes lists every covered key that differs, sorted by key.
	Changes []Change
	// PluginFrom and PluginTo are the plugin versions signed and now, set
	// when the installed tentaqles plugin moved to another version folder;
	// PluginFiles counts its hook files (by path inside the plugin) that
	// differ between the two. PluginBump reports that every change lies in
	// the two plugin folders or in installed_plugins.json#tq. It says where
	// the changes are, never what they hold: it only makes the workspace a
	// candidate for tq hooks verify --accept --all, and CheckRelease decides
	// whether the new folder is the published release.
	PluginFrom, PluginTo string
	PluginFiles          int
	PluginBump           bool
}

// Trusted reports whether the record verifies, is not revoked, names hash
// as the manifest hash and matches every covered file.
func (r Report) Trusted(hash string) bool {
	return r.Exists && !r.Revoked && r.Invalid == "" && r.ManifestHash == hash && len(r.Changes) == 0
}

// Verify checks name's signed record against the key and the covered files
// (Covered) as they are now. A missing record is Report{Exists: false}, not
// an error; an error is a problem reading a covered file. Within a memo
// scope it verifies once per workspace.
func Verify(name string) (Report, error) {
	if !validKey(name) {
		return Report{Name: name}, fmt.Errorf("trust: invalid workspace name %q", name)
	}
	if r, ok := memoVerify(name); ok {
		return r.rep, r.err
	}
	rep, err := verify(name)
	storeVerify(name, verifyResult{rep, err})
	return rep, err
}

func verify(name string) (Report, error) {
	verifications.Add(1)
	rep := Report{Name: name}
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
	ks := readKey()
	switch {
	case ks.err != nil:
		rep.Invalid = "integrity key unreadable: " + ks.err.Error()
		return rep, nil
	case !ks.ok:
		rep.Invalid = fmt.Sprintf("integrity key missing (the record was signed with key %s)", rec.KeyID)
		return rep, nil
	}
	rep.KeyID = keyID(ks.key)
	if rec.KeyID != rep.KeyID {
		rep.Invalid = fmt.Sprintf("integrity key changed (the record was signed with key %s, the key now is %s)", orNone(rec.KeyID), rep.KeyID)
		return rep, nil
	}
	if !rec.verify(ks.key) {
		rep.Invalid = "integrity record signature invalid"
		return rep, nil
	}
	rep.SignedAt = rec.SignedAt
	if rec.Revoked {
		rep.Revoked = true
		return rep, nil
	}
	rep.ManifestHash, rep.Files = rec.ManifestHash, len(rec.Files)
	cov := coverageOf(name)
	seen := map[string]bool{}
	var all []string
	for _, p := range append(keysOf(rec.Files), cov.keys...) {
		if !seen[p] {
			seen[p] = true
			all = append(all, p)
		}
	}
	sort.Strings(all)
	cur, err := hashKeys(all)
	if err != nil {
		return rep, err
	}
	for _, p := range all {
		h, old := cur[p], rec.Files[p]
		if old == h {
			continue
		}
		kind := "changed"
		switch {
		case old == "":
			kind = "added"
		case h == "":
			kind = "removed"
		}
		rep.Changes = append(rep.Changes, Change{Path: p, Kind: kind, Old: old, New: h})
	}
	if now := cov.plugin; rec.Plugin != "" && now != "" && rec.Plugin != now {
		rep.PluginFrom, rep.PluginTo = path.Base(rec.Plugin), path.Base(now)
		rep.PluginFiles = pluginDiff(rec.Plugin, rec.Files, now, cur)
		rep.PluginBump = true
		for _, ch := range rep.Changes {
			inPlugin := strings.HasPrefix(ch.Path, rec.Plugin+"/") || strings.HasPrefix(ch.Path, now+"/") || ch.Path == cov.installed
			if !inPlugin {
				rep.PluginBump = false
				break
			}
		}
	}
	return rep, nil
}

// pluginDiff counts the hook files, by path inside the plugin folder, whose
// hash differs between the signed folder and the current one.
func pluginDiff(oldRoot string, oldFiles map[string]string, newRoot string, cur map[string]string) int {
	rel := func(root string, m map[string]string) map[string]string {
		out := map[string]string{}
		for p, h := range m {
			if r, ok := strings.CutPrefix(p, root+"/"); ok && h != "" {
				out[r] = h
			}
		}
		return out
	}
	o, n := rel(oldRoot, oldFiles), rel(newRoot, cur)
	count := 0
	for r, h := range o {
		if n[r] != h {
			count++
		}
	}
	for r := range n {
		if _, ok := o[r]; !ok {
			count++
		}
	}
	return count
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// The kinds of Verdict.
const (
	KindTrusted   = "trusted"
	KindLegacy    = "legacy"    // trusted through a marker of an older tq
	KindUntrusted = "untrusted" // never allowed, or revoked: tq allow
	KindMismatch  = "mismatch"  // the record fails: tq hooks verify
)

// Verdict is Check's answer.
type Verdict struct {
	OK bool
	// Kind is one of the Kind constants.
	Kind string
	// Mismatch is set when a signed record exists but does not verify or
	// match: the fix is `tq hooks verify`, not `tq allow`.
	Mismatch bool
	// Reason is "" when OK, else why the workspace is untrusted, ending in
	// the command that fixes it.
	Reason string
}

// Check decides whether workspace name, whose manifest hashes to hash, is
// trusted, with the reason when it is not. With a signed record the record
// decides (Verify); with none, a legacy marker counts only as
// LegacyMarkers says.
func Check(name, hash string) Verdict {
	if !validKey(name) || !validKey(hash) {
		return Verdict{Kind: KindUntrusted, Reason: "untrusted (invalid workspace name or hash)"}
	}
	rep, err := Verify(name)
	mismatch := func(why string) Verdict {
		return Verdict{Kind: KindMismatch, Mismatch: true, Reason: fmt.Sprintf("untrusted: %s (run: tq hooks verify %s)", why, name)}
	}
	switch {
	case err != nil:
		return mismatch(err.Error())
	case !rep.Exists:
		if len(LegacyMarkers(name, hash)) > 0 {
			return Verdict{OK: true, Kind: KindLegacy}
		}
		return Verdict{Kind: KindUntrusted, Reason: fmt.Sprintf("untrusted (run: tq allow %s)", name)}
	case rep.Invalid != "":
		return mismatch(rep.Invalid)
	case rep.Revoked:
		return Verdict{Kind: KindUntrusted, Reason: fmt.Sprintf("untrusted: trust revoked by tq deny (run: tq allow %s)", name)}
	case len(rep.Changes) > 0:
		more := ""
		if n := len(rep.Changes) - 1; n > 0 {
			more = fmt.Sprintf(" and %d more", n)
		}
		return mismatch(fmt.Sprintf("integrity mismatch, %s %s%s", rep.Changes[0].Kind, rep.Changes[0].Path, more))
	case rep.ManifestHash != hash:
		return mismatch("integrity mismatch, the manifest is not the one signed")
	}
	return Verdict{OK: true, Kind: KindTrusted}
}

// IsTrusted is Check without the reason: the record exists, verifies,
// names hash and every covered file is as recorded (or a legacy marker
// counts).
func IsTrusted(name, hash string) bool { return Check(name, hash).OK }

// Allow signs the current state of workspace name as trusted: the manifest
// hash and every covered file as they are now (SignRecord, which refuses
// unless a human runs tq), written to SignedRecordPath. The legacy markers
// for that name and hash are removed and dropped from the snapshot, since
// the signed record now holds that trust.
func Allow(name, hash string) error {
	b, err := SignRecord(name, hash)
	if err != nil {
		return err
	}
	if err := writeAtomic(SignedRecordPath(name), b); err != nil {
		return err
	}
	invalidate()
	return removeLegacy(name, hash)
}

// Deny revokes name's trust: the record is rewritten as a signed tombstone
// (never deleted, so the workspace can never fall back to a legacy
// marker), and every legacy marker for the hash is removed. It refuses
// unless a human runs tq (HumanCheck).
func Deny(name, hash string) error {
	if !validKey(name) || !validKey(hash) {
		return fmt.Errorf("trust: invalid workspace name %q or hash %q", name, hash)
	}
	if err := HumanCheck("tq: revoke the integrity record of " + name); err != nil {
		return err
	}
	key, err := signingKey()
	if err != nil {
		return err
	}
	b, err := tombstone(name, key)
	if err != nil {
		return err
	}
	if err := writeAtomic(SignedRecordPath(name), b); err != nil {
		return err
	}
	invalidate()
	return removeLegacy(name, hash)
}

// removeLegacy deletes the markers for name and hash and drops them from
// the snapshot. Callers have passed HumanCheck.
func removeLegacy(name, hash string) error {
	var removed []string
	for _, p := range []string{RecordPath(name, hash), LegacyRecordPath(hash)} {
		if err := os.Remove(p); err == nil {
			removed = append(removed, p)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	// The per-name folder of the previous layout, once empty.
	_ = os.Remove(filepath.Join(paths.TrustDir(), name))
	if len(removed) == 0 {
		return nil
	}
	key, err := signingKey()
	if err != nil {
		return err
	}
	return dropLegacy(key, removed)
}
