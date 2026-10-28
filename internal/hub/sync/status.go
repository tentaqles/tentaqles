package sync

import (
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// Overview is the engine's state as tq hub sync status and the Connections page show it.
type Overview struct {
	Workspace string               `json:"workspace"`
	Provider  string               `json:"provider"`
	Scope     string               `json:"scope"`
	Auth      string               `json:"auth"` // the manifest's preferred source; the chain order is fixed
	Settings  store.SyncSettings   `json:"settings"`
	Enabled   bool                 `json:"enabled"`
	States    []store.SyncState    `json:"states"`
	NextAt    string               `json:"next_at,omitempty"` // the last poll plus the interval
	Outbox    map[string]int       `json:"outbox"`            // rows by status
	LaneMap   []store.LaneMapEntry `json:"lane_map"`
	// Fallback is the MCP fallback's own cursor (tq hub sync import), kept apart from the engine's
	// watermark; nil before the first import.
	Fallback *store.SyncState `json:"fallback,omitempty"`
}

// Overview reads the sync state of this engine's provider and scope. It never calls the tracker.
func (e *Engine) Overview() (Overview, error) {
	ov := Overview{Workspace: e.cfg.Workspace, Provider: e.provider, Scope: e.scope, Auth: e.cfg.Tracker.AuthOrDefault(),
		Settings: e.st.SyncSettings(true), Outbox: map[string]int{}}
	ov.Enabled = ov.Settings.Enabled
	st, ok, err := e.st.SyncState(e.provider, e.scope)
	if err != nil {
		return ov, err
	}
	ov.States = []store.SyncState{}
	if ok {
		ov.States = append(ov.States, st)
		last := st.LastOKAt
		if st.LastErrorAt > last {
			last = st.LastErrorAt
		}
		if t, err := time.Parse(time.RFC3339, last); err == nil && ov.Enabled {
			ov.NextAt = t.Add(time.Duration(ov.Settings.Interval) * time.Second).UTC().Format(time.RFC3339)
		}
	}
	if fb, ok, err := e.st.SyncState(e.provider, e.fallbackScope()); err != nil {
		return ov, err
	} else if ok {
		ov.Fallback = &fb
	}
	for _, s := range store.OutboxStatuses {
		ov.Outbox[s] = 0
	}
	rows, err := e.st.DB.Query(`SELECT status, COUNT(*) FROM sync_outbox GROUP BY status`)
	if err != nil {
		return ov, err
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return ov, err
		}
		ov.Outbox[s] = n
	}
	if err := rows.Err(); err != nil {
		return ov, err
	}
	lm, err := e.st.LaneMap(e.provider, e.scope)
	if err != nil {
		return ov, err
	}
	if lm == nil {
		lm = []store.LaneMapEntry{}
	}
	ov.LaneMap = lm
	return ov, nil
}
