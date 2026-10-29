package trust

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// GrantSuffix names a mode grant file: $TQ_HOME/trust/<manifest-hash>.mode.
const GrantSuffix = ".mode"

// GlobalGrantKey is the key of the global grant ($TQ_HOME/trust/global.mode),
// the ceiling for neutral folders (spec E §6.3).
const GlobalGrantKey = "global"

// Grant is a human's one-time permission for a workspace (or, with
// GlobalGrantKey, the global guard.yaml) to run looser than default: the
// ceiling. Table and Overrides hold guardcfg category/id -> value names
// (skip|ask|deny|rule); trust does not import guardcfg.
type Grant struct {
	Mode      string            `json:"mode"` // relaxed | custom
	Table     map[string]string `json:"table,omitempty"`
	Overrides map[string]string `json:"overrides,omitempty"`
	GrantedAt time.Time         `json:"granted_at"`
	// Digest (global grant only) is the SHA-256 of guard.yaml's mode, base,
	// categories and rules_override keys when the grant was made.
	Digest string `json:"digest,omitempty"`
	// Workspace names the workspace the grant was made for, so tq allow can
	// tell the human a re-trust dropped it. Empty for the global grant and
	// for grants read from a legacy .bypass file.
	Workspace string `json:"workspace,omitempty"`
	// Root is the workspace root the grant was made for. A grant is keyed by
	// the manifest hash and this root together: a byte copy of a granted
	// manifest in another folder hashes the same but gets no grant
	// (security review m4). Empty for the global grant and legacy grants.
	Root string `json:"root,omitempty"`

	// FromBypass is set on a grant read from a legacy <hash>.bypass file
	// (spec E §11.1): a relaxed grant with no table.
	FromBypass bool `json:"-"`
}

// GrantPath returns the grant file for key (a manifest hash or GlobalGrantKey).
func GrantPath(key string) string { return file(key, GrantSuffix) }

// LoadGrant reads key's grant. No file is (nil, nil). An unreadable or
// invalid file is an error, which callers treat as no grant (fail closed).
// A workspace key with no .mode file but a legacy .bypass file (tq allow
// --bypass before spec E) reads as a relaxed grant.
func LoadGrant(key string) (*Grant, error) {
	raw, err := os.ReadFile(GrantPath(key))
	if errors.Is(err, os.ErrNotExist) {
		if key != GlobalGrantKey && exists(file(key, ".bypass")) {
			return &Grant{Mode: "relaxed", FromBypass: true}, nil
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var g Grant
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("%s: %w", GrantPath(key), err)
	}
	if g.Mode == "" {
		return nil, fmt.Errorf("%s: grant without mode", GrantPath(key))
	}
	return &g, nil
}

// SaveGrant writes key's grant atomically (0600). A zero GrantedAt is set to now.
func SaveGrant(key string, g Grant) error {
	if g.Mode == "" {
		return errors.New("grant without mode")
	}
	if g.GrantedAt.IsZero() {
		g.GrantedAt = time.Now().UTC()
	}
	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	p := GrantPath(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// DeleteGrant removes key's grant, and a legacy .bypass file for a
// workspace key; a missing one is not an error.
func DeleteGrant(key string) error {
	suffixes := []string{GrantSuffix}
	if key != GlobalGrantKey {
		suffixes = append(suffixes, ".bypass")
	}
	for _, s := range suffixes {
		if err := os.Remove(file(key, s)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// AdoptBypass files a legacy <hash>.bypass grant as a .mode grant named for
// workspace ws at root (a relaxed grant with no table, which reads exactly
// as the .bypass file does), so GrantsFor finds it after a re-trust and it
// is pinned to that folder. It does nothing when hash has a .mode grant
// already, has no .bypass file, or is the global key.
func AdoptBypass(hash, ws, root string) error {
	if hash == "" || ws == "" || hash == GlobalGrantKey || exists(GrantPath(hash)) || !exists(file(hash, ".bypass")) {
		return nil
	}
	return SaveGrant(hash, Grant{Mode: "relaxed", Workspace: ws, Root: root})
}

// KeyedGrant is a stored grant with the key (manifest hash) it is filed under.
type KeyedGrant struct {
	Key   string
	Grant Grant
}

// GrantsFor returns the stored .mode grants made for workspace ws, sorted by
// key. Unreadable files and the global grant are skipped.
func GrantsFor(ws string) []KeyedGrant {
	if ws == "" {
		return nil
	}
	entries, err := os.ReadDir(paths.TrustDir())
	if err != nil {
		return nil
	}
	var out []KeyedGrant
	for _, e := range entries {
		name := e.Name()
		key, ok := strings.CutSuffix(name, GrantSuffix)
		if !ok || e.IsDir() || key == GlobalGrantKey {
			continue
		}
		g, err := LoadGrant(key)
		if err != nil || g == nil || g.Workspace != ws {
			continue
		}
		out = append(out, KeyedGrant{Key: key, Grant: *g})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
