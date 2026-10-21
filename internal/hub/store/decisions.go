package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

const (
	KindPlanApproval = "plan-approval"
	KindResult       = "result"
	KindJudge        = "judge"
	KindQuestion     = "question"
	KindPermission   = "permission" // the hub's own kind: permission asks and over-budget questions; no gate record

	DecisionPending  = "pending"
	DecisionAnswered = "answered"
)

type Decision struct {
	ID         string         `json:"id"`
	CardID     string         `json:"card_id"`
	RunID      string         `json:"run_id,omitempty"`
	Slug       string         `json:"slug,omitempty"`
	Kind       string         `json:"kind"`
	Checkpoint string         `json:"checkpoint,omitempty"`
	Reviewer   string         `json:"reviewer,omitempty"`
	Text       string         `json:"text"`
	Options    []string       `json:"options"`
	Status     string         `json:"status"`
	Choice     string         `json:"choice,omitempty"`
	Note       string         `json:"note,omitempty"`
	Actor      string         `json:"actor,omitempty"`
	AskedAt    string         `json:"asked_at,omitempty"`
	AnsweredAt string         `json:"answered_at,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

func (d Decision) HubOwned() bool { return d.Kind == KindPermission }

type MirrorResult struct{ Inserted, NowAnswered bool }

// ActorKind buckets an answer's actor for the activity log.
func ActorKind(actor string) string {
	switch {
	case strings.HasPrefix(actor, "hub:"):
		return "hub"
	case actor == "terminal":
		return "terminal"
	}
	return "other"
}

const decisionCols = `id, card_id, COALESCE(run_id, ''), COALESCE(slug, ''), kind, COALESCE(checkpoint, ''), COALESCE(reviewer, ''),
	text, options, status, COALESCE(choice, ''), COALESCE(note, ''), COALESCE(actor, ''), COALESCE(asked_at, ''), COALESCE(answered_at, ''), data`

func scanDecision(r scanner) (Decision, error) {
	var d Decision
	var opts, data string
	err := r.Scan(&d.ID, &d.CardID, &d.RunID, &d.Slug, &d.Kind, &d.Checkpoint, &d.Reviewer, &d.Text, &opts, &d.Status,
		&d.Choice, &d.Note, &d.Actor, &d.AskedAt, &d.AnsweredAt, &data)
	if err != nil {
		return d, err
	}
	_ = json.Unmarshal([]byte(opts), &d.Options)
	_ = json.Unmarshal([]byte(data), &d.Data)
	return d, nil
}

func (s *Store) Decision(id string) (Decision, error) {
	d, err := scanDecision(s.DB.QueryRow(`SELECT `+decisionCols+` FROM decisions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, fmt.Errorf("decision %s: %w", id, ErrNotFound)
	}
	return d, err
}

