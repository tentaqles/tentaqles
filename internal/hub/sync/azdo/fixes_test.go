package azdo

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// ---- cli source: identity isolation and token lifetime ----

func TestAzTokenRefusesWithoutAConfigDir(t *testing.T) {
	isolate(t)
	old := lookPath
	lookPath = func(string) (string, error) {
		t.Error("az must not run without this identity's AZURE_CONFIG_DIR")
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { lookPath = old })
	for name, env := range map[string][]string{
		"nil":     nil,
		"missing": withoutAzureDir(),
		"empty":   append(withoutAzureDir(), "AZURE_CONFIG_DIR="),
	} {
		_, err := AzToken(context.Background(), env)
		if !errors.Is(err, cred.ErrNoCredential) || !strings.Contains(err.Error(), "no AZURE_CONFIG_DIR for this identity") {
			t.Fatalf("%s env: err %v", name, err)
		}
	}
}

func TestAzTokenIsCachedForItsLifetime(t *testing.T) {
	isolate(t)
	installFakeAz(t)
	dir := filepath.Join(t.TempDir(), "acme")
	env := append(withoutAzureDir(), "AZURE_CONFIG_DIR="+dir)
	for i := 0; i < 3; i++ {
		tok, err := AzToken(context.Background(), env)
		if err != nil || tok.Value != "tok-acme" {
			t.Fatalf("token %v %v", tok, err)
		}
	}
	if n := azRuns(t, dir); n != 1 {
		t.Fatalf("az ran %d times, want 1", n)
	}
	// Another identity never gets the cached token.
	other := filepath.Join(t.TempDir(), "tester")
	tok, err := AzToken(context.Background(), append(withoutAzureDir(), "AZURE_CONFIG_DIR="+other))
	if err != nil || tok.Value != "tok-tester" {
		t.Fatalf("other identity %v %v", tok, err)
	}
}

