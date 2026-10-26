// Package runner drives hub runs: it queues work per card, checks the card out
// atomically, launches adapter rounds with the workspace's environment, reads the
// gate state after every round, classifies the outcome, enforces budgets, and
// carries the human's answers back to the gate. It is the only writer of runs.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

type Config struct {
	Workspace  string
	Root       string // the workspace root folder; card project folders are relative to it
	Store      *store.Store
	Gate       gate.API
	Adapters   map[string]adapter.Adapter
	Adapter    string          // default adapter; "" → claude
	Env        func() []string // the workspace's environment plan; nil → os.Environ
	Notify     func(ctx context.Context, title, body string) error
	Actor      string // "hub:<os user>"; "" → gate.Actor()
	Now        func() time.Time
	Tick       time.Duration // 0 → 2 s
	RetryDelay time.Duration // 0 → 2 min
	PollEvery  time.Duration // waiting runs re-read the gate this often; 0 → 15 s
	GateURL    string        // the gate's own live view, linked while a run is active
}

type flight struct {
	cancel   context.CancelFunc
	done     chan struct{}
	explicit atomic.Bool // set by Cancel; a parent context ending (hub shutdown) leaves it false
}

type Service struct {
	cfg Config
	st  *store.Store

	mu         sync.Mutex // flights, cancelling and lastPoll; also held across the queued → running transition
	flights    map[string]*flight
	cancelling map[string]bool // runs Cancel is ending: startRound never starts them
	lastPoll   map[string]time.Time
	enqMu      sync.Mutex // serializes Enqueue and Cancel's final transition within this process
	tickMu     sync.Mutex // serializes Tick so concurrent ticks cannot exceed the concurrency limit
	wg         sync.WaitGroup

	// OnApprove runs after the human approved a result (Task 11: the static review export).
	OnApprove func(card store.Card, run store.Run)
}