// UpsertDecision mirrors one gate record keyed by the gate's id. A pending row flips to answered once;
// an answered row never changes again.
func (s *Store) UpsertDecision(d Decision) (MirrorResult, error) {
	cur, err := s.Decision(d.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return MirrorResult{}, err
	}
	if errors.Is(err, ErrNotFound) {
		if d.Status == "" {
			d.Status = DecisionPending
		}
		if d.AskedAt == "" {
			d.AskedAt = Now()
		}
		if d.Options == nil {
			d.Options = []string{}
		}
		if d.Data == nil {
			d.Data = map[string]any{}
		}
		if _, err := s.DB.Exec(`INSERT INTO decisions(id, card_id, run_id, slug, kind, checkpoint, reviewer, text, options, status,
			choice, note, actor, asked_at, answered_at, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.ID, d.CardID, nullStr(d.RunID), nullStr(d.Slug), d.Kind, nullStr(d.Checkpoint), nullStr(d.Reviewer), s.Mask(d.Text),
			jsonText(d.Options), d.Status, nullStr(s.Mask(d.Choice)), nullStr(s.Mask(d.Note)), nullStr(d.Actor), d.AskedAt, nullStr(d.AnsweredAt),
			s.Mask(jsonText(d.Data))); err != nil {
			return MirrorResult{}, err
		}
		if err := s.Log(activity.Entry{Actor: "hub", Verb: activity.VerbDecisionAsk, CardID: d.CardID, RunID: d.RunID,
			Data: map[string]any{"decision": d.ID, "kind": d.Kind}}); err != nil {
			return MirrorResult{}, err
		}
		res := MirrorResult{Inserted: true}
		if d.Status == DecisionAnswered {
			if err := s.logAnswer(d); err != nil {
				return res, err
			}
			res.NowAnswered = true
		}
		s.Publish(Change{Kind: "decision", CardID: d.CardID, RunID: d.RunID})
		return res, nil
	}
	if cur.Status == DecisionAnswered || d.Status != DecisionAnswered {
		return MirrorResult{}, nil
	}
	if d.AnsweredAt == "" {
		d.AnsweredAt = Now()
	}
	if _, err := s.DB.Exec(`UPDATE decisions SET status = 'answered', choice = ?, note = ?, actor = ?, answered_at = ? WHERE id = ? AND status = 'pending'`,
		s.Mask(d.Choice), s.Mask(d.Note), d.Actor, d.AnsweredAt, d.ID); err != nil {
		return MirrorResult{}, err
	}
	cur.Choice, cur.Actor = s.Mask(d.Choice), d.Actor
	if err := s.logAnswer(cur); err != nil {
		return MirrorResult{}, err
	}
	s.Publish(Change{Kind: "decision", CardID: cur.CardID, RunID: cur.RunID})
	return MirrorResult{NowAnswered: true}, nil
}

func (s *Store) logAnswer(d Decision) error {
	return s.Log(activity.Entry{Actor: d.Actor, Verb: activity.VerbDecisionAnswer, CardID: d.CardID, RunID: d.RunID,
		Data: map[string]any{"decision": d.ID, "kind": d.Kind, "choice": d.Choice, "actor_kind": ActorKind(d.Actor)}})
}

// AskHub records one of the hub's own decisions (permission and over-budget).
func (s *Store) AskHub(cardID, runID, slug, text string, options []string, data map[string]any) (Decision, error) {
	d := Decision{ID: NewID("d"), CardID: cardID, RunID: runID, Slug: slug, Kind: KindPermission, Text: text,
		Options: options, Status: DecisionPending, AskedAt: Now(), Data: data}
	if _, err := s.UpsertDecision(d); err != nil {
		return Decision{}, err
	}
	return s.Decision(d.ID)
}

func (s *Store) AnswerHub(id, choice, note, actor string) (Decision, error) {
	d, err := s.Decision(id)
	if err != nil {
		return d, err
	}
	if !d.HubOwned() {
		return d, fmt.Errorf("decision %s belongs to the gate: answer it with tq hub answer", id)
	}
	if d.Status != DecisionPending {
		return d, fmt.Errorf("decision %s is already answered (%s by %s)", id, d.Choice, d.Actor)
	}
	if !slices.Contains(d.Options, choice) {
		return d, fmt.Errorf("choice must be one of %s", strings.Join(d.Options, ", "))
	}
	if _, err := s.UpsertDecision(Decision{ID: id, Status: DecisionAnswered, Choice: choice, Note: note, Actor: actor, AnsweredAt: Now()}); err != nil {
		return d, err
	}
	return s.Decision(id)
}

func (s *Store) decisions(q string, args ...any) ([]Decision, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DecisionsForCard(cardID string) ([]Decision, error) {
	return s.decisions(`SELECT `+decisionCols+` FROM decisions WHERE card_id = ? ORDER BY asked_at DESC, rowid DESC`, cardID)
}

func (s *Store) PendingDecisions() ([]Decision, error) {
	return s.decisions(`SELECT ` + decisionCols + ` FROM decisions WHERE status = 'pending' ORDER BY asked_at DESC, rowid DESC`)
}

func (s *Store) PendingCount(cardID string) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM decisions WHERE card_id = ? AND status = 'pending'`, cardID).Scan(&n)
	return n, err
}
