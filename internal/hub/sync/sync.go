// Package sync is the hub's board sync (spec B): the tracker Adapter interface, the item types
// adapters return, and the adapter registry. Adapters live in sub-packages
// (internal/hub/sync/<provider>) and register themselves from init; credentials come from
// internal/hub/sync/cred. Import it as hubsync to keep the standard library's sync usable.
package sync

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	gosync "sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
	"github.com/tentaqles/tentaqles/internal/manifest"
)

// Status categories: the hub lane kind a tracker status maps to by default.
const (
	CategoryBacklog = "backlog"
	CategoryTodo    = "todo"
	CategoryActive  = "active"
	CategoryReview  = "review"
	CategoryDone    = "done"
)

var Categories = []string{CategoryBacklog, CategoryTodo, CategoryActive, CategoryReview, CategoryDone}

func ValidCategory(c string) bool { return slices.Contains(Categories, c) }

// MaxComments caps Item.Comments.
const MaxComments = 20

// Item is one tracker work item as an adapter reads it.
type Item struct {
	ID, Key, URL, Title, Body, Status, StatusCategory string // StatusCategory: backlog|todo|active|review|done
	Assignee, ParentID, Rev                           string
	Labels                                            []string
	Acceptance                                        string       // ADO AcceptanceCriteria, Jira custom field; else ""
	Comments                                          []Comment    // newest first, capped at MaxComments
	Attachments                                       []Attachment // name + url only, never downloaded
	UpdatedAt                                         time.Time
}

type Comment struct {
	ID, Author, Body string
	CreatedAt        time.Time
}

type Attachment struct {
	Name, URL string
}

// Status is one tracker status, for lane mapping.
type Status struct {
	Name     string
	Category string // one of Categories
}

// NewItem is a work item to create. ParentID set makes it a child of that item.
type NewItem struct {
	Title, Body string
	Acceptance  string
	ParentID    string
	Type        string // ado work item type / jira issue type; "" = the adapter's default
	Status      string // "" = the tracker's default
	Labels      []string
}

// Adapter is one tracker (spec B §7).
type Adapter interface {
	Name() string
	Check(ctx context.Context) (account string, err error)                       // verify auth; used by doctor and onboarding
	Statuses(ctx context.Context) ([]Status, error)                              // name + category, for lane mapping
	ListChanged(ctx context.Context, since time.Time) ([]Item, time.Time, error) // assigned/mirrored items changed since; new watermark
	Get(ctx context.Context, id string) (Item, error)
	Move(ctx context.Context, id, status string) error      // jira: resolves the transition
	Comment(ctx context.Context, id, markdown string) error // adapter converts (jira ADF, ado HTML)
	Create(ctx context.Context, in NewItem) (Item, error)   // in.ParentID set -> child
	Update(ctx context.Context, id string, title, body *string) error
	SetLabels(ctx context.Context, id string, add, remove []string) error
}

var (
	// ErrNoAdapter: no adapter is registered for the provider.
	ErrNoAdapter = errors.New("no sync adapter")
	// ErrNotFound: the tracker has no such item (adapters wrap it).
	ErrNotFound = errors.New("tracker item not found")
	// ErrUnsupported: the tracker cannot do this (adapters wrap it).
	ErrUnsupported = errors.New("not supported by this tracker")
)

// RateLimitError is a 429 or an exhausted X-RateLimit budget. RetryAfter is 0 when the tracker
// did not say; the engine then backs off on its own (3 attempts per tick).
type RateLimitError struct {
	Provider   string
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("%s: rate limited, retry after %s", e.Provider, e.RetryAfter)
	}
	return fmt.Sprintf("%s: rate limited", e.Provider)
}

// Config is what a Factory gets to build an adapter.
type Config struct {
	Workspace  string
	Provider   string
	Scope      map[string]string // tracker.scope from the manifest; unknown keys are kept
	ScopeKey   string            // manifest.Tracker.ScopeKey(): the store's scope column
	AssignedTo string            // me | anyone
	Auth       cred.Source       // the manifest's preferred source ("" = cli)
	EnvVar     string            // the manifest's tracker.env
	Lanes      map[string]string // the manifest's tracker.lanes overrides (tracker status -> hub lane)
	Transport  cred.Transport    // the resolved credential; nil until resolved
	Env        []string          // the workspace identity environment, for adapters that run a CLI
	BaseURL    string            // "" = the provider's public API; tests point it at an httptest server
}

// ConfigFromManifest builds a Config from a manifest tracker block (nil gives a zero Config).
// The scope and lanes maps are copied.
func ConfigFromManifest(workspace string, t *manifest.Tracker) Config {
	if t == nil {
		return Config{Workspace: workspace}
	}
	return Config{Workspace: workspace, Provider: t.Provider, Scope: maps.Clone(t.Scope), ScopeKey: t.ScopeKey(),
		AssignedTo: t.AssignedToOrDefault(), Auth: cred.Source(t.AuthOrDefault()), EnvVar: t.Env, Lanes: maps.Clone(t.Lanes)}
}

// CredRequest is the credential request for this config.
func (c Config) CredRequest() cred.Request {
	return cred.Request{Workspace: c.Workspace, Provider: c.Provider, Preferred: c.Auth, EnvVar: c.EnvVar, Env: c.Env}
}

// Factory builds an adapter.
type Factory func(cfg Config) (Adapter, error)

var (
	regMu     gosync.RWMutex
	factories = map[string]Factory{}
)

// Register adds a provider's factory, normally from the adapter package's init. It panics on an
// empty name, a nil factory or a second registration of a name.
func Register(name string, f Factory) {
	name = strings.TrimSpace(name)
	if name == "" || f == nil {
		panic("sync: Register needs a name and a factory")
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := factories[name]; dup {
		panic("sync: Register called twice for " + name)
	}
	factories[name] = f
}

// Lookup is the factory registered for name.
func Lookup(name string) (Factory, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	f, ok := factories[name]
	return f, ok
}

// Registered lists the registered providers, sorted.
func Registered() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	return slices.Sorted(maps.Keys(factories))
}

// New builds the adapter for cfg.Provider.
func New(cfg Config) (Adapter, error) {
	f, ok := Lookup(cfg.Provider)
	if !ok {
		return nil, fmt.Errorf("%w for %q (registered: %s)", ErrNoAdapter, cfg.Provider, strings.Join(Registered(), ", "))
	}
	return f(cfg)
}

func unregister(name string) {
	regMu.Lock()
	defer regMu.Unlock()
	delete(factories, name)
}