func TestAzTokenNearExpiryRunsAzAgain(t *testing.T) {
	isolate(t)
	installFakeAz(t)
	dir := filepath.Join(t.TempDir(), "acme")
	env := append(withoutAzureDir(), "AZURE_CONFIG_DIR="+dir, "FAKE_AZ_EXPIRES_IN=30s")
	for i := 0; i < 2; i++ {
		if _, err := AzToken(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}
	if n := azRuns(t, dir); n != 2 {
		t.Fatalf("az ran %d times, want 2 (a token about to expire is not reused)", n)
	}
}

func TestAzTokenOlderCLIExpiresOnLocalTime(t *testing.T) {
	isolate(t)
	installFakeAz(t)
	dir := filepath.Join(t.TempDir(), "acme")
	env := append(withoutAzureDir(), "AZURE_CONFIG_DIR="+dir, "FAKE_AZ_OLD=1")
	for i := 0; i < 2; i++ {
		if _, err := AzToken(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}
	if n := azRuns(t, dir); n != 1 {
		t.Fatalf("az ran %d times, want 1", n)
	}
}

func TestAdapterRunsAzOncePerToken(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	installFakeAz(t)
	dir := filepath.Join(t.TempDir(), "acme")
	env := append(withoutAzureDir(), "AZURE_CONFIG_DIR="+dir)
	a := newAdapter(t, srv, func(c *hubsync.Config) {
		c.Env = env
		c.Transport = cred.New(cred.SourceCLI, func(ctx context.Context) (cred.Token, error) { return AzToken(ctx, env) })
	})
	if _, _, err := a.ListChanged(context.Background(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Get(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	if len(f.requests) < 4 {
		t.Fatalf("only %d requests", len(f.requests))
	}
	if n := azRuns(t, dir); n != 1 {
		t.Fatalf("az ran %d times for %d requests, want 1", n, len(f.requests))
	}
	// A 401 drops the cached token, so the next call asks az again.
	f.wantAuth = "Bearer other"
	if _, err := a.Get(context.Background(), "2"); err == nil {
		t.Fatal("want a 401")
	}
	f.wantAuth = "Bearer tok-acme"
	if _, err := a.Get(context.Background(), "2"); err != nil {
		t.Fatal(err)
	}
	if n := azRuns(t, dir); n != 2 {
		t.Fatalf("az ran %d times after a 401, want 2", n)
	}
}

// ---- ListChanged: capped watermark ----

// cappedFlat adds wiqlTop+1 assigned items changed one second apart (ids 2000..3000) from base.
func cappedFlat(f *fakeADO, base time.Time, step time.Duration) {
	for i := 0; i < wiqlTop+1; i++ {
		id := 2000 + i
		f.addItem(id, 1, base.Add(time.Duration(i)*step).Format(time.RFC3339Nano), map[string]any{
			"System.Title": "x", "System.WorkItemType": "Task", "System.State": "New",
			"System.AssignedTo": map[string]any{"uniqueName": "tester@acme.test"},
		})
		f.flat = append(f.flat, id)
	}
	f.filterFlat = true
}

func hasID(items []hubsync.Item, id string) bool {
	for _, it := range items {
		if it.ID == id {
			return true
		}
	}
	return false
}

func TestListChangedCappedMarkIgnoresEditsAfterTheQuery(t *testing.T) {
	for _, ignoreAsOf := range []bool{false, true} {
		f, srv := newFake(t)
		base := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
		cappedFlat(f, base, time.Second)
		f.asOf = "2026-10-27T09:00:00Z"
		f.ignoreAsOf = ignoreAsOf
		// Item 2000 is edited after the WIQL ran, before the batch read.
		edited := false
		f.hook = func(w http.ResponseWriter, r *http.Request) bool {
			if strings.HasSuffix(r.URL.Path, "/workitemsbatch") && !edited {
				edited = true
				it := f.items[2000]
				fields := map[string]any{}
				for k, v := range it["fields"].(map[string]any) {
					fields[k] = v
				}
				f.past = map[int]map[string]any{2000: {"id": 2000, "rev": 1, "fields": fields, "url": it["url"]}}
				it["fields"].(map[string]any)["System.ChangedDate"] = "2026-10-27T10:00:00Z"
				it["rev"] = 2
			}
			return false
		}
		a := newAdapter(t, srv)
		_, mark, err := a.ListChanged(context.Background(), base)
		if err != nil {
			t.Fatal(err)
		}
		if want := base.Add(time.Duration(wiqlTop-1) * time.Second); !mark.Equal(want) {
			t.Fatalf("ignoreAsOf %v: mark %v, want the 1000th item's ChangedDate %v", ignoreAsOf, mark, want)
		}
		items, _, err := a.ListChanged(context.Background(), mark)
		if err != nil {
			t.Fatal(err)
		}
		if !hasID(items, "3000") {
			t.Fatalf("ignoreAsOf %v: item 1001 skipped on the next tick (%d items)", ignoreAsOf, len(items))
		}
		if ignoreAsOf {
			continue
		}
		for _, b := range f.batch[:1] {
			if b["asOf"] != f.asOf {
				t.Fatalf("batch without the query's asOf: %v", b["asOf"])
			}
		}
	}
}

func TestListChangedPagesPastAChangedDateTie(t *testing.T) {
	f, srv := newFake(t)
	base := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	cappedFlat(f, base, 0) // 1001 items in the same millisecond
	a := newAdapter(t, srv)
	items, mark, err := a.ListChanged(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != wiqlTop || !mark.Equal(base) {
		t.Fatalf("first page %d items, mark %v", len(items), mark)
	}
	if q := f.queries[0]; !strings.Contains(q, "ORDER BY [System.ChangedDate] ASC, [System.Id] ASC") {
		t.Fatalf("flat query without an id order: %s", q)
	}
	items, mark, err = a.ListChanged(context.Background(), mark)
	if err != nil {
		t.Fatal(err)
	}
	if !hasID(items, "3000") || len(items) != 1 {
		t.Fatalf("second page %d items, has 3000: %v", len(items), hasID(items, "3000"))
	}
	if !mark.Equal(base) {
		t.Fatalf("mark %v", mark)
	}
	// An uncapped page clears the tiebreak: the next query is a plain >= again.
	if _, _, err := a.ListChanged(context.Background(), mark); err != nil {
		t.Fatal(err)
	}
	var lastFlat string
	for _, q := range f.queries {
		if strings.Contains(q, "FROM WorkItems ") {
			lastFlat = q
		}
	}
	if !strings.Contains(lastFlat, "AND [System.ChangedDate] >= '") || strings.Contains(lastFlat, "[System.Id] >") {
		t.Fatalf("tiebreak kept after an uncapped page: %s", lastFlat)
	}
}

func TestListChangedBoundsCommentFetches(t *testing.T) {
	f, srv := newFake(t)
	base := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		id := 10 + i
		f.addItem(id, 1, base.Add(time.Duration(i)*time.Minute).Format(time.RFC3339Nano), map[string]any{
			"System.Title": "x", "System.WorkItemType": "Task", "System.State": "New", "System.CommentCount": 1,
		})
		f.comments[id] = []map[string]any{{"id": 1, "text": "hi", "createdDate": "2026-10-27T07:00:00Z"}}
		f.flat = append(f.flat, id)
	}
	f.filterFlat = true
	a := newAdapter(t, srv, func(c *hubsync.Config) { c.AssignedTo = "anyone" })
	a.commentBudget = 2
	var got []string
	since := base
	for tick := 0; tick < 4 && len(got) < 5; tick++ {
		n := len(f.requests)
		items, mark, err := a.ListChanged(context.Background(), since)
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		for _, r := range f.requests[n:] {
			if strings.HasSuffix(r, "/comments") {
				calls++
			}
		}
		if calls > 2 {
			t.Fatalf("tick %d: %d comment reads, budget 2", tick, calls)
		}
		for _, it := range items {
			if len(it.Comments) != 1 {
				t.Fatalf("item %s returned without its comments", it.ID)
			}
			got = append(got, it.ID)
		}
		since = mark
	}
	if strings.Join(got, ",") != "10,11,12,13,14" {
		t.Fatalf("items over the ticks %v", got)
	}
}

// ---- labels ----

func TestLabelsWithSemicolonsRoundTrip(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	a := newAdapter(t, srv)
	ctx := context.Background()
	if err := a.SetLabels(ctx, "1", []string{"a;b"}, nil); err != nil {
		t.Fatal(err)
	}
	p := f.patches[1]
	if len(p) != 2 || strings.Count(p[1]["value"].(string), ";") != 2 {
		t.Fatalf("a ';' in a label must not split the tag: %v", p)
	}
	it, err := a.Get(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(it.Labels, "|") != "frontend|agent-ready|a;b" {
		t.Fatalf("labels back %q", it.Labels)
	}
	n := len(f.patches[1])
	if err := a.SetLabels(ctx, "1", it.Labels, nil); err != nil || len(f.patches[1]) != n {
		t.Fatalf("SetLabels did not converge: %v %v", err, f.patches[1][n:])
	}
	if err := a.SetLabels(ctx, "1", nil, []string{"a;b"}); err != nil {
		t.Fatal(err)
	}
	if got := f.patches[1][len(f.patches[1])-1]["value"]; got != "frontend; agent-ready" {
		t.Fatalf("remove %v", got)
	}
	created, err := a.Create(ctx, hubsync.NewItem{Title: "x", Labels: []string{"x;y", "z"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(created.Labels, "|") != "x;y|z" {
		t.Fatalf("created labels %q", created.Labels)
	}
}

// ---- Create ----

func TestCreateSetsTheStateAfterwardsAndReportsAPartialCreate(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	a := newAdapter(t, srv)
	it, err := a.Create(context.Background(), hubsync.NewItem{Title: "x", Status: "Active"})
	if err != nil {
		t.Fatal(err)
	}
	if it.Status != "Active" {
		t.Fatalf("status %q", it.Status)
	}
	for _, op := range f.created[0]["ops"].([]map[string]any) {
		if op["path"] == "/fields/System.State" {
			t.Fatal("the state goes in a later patch, so a refused state never blocks the create")
		}
	}
	if p := f.patches[101]; len(p) != 1 || p[0]["value"] != "Active" {
		t.Fatalf("state patch %v", p)
	}
	// The state patch fails: the item exists, so the error says which one.
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == "PATCH" {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"message": "TF401320: invalid state"})
			return true
		}
		return false
	}
	_, err = a.Create(context.Background(), hubsync.NewItem{Title: "y", Status: "Bogus"})
	var pc *hubsync.PartialCreateError
	if !errors.As(err, &pc) {
		t.Fatalf("err %T %v", err, err)
	}
	got, ok := hubsync.CreatedItem(err)
	if !ok || got.ID != "102" || got.Title != "y" || !strings.Contains(err.Error(), "invalid state") {
		t.Fatalf("created %+v %v", got, err)
	}
	// Nothing created: a plain error.
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusBadRequest)
		return true
	}
	if _, err := a.Create(context.Background(), hubsync.NewItem{Title: "z"}); err == nil || errors.As(err, &pc) {
		t.Fatalf("a failed create must be a plain error: %v", err)
	}
}
