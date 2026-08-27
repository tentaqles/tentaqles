package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const stateFileName = ".tq-bundle-state.json"

// State records which MCP servers and skills tq itself installed into a
// workspace's Claude identity dir, so sync can prune entries it no longer
// owns without touching anything else.
type State struct {
	MCP      []string `json:"mcp"`
	Skills   []string `json:"skills"`
	SyncedAt string   `json:"synced_at"`
}

// LoadState reads <dir>/.tq-bundle-state.json. A missing or unreadable file
// yields a zero-value State, not an error.
func LoadState(dir string) State {
	raw, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		return State{}
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}
	}
	return s
}

// Save writes s to <dir>/.tq-bundle-state.json, stamping SyncedAt if unset.
func (s State) Save(dir string) error {
	if s.SyncedAt == "" {
		s.SyncedAt = time.Now().UTC().Format(time.RFC3339)
	}
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(filepath.Join(dir, stateFileName), out, 0o600)
}
