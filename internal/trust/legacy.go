package trust

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// The markers of older versions (trust/<hash>, trust/<name>/<hash>) are
// empty files anyone can create, so they cannot count forever. The rule:
//
//   - before anything was ever signed on this machine (no key and no list),
//     every marker counts, as it did for the tq that wrote it (PRD §13:
//     nothing changes for an existing identity on upgrade);
//   - the first signature takes a signed snapshot of the markers then on
//     disk, trust/legacy.json; from then on a marker counts only while the
//     snapshot lists it and the snapshot verifies. A marker created later
//     (a downgrade) never counts, and a missing or forged snapshot makes
//     none count (fail closed);
//   - a workspace that has a signed record file (valid, revoked or broken)
//     never counts a marker again, and every signature or revocation of a
//     workspace removes its markers and drops them from the snapshot.
//
// So a signed record cannot be downgraded by deleting it and planting a
// marker: the planted marker is not in the snapshot.

const legacySchema = "tentaqles-integrity-legacy-v1"

// LegacyListPath is the signed snapshot of the legacy markers that still
// count, trust/legacy.json.
func LegacyListPath() string { return filepath.Join(paths.TrustDir(), "legacy.json") }

type legacyList struct {
	Schema    string   `json:"schema"`
	Markers   []string `json:"markers"`
	KeyID     string   `json:"key_id"`
	SignedAt  string   `json:"signed_at"`
	Signature string   `json:"signature,omitempty"`
}

func (l legacyList) payload() ([]byte, error) {
	m := append([]string{}, l.Markers...)
	sort.Strings(m)
	return json.Marshal(struct {
		Schema   string   `json:"schema"`
		Markers  []string `json:"markers"`
		KeyID    string   `json:"key_id"`
		SignedAt string   `json:"signed_at"`
	}{l.Schema, m, l.KeyID, l.SignedAt})
}

func (l legacyList) mac(key []byte) (string, error) {
	p, err := l.payload()
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write(p)
	return hex.EncodeToString(m.Sum(nil)), nil
}

// legacyState is what the snapshot allows: all (nothing signed yet), or the
// listed markers.
type legacyState struct {
	all    bool
	listed map[string]bool
}

