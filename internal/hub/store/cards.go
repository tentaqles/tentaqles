package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

const (
	AuthorHuman  = "human"
	AuthorAgent  = "agent"
	AuthorSystem = "system"
)

type Card struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Body             string   `json:"body"`
	Lane             string   `json:"lane"`
	Kind             string   `json:"kind"`
	Tags             []string `json:"tags"`
	ParentID         string   `json:"parent_id,omitempty"`
	ProjectDir       string   `json:"project_dir,omitempty"`
	ExternalProvider string   `json:"external_provider,omitempty"`
	ExternalID       string   `json:"external_id,omitempty"`
	ExternalURL      string   `json:"external_url,omitempty"`
	ExternalLane     string   `json:"external_lane,omitempty"`
	LockedByRun      string   `json:"locked_by_run,omitempty"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
	DoneAt           string   `json:"done_at,omitempty"`
}

func (c Card) HasTag(tag string) bool { return slices.Contains(c.Tags, tag) }

type Lane struct {
	Name     string `json:"name"`
	Position int    `json:"position"`
	Kind     string `json:"kind"`
}

type NewCard struct {
	Title      string   `json:"title"`
	Body       string   `json:"body,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Lane       string   `json:"lane,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	ParentID   string   `json:"parent_id,omitempty"`
	ProjectDir string   `json:"project_dir,omitempty"`
}

type CardFilter struct {
	Lane string `json:"lane,omitempty"`
	Kind string `json:"kind,omitempty"`
	Tag  string `json:"tag,omitempty"`
}

type CardUpdate struct {
	Title, Body, ProjectDir *string
	Tags                    *[]string
}

type MoveResult struct {
	Card     Card `json:"card"`
	From, To Lane
}

func (m MoveResult) EnteredActive() bool { return m.To.Kind == LaneActive && m.From.Kind != LaneActive }
func (m MoveResult) LeftActive() bool    { return m.From.Kind == LaneActive && m.To.Kind != LaneActive }

