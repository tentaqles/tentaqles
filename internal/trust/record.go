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
// later is a change.
type Record struct {
	Schema       string            `json:"schema"`
	Workspace    string            `json:"workspace"`
	ManifestHash string            `json:"manifest_hash"`
	Files        map[string]string `json:"files"`
	SignedAt     string            `json:"signed_at"`
	Signature    string            `json:"signature,omitempty"`
}

// payload is the canonical JSON the signature covers: every field but the
// signature, in this order, map keys sorted (encoding/json sorts them), no
// whitespace. The file on disk is indented for reading; only the payload
// is signed, so re-indenting the file changes nothing and any change to a
// value does.
func (r Record) payload() ([]byte, error) {
	files := r.Files
	if files == nil {
		files = map[string]string{}
	}
	return json.Marshal(struct {
		Schema       string            `json:"schema"`
		Workspace    string            `json:"workspace"`
		ManifestHash string            `json:"manifest_hash"`
		Files        map[string]string `json:"files"`
		SignedAt     string            `json:"signed_at"`
	}{r.Schema, r.Workspace, r.ManifestHash, files, r.SignedAt})
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
func SignRecord(name, hash string) ([]byte, error) {
	if !validKey(name) || !validKey(hash) {
		return nil, fmt.Errorf("trust: invalid workspace name %q or hash %q", name, hash)
	}
	files := map[string]string{}
	for _, p := range Covered(name) {
		h, err := currentHash(p)
		if err != nil {
			return nil, fmt.Errorf("trust: hashing %s: %w", p, err)
		}
		files[p] = h
	}
	rec := Record{Schema: Schema, Workspace: name, ManifestHash: hash, Files: files,
		SignedAt: now().UTC().Format(time.RFC3339)}
	key, err := loadKey()
	if err != nil {
		return nil, err
	}
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
