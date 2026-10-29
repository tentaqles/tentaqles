package trust

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
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
}

// GrantPath returns the grant file for key (a manifest hash or GlobalGrantKey).
func GrantPath(key string) string { return file(key, GrantSuffix) }

// LoadGrant reads key's grant. No file is (nil, nil). An unreadable or
// invalid file is an error, which callers treat as no grant (fail closed).
//
// Interface commit: reads the .mode file only. WS1 adds reading a legacy
// <hash>.bypass file as a relaxed grant (spec E §11.1).
func LoadGrant(key string) (*Grant, error) {
	raw, err := os.ReadFile(GrantPath(key))
	if errors.Is(err, os.ErrNotExist) {
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

// DeleteGrant removes key's grant; a missing one is not an error.
func DeleteGrant(key string) error {
	if err := os.Remove(GrantPath(key)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