type Comment struct {
	ID         string `json:"id"`
	CardID     string `json:"card_id"`
	AuthorKind string `json:"author_kind"`
	Author     string `json:"author"`
	Body       string `json:"body"`
	RunID      string `json:"run_id,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type Attachment struct {
	ID        string `json:"id"`
	CardID    string `json:"card_id"`
	Path      string `json:"path"`
	Label     string `json:"label,omitempty"`
	CreatedAt string `json:"created_at"`
}

const cardCols = `c.id, c.title, c.body, c.lane, c.kind, c.tags, COALESCE(c.parent_id, ''), c.project_dir,
	COALESCE(c.external_provider, ''), COALESCE(c.external_id, ''), COALESCE(c.external_url, ''), COALESCE(c.external_lane, ''),
	COALESCE(c.locked_by_run, ''), c.created_at, c.updated_at, COALESCE(c.done_at, '')`

type scanner interface{ Scan(dest ...any) error }

func scanCard(r scanner) (Card, error) {
	var c Card
	var tags string
	err := r.Scan(&c.ID, &c.Title, &c.Body, &c.Lane, &c.Kind, &tags, &c.ParentID, &c.ProjectDir,
		&c.ExternalProvider, &c.ExternalID, &c.ExternalURL, &c.ExternalLane, &c.LockedByRun, &c.CreatedAt, &c.UpdatedAt, &c.DoneAt)
	if err != nil {
		return c, err
	}
	_ = json.Unmarshal([]byte(tags), &c.Tags)
	if c.Tags == nil {
		c.Tags = []string{}
	}
	return c, nil
}

func (s *Store) Lanes() ([]Lane, error) {
	rows, err := s.DB.Query(`SELECT name, position, kind FROM lanes ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lane
	for rows.Next() {
		var l Lane
		if err := rows.Scan(&l.Name, &l.Position, &l.Kind); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) Lane(name string) (Lane, error) {
	var l Lane
	err := s.DB.QueryRow(`SELECT name, position, kind FROM lanes WHERE name = ? COLLATE NOCASE`, strings.TrimSpace(name)).Scan(&l.Name, &l.Position, &l.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		ls, _ := s.Lanes()
		names := make([]string, len(ls))
		for i, x := range ls {
			names[i] = x.Name
		}
		return l, fmt.Errorf("lane %q: %w (lanes: %s)", name, ErrNotFound, strings.Join(names, ", "))
	}
	return l, err
}

func (s *Store) FirstLane(kind string) (Lane, error) {
	var l Lane
	err := s.DB.QueryRow(`SELECT name, position, kind FROM lanes WHERE kind = ? ORDER BY position LIMIT 1`, kind).Scan(&l.Name, &l.Position, &l.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return l, fmt.Errorf("no lane of kind %s: %w", kind, ErrNotFound)
	}
	return l, err
}

// cleanProjectDir keeps a workspace-relative, forward-slash path that cannot escape the workspace.
func cleanProjectDir(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || (len(p) > 1 && p[1] == ':') {
		return "", fmt.Errorf("project folder %q must be relative to the workspace root", p)
	}
	c := path.Clean(strings.ReplaceAll(p, `\`, "/"))
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("project folder %q leaves the workspace", p)
	}
	if c == "." {
		return "", nil
	}
	return c, nil
}

func dedupe(tags []string) []string {
	out := []string{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

func (s *Store) AddCard(nc NewCard, actor string) (Card, error) {
	title := strings.TrimSpace(nc.Title)
	if title == "" {
		return Card{}, errors.New("a card needs a title")
	}
	kind := nc.Kind
	if kind == "" {
		kind = "feature"
	}
	if !slices.Contains(CardKinds, kind) {
		return Card{}, fmt.Errorf("kind %q: use one of %s", kind, strings.Join(CardKinds, ", "))
	}
	var lane Lane
	var err error
	if nc.Lane == "" {
		lane, err = s.FirstLane(LaneBacklog)
	} else {
		lane, err = s.Lane(nc.Lane)
	}
	if err != nil {
		return Card{}, err
	}
	if nc.ParentID != "" {
		if _, err := s.Card(nc.ParentID); err != nil {
			return Card{}, fmt.Errorf("parent %s: %w", nc.ParentID, err)
		}
	}
	pd, err := cleanProjectDir(nc.ProjectDir)
	if err != nil {
		return Card{}, err
	}
	tags, _ := json.Marshal(dedupe(nc.Tags))
	now := Now()
	id := NewID("c")
	if _, err := s.DB.Exec(`INSERT INTO cards(id, title, body, lane, kind, tags, parent_id, project_dir, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, s.Mask(title), s.Mask(nc.Body), lane.Name, kind, string(tags),
		nullStr(nc.ParentID), pd, now, now); err != nil {
		return Card{}, err
	}
	// The row is committed; the activity mirror is best-effort.
	_ = s.Log(activity.Entry{Actor: actor, Verb: activity.VerbCardCreate, CardID: id, Data: map[string]any{"lane": lane.Name, "kind": kind}})
	s.Publish(Change{Kind: "card", CardID: id})
	return s.Card(id)
}

func (s *Store) Card(id string) (Card, error) {
	c, err := scanCard(s.DB.QueryRow(`SELECT `+cardCols+` FROM cards c WHERE c.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("card %s: %w", id, ErrNotFound)
	}
	return c, err
}

func (s *Store) ListCards(f CardFilter) ([]Card, error) {
	q := `SELECT ` + cardCols + ` FROM cards c JOIN lanes l ON l.name = c.lane COLLATE NOCASE WHERE 1 = 1`
	var args []any
	if f.Lane != "" {
		q += ` AND c.lane = ? COLLATE NOCASE`
		args = append(args, f.Lane)
	}
	if f.Kind != "" {
		q += ` AND c.kind = ?`
		args = append(args, f.Kind)
	}
	if f.Tag != "" {
		q += ` AND EXISTS (SELECT 1 FROM json_each(c.tags) WHERE value = ?)`
		args = append(args, f.Tag)
	}
	q += ` ORDER BY l.position, c.updated_at DESC, c.id`
	return s.cards(q, args...)
}

func (s *Store) cards(q string, args ...any) ([]Card, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Children(parentID string) ([]Card, error) {
	return s.cards(`SELECT `+cardCols+` FROM cards c WHERE c.parent_id = ? ORDER BY c.created_at`, parentID)
}

func (s *Store) UpdateCard(id string, u CardUpdate, actor string) (Card, error) {
	c, err := s.Card(id)
	if err != nil {
		return c, err
	}
	var fields []any
	if u.Title != nil {
		if strings.TrimSpace(*u.Title) == "" {
			return c, errors.New("a card needs a title")
		}
		c.Title = s.Mask(strings.TrimSpace(*u.Title))
		fields = append(fields, "title")
	}
	if u.Body != nil {
		c.Body = s.Mask(*u.Body)
		fields = append(fields, "body")
	}
	if u.ProjectDir != nil {
		pd, err := cleanProjectDir(*u.ProjectDir)
		if err != nil {
			return c, err
		}
		c.ProjectDir = pd
		fields = append(fields, "project_dir")
	}
	if u.Tags != nil {
		c.Tags = dedupe(*u.Tags)
		fields = append(fields, "tags")
	}
	tags, _ := json.Marshal(c.Tags)
	if _, err := s.DB.Exec(`UPDATE cards SET title = ?, body = ?, project_dir = ?, tags = ?, updated_at = ? WHERE id = ?`,
		c.Title, c.Body, c.ProjectDir, string(tags), Now(), id); err != nil {
		return c, err
	}
	_ = s.Log(activity.Entry{Actor: actor, Verb: activity.VerbCardUpdate, CardID: id, Data: map[string]any{"fields": fields}})
	s.Publish(Change{Kind: "card", CardID: id})
	return s.Card(id)
}

// MoveCard moves a card between lanes. The row write is authoritative; the activity entry and parent
// completion are best-effort. Parent completion moves an unlocked parent to review; the caller sees
// only the child's transition.
func (s *Store) MoveCard(id, lane, actor string) (MoveResult, error) {
	c, err := s.Card(id)
	if err != nil {
		return MoveResult{}, err
	}
	from, err := s.Lane(c.Lane)
	if err != nil {
		return MoveResult{}, err
	}
	to, err := s.Lane(lane)
	if err != nil {
		return MoveResult{}, err
	}
	now := Now()
	var doneAt any
	if to.Kind == LaneDone {
		doneAt = now
	}
	if _, err := s.DB.Exec(`UPDATE cards SET lane = ?, done_at = ?, updated_at = ? WHERE id = ?`, to.Name, doneAt, now, id); err != nil {
		return MoveResult{}, err
	}
	_ = s.Log(activity.Entry{Actor: actor, Verb: activity.VerbCardMove, CardID: id,
		Data: map[string]any{"from": from.Name, "to": to.Name, "kind": c.Kind}})
	s.Publish(Change{Kind: "card", CardID: id})
	moved, err := s.Card(id)
	if err != nil {
		return MoveResult{}, err
	}
	res := MoveResult{Card: moved, From: from, To: to}
	if to.Kind == LaneDone && c.ParentID != "" {
		_ = s.completeParent(c.ParentID) // best-effort: the child's move is already committed
	}
	return res, nil
}

// completeParent moves the parent to review (never done: the parent's own review is the human's)
// once every child is in a done lane.
func (s *Store) completeParent(parentID string) error {
	kids, err := s.Children(parentID)
	if err != nil || len(kids) == 0 {
		return err
	}
	for _, k := range kids {
		l, err := s.Lane(k.Lane)
		if err != nil {
			return err
		}
		if l.Kind != LaneDone {
			return nil
		}
	}
	p, err := s.Card(parentID)
	if err != nil {
		return err
	}
	pl, err := s.Lane(p.Lane)
	if err != nil {
		return err
	}
	if pl.Kind == LaneDone || pl.Kind == LaneReview {
		return nil
	}
	if p.LockedByRun != "" {
		// A run owns the parent: do not move it under that run, only leave a note.
		_, err := s.AddComment(parentID, AuthorSystem, "hub",
			fmt.Sprintf("All %d child cards are done; this card moves to review when its run ends.", len(kids)), "")
		return err
	}
	rev, err := s.FirstLane(LaneReview)
	if err != nil {
		return err
	}
	if _, err := s.MoveCard(parentID, rev.Name, "hub"); err != nil {
		return err
	}
	_, err = s.AddComment(parentID, AuthorSystem, "hub",
		fmt.Sprintf("All %d child cards are done; this card moves to review for your own review.", len(kids)), "")
	return err
}

func (s *Store) AddComment(cardID, authorKind, author, body, runID string) (Comment, error) {
	if !slices.Contains([]string{AuthorHuman, AuthorAgent, AuthorSystem}, authorKind) {
		return Comment{}, fmt.Errorf("author kind %q: use human, agent or system", authorKind)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, errors.New("a comment needs text")
	}
	if _, err := s.Card(cardID); err != nil {
		return Comment{}, err
	}
	masked := s.Mask(body)
	if len(masked) > 20000 {
		masked = masked[:20000] + "\n[truncated]"
	}
	c := Comment{ID: NewID("m"), CardID: cardID, AuthorKind: authorKind, Author: s.Mask(author), Body: masked, RunID: runID, CreatedAt: Now()}
	if _, err := s.DB.Exec(`INSERT INTO comments(id, card_id, author_kind, author, body, run_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.CardID, c.AuthorKind, c.Author, c.Body, nullStr(runID), c.CreatedAt); err != nil {
		return Comment{}, err
	}
	if _, err := s.DB.Exec(`UPDATE cards SET updated_at = ? WHERE id = ?`, c.CreatedAt, cardID); err != nil {
		return Comment{}, err
	}
	_ = s.Log(activity.Entry{Actor: author, Verb: activity.VerbCommentAdd, CardID: cardID, RunID: runID,
		Data: map[string]any{"author_kind": authorKind}})
	s.Publish(Change{Kind: "comment", CardID: cardID, RunID: runID})
	return c, nil
}

func (s *Store) Comments(cardID string) ([]Comment, error) {
	rows, err := s.DB.Query(`SELECT id, card_id, author_kind, author, body, COALESCE(run_id, ''), created_at
		FROM comments WHERE card_id = ? ORDER BY created_at, rowid`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.CardID, &c.AuthorKind, &c.Author, &c.Body, &c.RunID, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddAttachment stores a reference, never a copy.
func (s *Store) AddAttachment(cardID, p, label string) (Attachment, error) {
	if strings.TrimSpace(p) == "" {
		return Attachment{}, errors.New("an attachment needs a path")
	}
	if _, err := s.Card(cardID); err != nil {
		return Attachment{}, err
	}
	a := Attachment{ID: NewID("a"), CardID: cardID, Path: strings.TrimSpace(p), Label: s.Mask(label), CreatedAt: Now()}
	_, err := s.DB.Exec(`INSERT INTO attachments(id, card_id, path, label, created_at) VALUES (?, ?, ?, ?, ?)`, a.ID, a.CardID, a.Path, a.Label, a.CreatedAt)
	if err == nil {
		s.Publish(Change{Kind: "card", CardID: cardID})
	}
	return a, err
}

func (s *Store) Attachments(cardID string) ([]Attachment, error) {
	rows, err := s.DB.Query(`SELECT id, card_id, path, label, created_at FROM attachments WHERE card_id = ? ORDER BY created_at`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.CardID, &a.Path, &a.Label, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Checkout is the one atomic statement of spec §8.1: zero rows means another run owns the card.
func (s *Store) Checkout(cardID, runID string) (bool, error) {
	res, err := s.DB.Exec(`UPDATE cards SET locked_by_run = ?, updated_at = ? WHERE id = ? AND locked_by_run IS NULL`, runID, Now(), cardID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if n == 1 {
		s.Publish(Change{Kind: "card", CardID: cardID, RunID: runID})
	}
	return n == 1, err
}

func (s *Store) Release(cardID, runID string) error {
	_, err := s.DB.Exec(`UPDATE cards SET locked_by_run = NULL, updated_at = ? WHERE id = ? AND locked_by_run = ?`, Now(), cardID, runID)
	if err == nil {
		s.Publish(Change{Kind: "card", CardID: cardID})
	}
	return err
}

func (s *Store) ForceRelease(cardID string) error {
	_, err := s.DB.Exec(`UPDATE cards SET locked_by_run = NULL, updated_at = ? WHERE id = ?`, Now(), cardID)
	return err
}
