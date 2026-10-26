package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

func (s *Store) maskAny(v any) any {
	switch x := v.(type) {
	case string:
		return s.Mask(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = s.maskAny(e)
		}
		return out
	case []string:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = s.Mask(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = s.maskAny(e)
		}
		return out
	}
	return v
}

// Log records one mutation: a row in `activity` and a line in activity.jsonl, both masked.
// Row first; the jsonl mirror and publish are best-effort for callers that already committed their
// own write. If the append fails the error is returned but the row stays (no rollback).
func (s *Store) Log(e activity.Entry) error {
	e.Workspace = s.Workspace
	if e.TS == "" {
		e.TS = Now()
	}
	e.Actor = s.Mask(e.Actor)
	if e.Data != nil {
		e.Data = s.maskAny(e.Data).(map[string]any)
	}
	data, err := json.Marshal(e.Data)
	if err != nil {
		return err
	}
	if e.Data == nil {
		data = []byte("{}")
	}
	if _, err := s.DB.Exec(`INSERT INTO activity(ts, workspace, actor, verb, card_id, run_id, data) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.TS, e.Workspace, e.Actor, e.Verb, nullStr(e.CardID), nullStr(e.RunID), string(data)); err != nil {
		return err
	}
	s.logMu.Lock()
	err = activity.Append(ActivityPath(s.Workspace), e)
	s.logMu.Unlock()
	s.Publish(Change{Kind: "activity", CardID: e.CardID, RunID: e.RunID})
	if err != nil {
		return fmt.Errorf("activity.jsonl: %w", err)
	}
	return nil
}

func (s *Store) Activity(limit int) ([]activity.Entry, error) {
	if limit <= 0 {
		limit = 200
	}
	return scanActivity(s.DB.Query(`SELECT ts, workspace, actor, verb, COALESCE(card_id, ''), COALESCE(run_id, ''), data
		FROM activity ORDER BY rowid DESC LIMIT ?`, limit))
}

// ActivitySince is every entry at or after since, newest first, at most limit (0 → 500). Stored
// times are RFC3339 UTC, so the text comparison is a time comparison.
func (s *Store) ActivitySince(since time.Time, limit int) ([]activity.Entry, error) {
	if limit <= 0 {
		limit = 500
	}
	return scanActivity(s.DB.Query(`SELECT ts, workspace, actor, verb, COALESCE(card_id, ''), COALESCE(run_id, ''), data
		FROM activity WHERE ts >= ? ORDER BY rowid DESC LIMIT ?`, since.UTC().Format(time.RFC3339), limit))
}

func scanActivity(rows *sql.Rows, err error) ([]activity.Entry, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []activity.Entry
	for rows.Next() {
		var e activity.Entry
		var data string
		if err := rows.Scan(&e.TS, &e.Workspace, &e.Actor, &e.Verb, &e.CardID, &e.RunID, &data); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(data), &e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}
