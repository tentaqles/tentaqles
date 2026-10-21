package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	AdapterClaude  = "claude"
	AdapterProcess = "process"

	OnRampKickoff = "kickoff"
	OnRampFeature = "feature"
	OnRampFix     = "fix"
	OnRampBuild   = "build"
	OnRampResume  = "resume"

	PhaseStart     = "start"     // the on-ramp's prompt in the card's project folder
	PhaseRelocated = "relocated" // the on-ramp's prompt again, in the worktree TQ_HUB_RELOCATE named
	PhaseResume    = "resume"    // /tentaqles:build resume <slug> --headless
)

var ActiveStatuses = []string{StatusQueued, StatusRunning, StatusWaiting}

type Usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// Tokens is what the token budget counts: input + output + cache write (like the overnight loop).
func (u Usage) Tokens() int64 { return u.Input + u.Output + u.CacheWrite }

func (u Usage) Add(o Usage) Usage {
	return Usage{u.Input + o.Input, u.Output + o.Output, u.CacheRead + o.CacheRead, u.CacheWrite + o.CacheWrite}
}

type RoundRecord struct {
	N           int     `json:"n"`
	StartedAt   string  `json:"started_at"`
	Seconds     float64 `json:"seconds"`
	Usage       Usage   `json:"usage"`
	CostUSD     float64 `json:"cost_usd"`
	CostUnknown bool    `json:"cost_unknown,omitempty"`
	ExitCode    int     `json:"exit_code"`
	Subtype     string  `json:"subtype,omitempty"`
	Outcome     string  `json:"outcome,omitempty"`
}

type Run struct {
	ID           string          `json:"id"`
	CardID       string          `json:"card_id"`
	Workspace    string          `json:"workspace"`
	Adapter      string          `json:"adapter"`
	OnRamp       string          `json:"on_ramp"`
	ProjectDir   string          `json:"project_dir"`
	Worktree     string          `json:"worktree,omitempty"`
	Slug         string          `json:"slug,omitempty"`
	SessionID    string          `json:"session_id,omitempty"`
	Status       string          `json:"status"`
	Budget       Budget          `json:"budget"`
	Usage        Usage           `json:"usage"`
	CostUSD      float64         `json:"cost_usd"`
	Rounds       int             `json:"rounds"`
	Seconds      float64         `json:"seconds"`
	StartedAt    string          `json:"started_at,omitempty"`
	EndedAt      string          `json:"ended_at,omitempty"`
	LogPath      string          `json:"log_path"`
	State        json.RawMessage `json:"state,omitempty"`
	StopReason   string          `json:"stop_reason,omitempty"`
	QueuedAt     string          `json:"queued_at"`
	NotBefore    string          `json:"not_before"`
	UpdatedAt    string          `json:"updated_at"`
	Phase        string          `json:"phase"`
	Retried      int             `json:"retried"`
	Relocations  []string        `json:"relocations"`
	AllowedTools []string        `json:"allowed_tools"`
	RoundLog     []RoundRecord   `json:"round_log"`
	Kind         string          `json:"kind"`
}

func (r Run) Active() bool { return slices.Contains(ActiveStatuses, r.Status) }

// Exceeded names the first limit the run has reached, or "".
func (r Run) Exceeded() string {
	b := r.Budget
	switch {
	case b.Rounds > 0 && r.Rounds >= b.Rounds:
		return "rounds"
	case b.Tokens > 0 && r.Usage.Tokens() >= b.Tokens:
		return "tokens"
	case b.USD > 0 && r.CostUSD >= b.USD:
		return "usd"
	case b.Minutes > 0 && r.Seconds/60 >= b.Minutes:
		return "minutes"
	}
	return ""
}

const runCols = `id, card_id, workspace, adapter, on_ramp, project_dir, COALESCE(worktree, ''), COALESCE(slug, ''), COALESCE(session_id, ''),
	status, budget, usage, cost_usd, rounds, seconds, COALESCE(started_at, ''), COALESCE(ended_at, ''), log_path, COALESCE(state, ''),
	COALESCE(stop_reason, ''), queued_at, not_before, updated_at, phase, retried, relocations, allowed_tools, round_log, kind`

