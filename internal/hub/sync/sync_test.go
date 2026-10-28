package sync

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
	"github.com/tentaqles/tentaqles/internal/manifest"
)

// fake is a do-nothing Adapter: it proves the interface is implementable as the spec writes it.
type fake struct{ cfg Config }

func (f *fake) Name() string                              { return f.cfg.Provider }
func (f *fake) Check(ctx context.Context) (string, error) { return "tester", nil }
func (f *fake) Statuses(ctx context.Context) ([]Status, error) {
	return []Status{{Name: "To Do", Category: CategoryTodo}}, nil
}
func (f *fake) ListChanged(ctx context.Context, since time.Time) ([]Item, time.Time, error) {
	return nil, since, nil
}
func (f *fake) Get(ctx context.Context, id string) (Item, error)       { return Item{ID: id}, nil }
func (f *fake) Move(ctx context.Context, id, status string) error      { return nil }
func (f *fake) Comment(ctx context.Context, id, markdown string) error { return nil }
func (f *fake) Create(ctx context.Context, in NewItem) (Item, error) {
	return Item{Title: in.Title}, nil
}
func (f *fake) Update(ctx context.Context, id string, title, body *string) error {
	return nil
}
func (f *fake) SetLabels(ctx context.Context, id string, add, remove []string) error { return nil }

var _ Adapter = (*fake)(nil)

func TestRegistry(t *testing.T) {
	t.Cleanup(func() { unregister("acme-fake") })
	if _, ok := Lookup("acme-fake"); ok {
		t.Fatal("found before register")
	}
	Register("acme-fake", func(cfg Config) (Adapter, error) { return &fake{cfg: cfg}, nil })
	if !slices.Contains(Registered(), "acme-fake") {
		t.Fatalf("%v", Registered())
	}
	a, err := New(Config{Workspace: "acme", Provider: "acme-fake"})
	if err != nil || a.Name() != "acme-fake" {
		t.Fatal(a, err)
	}
	if _, err := New(Config{Provider: "nobody"}); !errors.Is(err, ErrNoAdapter) {
		t.Fatalf("unknown: %v", err)
	}
	for _, bad := range []func(){
		func() { Register("acme-fake", func(Config) (Adapter, error) { return nil, nil }) },
		func() { Register("", func(Config) (Adapter, error) { return nil, nil }) },
		func() { Register("acme-nil", nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("bad registration did not panic")
				}
			}()
			bad()
		}()
	}
}

func TestCategoriesMatchLaneKinds(t *testing.T) {
	for _, c := range Categories {
		if !slices.Contains(store.LaneKinds, c) {
			t.Fatalf("category %s is not a lane kind", c)
		}
		if !ValidCategory(c) {
			t.Fatal(c)
		}
	}
	if ValidCategory("other") || ValidCategory("") {
		t.Fatal("other is a lane kind, not a status category")
	}
}

func TestConfigFromManifest(t *testing.T) {
	tr := &manifest.Tracker{Provider: "jira", Scope: map[string]string{"org": "acme", "x": "y"}, Auth: "env", Env: "ACME_JIRA_TOKEN"}
	c := ConfigFromManifest("acme", tr)
	if c.Workspace != "acme" || c.Provider != "jira" || c.Scope["x"] != "y" || c.ScopeKey != tr.ScopeKey() || c.AssignedTo != "me" {
		t.Fatalf("%+v", c)
	}
	tr.Scope["x"] = "changed"
	if c.Scope["x"] != "y" {
		t.Fatal("scope is shared with the manifest")
	}
	r := c.CredRequest()
	if r.Workspace != "acme" || r.Provider != "jira" || r.Preferred != cred.SourceEnv || r.EnvVar != "ACME_JIRA_TOKEN" {
		t.Fatalf("%+v", r)
	}
	if ConfigFromManifest("acme", nil).Provider != "" {
		t.Fatal("nil tracker")
	}
}

func TestRateLimitError(t *testing.T) {
	var err error = &RateLimitError{Provider: "github", RetryAfter: 30 * time.Second}
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 30*time.Second || err.Error() != "github: rate limited, retry after 30s" {
		t.Fatal(err)
	}
}