// isHash reports a 64-digit lower-case hex name, the name of a marker.
func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// legacyOnDisk lists the markers on disk, relative to the trust folder with
// forward slashes: "<hash>" and "<name>/<hash>".
func legacyOnDisk() []string {
	var out []string
	entries, _ := os.ReadDir(paths.TrustDir())
	for _, e := range entries {
		switch {
		case !e.IsDir() && isHash(e.Name()):
			out = append(out, e.Name())
		case e.IsDir() && validKey(e.Name()):
			sub, _ := os.ReadDir(filepath.Join(paths.TrustDir(), e.Name()))
			for _, s := range sub {
				if !s.IsDir() && isHash(s.Name()) {
					out = append(out, e.Name()+"/"+s.Name())
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// legacyBytes is the signed snapshot listing markers.
func legacyBytes(key []byte, markers []string) ([]byte, error) {
	if markers == nil {
		markers = []string{}
	}
	sort.Strings(markers)
	l := legacyList{Schema: legacySchema, Markers: markers, KeyID: keyID(key), SignedAt: now().UTC().Format(time.RFC3339)}
	sig, err := l.mac(key)
	if err != nil {
		return nil, err
	}
	l.Signature = sig
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// snapshotLegacy writes the first snapshot, when the first key is made. An
// existing list is never replaced: one the new key cannot verify keeps
// every marker from counting.
func snapshotLegacy(key []byte) error {
	if exists(LegacyListPath()) {
		return nil
	}
	b, err := legacyBytes(key, legacyOnDisk())
	if err != nil {
		return err
	}
	if err := writeAtomic(LegacyListPath(), b); err != nil {
		return err
	}
	invalidate()
	return nil
}

// allowedLegacy reads what the snapshot allows.
func allowedLegacy() legacyState {
	if s, ok := memoLegacy(); ok {
		return s
	}
	s := readLegacy()
	storeLegacy(s)
	return s
}

func readLegacy() legacyState {
	ks := readKey()
	listExists := exists(LegacyListPath())
	if !ks.ok && ks.err == nil && !listExists {
		return legacyState{all: true}
	}
	if ks.err != nil || !ks.ok || !listExists {
		return legacyState{}
	}
	l, ok := verifiedLegacy(ks.key)
	if !ok {
		return legacyState{}
	}
	set := map[string]bool{}
	for _, m := range l.Markers {
		set[m] = true
	}
	return legacyState{listed: set}
}

func verifiedLegacy(key []byte) (legacyList, bool) {
	raw, err := os.ReadFile(LegacyListPath())
	if err != nil {
		return legacyList{}, false
	}
	var l legacyList
	if json.Unmarshal(raw, &l) != nil || l.Schema != legacySchema || l.KeyID != keyID(key) {
		return legacyList{}, false
	}
	want, err := l.mac(key)
	if err != nil {
		return legacyList{}, false
	}
	got, err1 := hex.DecodeString(l.Signature)
	w, err2 := hex.DecodeString(want)
	if err1 != nil || err2 != nil || !hmac.Equal(got, w) {
		return legacyList{}, false
	}
	return l, true
}

// legacyRel is a marker's name relative to the trust folder.
func legacyRel(abs string) string {
	r, err := filepath.Rel(paths.TrustDir(), abs)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(r)
}

// LegacyMarkers lists the markers of an older tq that count for workspace
// name with manifest hash, as paths: the hash-only trust/<hash> and the
// name-keyed trust/<name>/<hash>, each only while the snapshot allows it,
// and none once the workspace has a signed record file.
func LegacyMarkers(name, hash string) []string {
	if !validKey(name) || !validKey(hash) || exists(SignedRecordPath(name)) {
		return nil
	}
	var cands []string
	if exists(LegacyRecordPath(hash)) {
		cands = append(cands, LegacyRecordPath(hash))
	}
	if exists(RecordPath(name, hash)) {
		cands = append(cands, RecordPath(name, hash))
	}
	if len(cands) == 0 {
		return nil
	}
	s := allowedLegacy()
	var out []string
	for _, c := range cands {
		if s.all || s.listed[legacyRel(c)] {
			out = append(out, c)
		}
	}
	return out
}

// dropLegacy rewrites the snapshot without the markers at abs (removed by
// a signature or a revocation). Without a snapshot that verifies there is
// nothing to drop. Callers have passed HumanCheck.
func dropLegacy(key []byte, abs []string) error {
	b, ok, err := legacyWithout(key, abs)
	if err != nil || !ok {
		return err
	}
	if err := writeAtomic(LegacyListPath(), b); err != nil {
		return err
	}
	invalidate()
	return nil
}

func legacyWithout(key []byte, abs []string) ([]byte, bool, error) {
	l, ok := verifiedLegacy(key)
	if !ok {
		return nil, false, nil
	}
	drop := map[string]bool{}
	for _, a := range abs {
		drop[legacyRel(a)] = true
	}
	var keep []string
	for _, m := range l.Markers {
		if !drop[m] {
			keep = append(keep, m)
		}
	}
	if len(keep) == len(l.Markers) {
		return nil, false, nil
	}
	b, err := legacyBytes(key, keep)
	return b, err == nil, err
}

// LegacyListWithout returns the snapshot rewritten without the markers at
// abs, for `tq migrate manifests`, which writes it under its journal; ok is
// false when there is nothing to change. It is a signature, so it passes
// HumanCheck and may create the key (and the first snapshot).
func LegacyListWithout(abs []string) (content []byte, ok bool, err error) {
	if err := HumanCheck("tq: update the legacy trust list"); err != nil {
		return nil, false, err
	}
	key, err := signingKey()
	if err != nil {
		return nil, false, err
	}
	return legacyWithout(key, abs)
}
