package trust

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// Schema is the integrity record's schema id.
const Schema = "tentaqles-integrity-v1"

// Record is the signed integrity record of a workspace (PRD v2 §8.3,
// ADR-0004): the manifest hash and the hash of every covered file at the
// moment a human allowed the workspace, under an HMAC-SHA256 signature. A
// file hash of "" records that the file was absent then; it appearing
// later is a change. A revoked record (tq deny) is a signed tombstone: it
// holds no files and trusts nothing, and it stays, so that a workspace
// that once had a record never falls back to a legacy marker.
type Record struct {
	Schema       string            `json:"schema"`
	Workspace    string            `json:"workspace"`
	Revoked      bool              `json:"revoked,omitempty"`
	ManifestHash string            `json:"manifest_hash,omitempty"`
	Files        map[string]string `json:"files,omitempty"`
	// Plugin is the key of the tentaqles plugin folder covered, "" when none
	// is installed; it names the version a later report compares against.
	Plugin string `json:"plugin,omitempty"`
	// KeyID is the id of the key that signed (keyID), so a record signed
	// with another key reports "integrity key changed".
	KeyID     string `json:"key_id"`
	SignedAt  string `json:"signed_at"`
	Signature string `json:"signature,omitempty"`
}

// payload is the canonical JSON the signature covers: every field but the
// signature, in this order and always present, map keys sorted (encoding/
// json sorts them), no whitespace. The file on disk is indented and omits
// empty fields; only the payload is signed, so re-indenting the file
// changes nothing and any change to a value does.
func (r Record) payload() ([]byte, error) {
	files := r.Files
	if files == nil {
		files = map[string]string{}
	}
	return json.Marshal(struct {
		Schema       string            `json:"schema"`
		Workspace    string            `json:"workspace"`
		Revoked      bool              `json:"revoked"`
		ManifestHash string            `json:"manifest_hash"`
		Files        map[string]string `json:"files"`
		Plugin       string            `json:"plugin"`
		KeyID        string            `json:"key_id"`
		SignedAt     string            `json:"signed_at"`
	}{r.Schema, r.Workspace, r.Revoked, r.ManifestHash, files, r.Plugin, r.KeyID, r.SignedAt})
}

func (r Record) sign(key []byte) (string, error) {
	p, err := r.payload()
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write(p)
	return hex.EncodeToString(m.Sum(nil)), nil
}

// verify reports whether r's signature is the HMAC of its payload under key.
func (r Record) verify(key []byte) bool {
	want, err := r.sign(key)
	if err != nil {
		return false
	}
	got, err := hex.DecodeString(r.Signature)
	if err != nil {
		return false
	}
	w, _ := hex.DecodeString(want)
	return hmac.Equal(got, w)
}

// SignedRecordPath is the integrity record of workspace name,
// trust/<name>.json.
func SignedRecordPath(name string) string { return filepath.Join(paths.TrustDir(), name+".json") }

// now is the signing clock (a seam for tests).
var now = time.Now

// SignRecord builds and signs the record of name's state as it is now:
// hash is the manifest hash resolve read (the file tq reads as the
// manifest), and every covered file (Covered) is hashed. It returns the
// record's bytes for the caller to write: Allow writes them to
// SignedRecordPath; `tq migrate manifests` writes them under its journal.
// It refuses unless a human at a terminal runs tq (HumanCheck).
func SignRecord(name, hash string) ([]byte, error) {
	if !validKey(name) || !validKey(hash) {
		return nil, fmt.Errorf("trust: invalid workspace name %q or hash %q", name, hash)
	}
	if err := HumanCheck("tq: sign the integrity record of " + name); err != nil {
		return nil, err
	}
	key, err := signingKey()
	if err != nil {
		return nil, err
	}
	cov := computeCoverage(name)
	files, err := hashKeys(cov.keys)
	if err != nil {
		return nil, err
	}
	return marshalSigned(Record{Schema: Schema, Workspace: name, ManifestHash: hash, Files: files,
		Plugin: cov.plugin, KeyID: keyID(key), SignedAt: now().UTC().Format(time.RFC3339)}, key)
}

// tombstone is the signed revoked record of name.
func tombstone(name string, key []byte) ([]byte, error) {
	return marshalSigned(Record{Schema: Schema, Workspace: name, Revoked: true, KeyID: keyID(key),
		SignedAt: now().UTC().Format(time.RFC3339)}, key)
}

func marshalSigned(rec Record, key []byte) ([]byte, error) {
	sig, err := rec.sign(key)
	if err != nil {
		return nil, err
	}
	rec.Signature = sig
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// readRecord loads name's signed record. exists is false when there is
// none; err is a read or parse problem of a record that is there.
func readRecord(name string) (rec Record, exists bool, err error) {
	raw, err := os.ReadFile(SignedRecordPath(name))
	if err != nil {
		if absent(err) {
			return Record{}, false, nil
		}
		return Record{}, true, err
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return Record{}, true, errors.New("not valid JSON")
	}
	return rec, true, nil
}