func New(cfg Config) (*Service, error) {
	if cfg.Store == nil {
		return nil, errors.New("runner: no store")
	}
	if cfg.Gate == nil {
		cfg.Gate = gate.Unavailable{Err: errors.New("no build gate configured")}
	}
	if cfg.Adapter == "" {
		cfg.Adapter = store.AdapterClaude
	}
	if cfg.Env == nil {
		cfg.Env = os.Environ
	}
	if cfg.Actor == "" {
		cfg.Actor = gate.Actor()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Tick <= 0 {
		cfg.Tick = 2 * time.Second
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 2 * time.Minute
	}
	if cfg.PollEvery <= 0 {
		cfg.PollEvery = 15 * time.Second
	}
	return &Service{cfg: cfg, st: cfg.Store, flights: map[string]*flight{}, cancelling: map[string]bool{}, lastPoll: map[string]time.Time{}}, nil
}

func (s *Service) Store() *store.Store { return s.st }
func (s *Service) Gate() gate.API      { return s.cfg.Gate }
func (s *Service) Workspace() string   { return s.cfg.Workspace }
func (s *Service) Root() string        { return s.cfg.Root }
func (s *Service) GateURL() string     { return s.cfg.GateURL }

func (s *Service) human() string { return strings.TrimPrefix(s.cfg.Actor, "hub:") }
func (s *Service) stamp() string { return s.cfg.Now().UTC().Format(time.RFC3339) }

// RunDir is where a round runs: the worktree once the hand-off named it, else the card's project folder.
func (s *Service) RunDir(r store.Run) string {
	if r.Worktree != "" {
		return r.Worktree
	}
	if r.ProjectDir == "" {
		return s.cfg.Root
	}
	return filepath.Join(s.cfg.Root, filepath.FromSlash(r.ProjectDir))
}

func (s *Service) adapterFor(c store.Card) string {
	if c.HasTag("adapter:process") {
		if _, ok := s.cfg.Adapters[store.AdapterProcess]; ok {
			return store.AdapterProcess
		}
	}
	return s.cfg.Adapter
}

type agentKey struct{}

// WithAgent marks a request as coming from an agent session (spec §15).
func WithAgent(ctx context.Context) context.Context { return context.WithValue(ctx, agentKey{}, true) }

func IsAgent(ctx context.Context) bool {
	v, _ := ctx.Value(agentKey{}).(bool)
	return v
}

var (
	ErrHumanOnly = errors.New("human-only: run it in your own terminal or on the hub page (agent sessions may add cards and comment, nothing else)")
	ErrLocked    = errors.New("the card is locked by another run")
)

type RunActiveError struct{ Run store.Run }

func (e *RunActiveError) Error() string {
	return fmt.Sprintf("card %s has a %s run %s; pass --cancel-run (or confirm on the page) to cancel it and move the card",
		e.Run.CardID, e.Run.Status, e.Run.ID)
}

type CardView struct {
	store.Card
	Waiting   int     `json:"waiting"`
	Running   bool    `json:"running"`
	RunID     string  `json:"run_id,omitempty"`
	RunStatus string  `json:"run_status,omitempty"`
	CostUSD   float64 `json:"cost_usd"`
}

type CardDetail struct {
	CardView
	Comments    []store.Comment    `json:"comments"`
	Decisions   []store.Decision   `json:"decisions"`
	Runs        []store.Run        `json:"runs"`
	Attachments []store.Attachment `json:"attachments"`
	Children    []store.Card       `json:"children"`
	Parent      *store.Card        `json:"parent,omitempty"`
}

type WaitingItem struct {
	Workspace string         `json:"workspace"`
	Card      store.Card     `json:"card"`
	Decision  store.Decision `json:"decision"`
}

type MoveOutcome struct {
	Card      store.Card `json:"card"`
	Enqueued  *store.Run `json:"enqueued,omitempty"`
	Cancelled *store.Run `json:"cancelled,omitempty"`
}

type AnswerOutcome struct {
	Decision        store.Decision `json:"decision"`
	AlreadyAnswered bool           `json:"already_answered"`
	Resumed         *store.Run     `json:"resumed,omitempty"`
	Message         string         `json:"message"`
}

// Ops is what the CLI needs, served by *Service directly or by web.Client over a running hub.
type Ops interface {
	Add(ctx context.Context, nc store.NewCard, budget *store.Budget) (store.Card, error)
	List(ctx context.Context, f store.CardFilter, waitingOnly bool) ([]CardView, error)
	Show(ctx context.Context, id string) (CardDetail, error)
	Move(ctx context.Context, id, lane string, cancelRun bool) (MoveOutcome, error)
	Run(ctx context.Context, id string) (store.Run, error)
	Answer(ctx context.Context, cardID, decisionID, choice, note string) (AnswerOutcome, error)
	Cancel(ctx context.Context, runID string) (store.Run, error)
	Comment(ctx context.Context, cardID, body string) (store.Comment, error)
	Waiting(ctx context.Context) ([]WaitingItem, error)
}

func (s *Service) Add(ctx context.Context, nc store.NewCard, budget *store.Budget) (store.Card, error) {
	if budget != nil {
		if IsAgent(ctx) {
			return store.Card{}, ErrHumanOnly
		}
		nc.Body = store.WithBudgetFrontMatter(nc.Body, *budget)
	}
	agent := IsAgent(ctx)
	lane, err := s.addLane(nc.Lane)
	if err != nil {
		return store.Card{}, err
	}
	// An agent only files planning work: no budget (by argument or front matter), no card
	// created straight into an active, review or done lane (spec §15).
	if agent {
		if _, ok := store.FrontMatter(nc.Body)["budget"]; ok {
			return store.Card{}, ErrHumanOnly
		}
		if !agentLane(lane.Kind) {
			return store.Card{}, ErrHumanOnly
		}
	}
	actor := s.human()
	if agent {
		actor = "agent"
	}
	c, err := s.st.AddCard(nc, actor)
	if err != nil {
		return c, err
	}
	// Created straight into an active lane is entering it (spec 8.1).
	if !agent && lane.Kind == store.LaneActive && c.HasTag(store.TagAgentReady) {
		if _, _, err := s.Enqueue(ctx, c.ID, "added to "+lane.Name); err != nil && !errors.Is(err, ErrLocked) {
			return c, err
		}
		return s.st.Card(c.ID)
	}
	return c, nil
}

// addLane is the lane a new card lands in: the named one, else the first backlog lane.
func (s *Service) addLane(name string) (store.Lane, error) {
	if strings.TrimSpace(name) == "" {
		return s.st.FirstLane(store.LaneBacklog)
	}
	return s.st.Lane(name)
}

func (s *Service) view(c store.Card) (CardView, error) {
	n, err := s.st.PendingCount(c.ID)
	if err != nil {
		return CardView{Card: c}, err
	}
	runs, err := s.st.RunsForCard(c.ID)
	if err != nil {
		return CardView{Card: c}, err
	}
	return NewCardView(c, runs, n), nil
}

// NewCardView builds a card's view from its runs (newest first, as RunsForCard and RunsForCards
// return them) and its pending decision count. The active run is the newest active one, as
// store.ActiveRun picks it.
func NewCardView(c store.Card, runs []store.Run, waiting int) CardView {
	v := CardView{Card: c, Waiting: waiting}
	active := false
	for _, r := range runs {
		v.CostUSD += r.CostUSD
		if !active && r.Active() {
			active = true
			v.RunID, v.RunStatus, v.Running = r.ID, r.Status, r.Status == store.StatusRunning
		}
	}
	return v
}

// List reads the cards, then their runs and pending counts in one batched query each.
func (s *Service) List(ctx context.Context, f store.CardFilter, waitingOnly bool) ([]CardView, error) {
	cards, err := s.st.ListCards(f)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(cards))
	for i, c := range cards {
		ids[i] = c.ID
	}
	runs, err := s.st.RunsForCards(ids)
	if err != nil {
		return nil, err
	}
	waiting, err := s.st.PendingCounts(ids)
	if err != nil {
		return nil, err
	}
	out := []CardView{}
	for _, c := range cards {
		v := NewCardView(c, runs[c.ID], waiting[c.ID])
		if waitingOnly && v.Waiting == 0 {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) Show(ctx context.Context, id string) (CardDetail, error) {
	c, err := s.st.Card(id)
	if err != nil {
		return CardDetail{}, err
	}
	v, err := s.view(c)
	if err != nil {
		return CardDetail{}, err
	}
	d := CardDetail{CardView: v}
	if d.Comments, err = s.st.Comments(id); err != nil {
		return d, err
	}
	if d.Decisions, err = s.st.DecisionsForCard(id); err != nil {
		return d, err
	}
	if d.Runs, err = s.st.RunsForCard(id); err != nil {
		return d, err
	}
	if d.Attachments, err = s.st.Attachments(id); err != nil {
		return d, err
	}
	if d.Children, err = s.st.Children(id); err != nil {
		return d, err
	}
	if c.ParentID != "" {
		if p, err := s.st.Card(c.ParentID); err == nil {
			d.Parent = &p
		}
	}
	return d, nil
}

func (s *Service) Comment(ctx context.Context, cardID, body string) (store.Comment, error) {
	if IsAgent(ctx) {
		return s.st.AddComment(cardID, store.AuthorAgent, "agent", body, "")
	}
	return s.st.AddComment(cardID, store.AuthorHuman, s.human(), body, "")
}

func (s *Service) Waiting(ctx context.Context) ([]WaitingItem, error) {
	ds, err := s.st.PendingDecisions()
	if err != nil {
		return nil, err
	}
	out := []WaitingItem{}
	for _, d := range ds {
		c, err := s.st.Card(d.CardID)
		if err != nil {
			continue
		}
		out = append(out, WaitingItem{Workspace: s.cfg.Workspace, Card: c, Decision: d})
	}
	return out, nil
}

// Run enqueues (or resumes) a run for the card now (tq hub run).
func (s *Service) Run(ctx context.Context, id string) (store.Run, error) {
	if IsAgent(ctx) {
		return store.Run{}, ErrHumanOnly
	}
	r, _, err := s.Enqueue(ctx, id, "requested")
	return r, err
}