func scanRun(r scanner) (Run, error) {
	var x Run
	var budget, usage, state, reloc, tools, rlog string
	err := r.Scan(&x.ID, &x.CardID, &x.Workspace, &x.Adapter, &x.OnRamp, &x.ProjectDir, &x.Worktree, &x.Slug, &x.SessionID,
		&x.Status, &budget, &usage, &x.CostUSD, &x.Rounds, &x.Seconds, &x.StartedAt, &x.EndedAt, &x.LogPath, &state,
		&x.StopReason, &x.QueuedAt, &x.NotBefore, &x.UpdatedAt, &x.Phase, &x.Retried, &reloc, &tools, &rlog, &x.Kind)
	if err != nil {
		return x, err
	}
	_ = json.Unmarshal([]byte(budget), &x.Budget)
	_ = json.Unmarshal([]byte(usage), &x.Usage)
	if state != "" {
		x.State = json.RawMessage(state)
	}
	_ = json.Unmarshal([]byte(reloc), &x.Relocations)
	_ = json.Unmarshal([]byte(tools), &x.AllowedTools)
	_ = json.Unmarshal([]byte(rlog), &x.RoundLog)
	return x, nil
}

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *Store) CreateRun(r Run) (Run, error) {
	if !slices.Contains([]string{AdapterClaude, AdapterProcess}, r.Adapter) {
		return Run{}, fmt.Errorf("adapter %q: use claude or process", r.Adapter)
	}
	if !slices.Contains([]string{OnRampKickoff, OnRampFeature, OnRampFix, OnRampBuild, OnRampResume}, r.OnRamp) {
		return Run{}, fmt.Errorf("on-ramp %q is not known", r.OnRamp)
	}
	if _, err := s.Card(r.CardID); err != nil {
		return Run{}, err
	}
	now := Now()
	r.ID = NewID("r")
	r.Workspace = s.Workspace
	if r.Status == "" {
		r.Status = StatusQueued
	}
	if r.Budget == (Budget{}) {
		r.Budget = s.DefaultBudget()
	}
	if r.Phase == "" {
		r.Phase = PhaseStart
	}
	if r.QueuedAt == "" {
		r.QueuedAt = now
	}
	if r.NotBefore == "" {
		r.NotBefore = r.QueuedAt
	}
	r.UpdatedAt = now
	r.LogPath = filepath.Join(RunsDir(s.Workspace), r.ID+".log")
	if r.Relocations == nil {
		r.Relocations = []string{}
	}
	if r.AllowedTools == nil {
		r.AllowedTools = []string{}
	}
	if r.RoundLog == nil {
		r.RoundLog = []RoundRecord{}
	}
	_, err := s.DB.Exec(`INSERT INTO runs(id, card_id, workspace, adapter, on_ramp, project_dir, worktree, slug, session_id, status, budget,
		usage, log_path, queued_at, not_before, updated_at, phase, relocations, allowed_tools, round_log, kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.CardID, r.Workspace, r.Adapter, r.OnRamp, r.ProjectDir, nullStr(r.Worktree), nullStr(r.Slug), nullStr(r.SessionID),
		r.Status, jsonText(r.Budget), jsonText(r.Usage), r.LogPath, r.QueuedAt, r.NotBefore, r.UpdatedAt, r.Phase,
		jsonText(r.Relocations), jsonText(r.AllowedTools), jsonText(r.RoundLog), r.Kind)
	if err != nil {
		return Run{}, err
	}
	s.Publish(Change{Kind: "run", CardID: r.CardID, RunID: r.ID})
	return s.Run(r.ID)
}

func (s *Store) Run(id string) (Run, error) {
	r, err := scanRun(s.DB.QueryRow(`SELECT `+runCols+` FROM runs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("run %s: %w", id, ErrNotFound)
	}
	return r, err
}

func (s *Store) SaveRun(r Run) error {
	r.UpdatedAt = Now()
	var state any
	if len(r.State) > 0 {
		state = string(r.State)
	}
	res, err := s.DB.Exec(`UPDATE runs SET status = ?, worktree = ?, slug = ?, session_id = ?, budget = ?, usage = ?, cost_usd = ?,
		rounds = ?, seconds = ?, started_at = ?, ended_at = ?, state = ?, stop_reason = ?, not_before = ?, updated_at = ?, phase = ?,
		retried = ?, relocations = ?, allowed_tools = ?, round_log = ? WHERE id = ?`,
		r.Status, nullStr(r.Worktree), nullStr(r.Slug), nullStr(r.SessionID), jsonText(r.Budget), jsonText(r.Usage), r.CostUSD,
		r.Rounds, r.Seconds, nullStr(r.StartedAt), nullStr(r.EndedAt), state, nullStr(s.Mask(r.StopReason)), r.NotBefore, r.UpdatedAt,
		r.Phase, r.Retried, jsonText(nonNil(r.Relocations)), jsonText(nonNil(r.AllowedTools)), jsonText(r.RoundLog), r.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("run %s: %w", r.ID, ErrNotFound)
	}
	s.Publish(Change{Kind: "run", CardID: r.CardID, RunID: r.ID})
	return nil
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func (s *Store) runs(q string, args ...any) ([]Run, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RunsForCard(cardID string) ([]Run, error) {
	return s.runs(`SELECT `+runCols+` FROM runs WHERE card_id = ? ORDER BY queued_at DESC, rowid DESC`, cardID)
}

func (s *Store) ActiveRun(cardID string) (Run, bool, error) {
	rs, err := s.runs(`SELECT `+runCols+` FROM runs WHERE card_id = ? AND status IN ('queued','running','waiting')
		ORDER BY queued_at DESC, rowid DESC LIMIT 1`, cardID)
	if err != nil || len(rs) == 0 {
		return Run{}, false, err
	}
	return rs[0], true, nil
}

func (s *Store) DueRuns(now time.Time, limit int) ([]Run, error) {
	if limit <= 0 {
		return nil, nil
	}
	return s.runs(`SELECT `+runCols+` FROM runs WHERE status = 'queued' AND not_before <= ? ORDER BY queued_at, rowid LIMIT ?`,
		now.UTC().Format(time.RFC3339), limit)
}

func (s *Store) RunsWithStatus(statuses ...string) ([]Run, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	q := `SELECT ` + runCols + ` FROM runs WHERE status IN (?` + strings.Repeat(",?", len(statuses)-1) + `) ORDER BY queued_at`
	args := make([]any, len(statuses))
	for i, st := range statuses {
		args[i] = st
	}
	return s.runs(q, args...)
}

type Lock struct {
	CardID    string `json:"card_id"`
	RunID     string `json:"run_id"`
	RunStatus string `json:"run_status"`
}

func (s *Store) Locks() ([]Lock, error) {
	rows, err := s.DB.Query(`SELECT c.id, c.locked_by_run, COALESCE(r.status, 'missing') FROM cards c
		LEFT JOIN runs r ON r.id = c.locked_by_run WHERE c.locked_by_run IS NOT NULL ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lock
	for rows.Next() {
		var l Lock
		if err := rows.Scan(&l.CardID, &l.RunID, &l.RunStatus); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
