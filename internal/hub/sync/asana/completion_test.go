package asana

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
)

func TestMoveSetsCompletion(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, nil)
	ctx := context.Background()
	// 1004 is completed and sits in "In Review": a move out of done reopens it
	if err := a.Move(ctx, "1004", "Doing"); err != nil {
		t.Fatal(err)
	}
	if f.tasks["1004"].Completed || f.tasks["1004"].Section != "302" {
		t.Fatalf("reopen: %+v", f.tasks["1004"])
	}
	it, err := a.Get(ctx, "1004")
	if err != nil || it.StatusCategory != "active" || it.Status != "Doing" {
		t.Fatalf("after reopen: %+v %v", it, err)
	}
	// a move to the Done section completes the task
	f.reset()
	if err := a.Move(ctx, "1001", "Done"); err != nil {
		t.Fatal(err)
	}
	if !f.tasks["1001"].Completed || f.tasks["1001"].Section != "305" {
		t.Fatalf("complete: %+v", f.tasks["1001"])
	}
	// the request order: addTask, then the completion change
	reqs := f.requests()
	if i, j := slices.Index(reqs, "POST /api/1.0/sections/305/addTask"), slices.Index(reqs, "PUT /api/1.0/tasks/1001"); i < 0 || j < i {
		t.Fatalf("requests %v", reqs)
	}
	if it, err := a.Get(ctx, "1001"); err != nil || it.StatusCategory != "done" {
		t.Fatalf("after complete: %+v %v", it, err)
	}
	// the lanes override decides done too: QA mapped to done completes
	b := newTest(t, f, func(c *hubsync.Config) { c.Lanes = map[string]string{"QA": "done"} })
	if err := b.Move(ctx, "1002", "QA"); err != nil || !f.tasks["1002"].Completed {
		t.Fatalf("lane override: %+v %v", f.tasks["1002"], err)
	}
	// a failing completion change fails the move, so the engine retries it
	f.failOn = "PUT /api/1.0/tasks/1001"
	if err := a.Move(ctx, "1001", "Doing"); err == nil {
		t.Fatal("a failed completion change was swallowed")
	}
}

func TestCreateInDoneIsCompleted(t *testing.T) {
	f := newFake(t)
	it, err := newTest(t, f, nil).Create(context.Background(), hubsync.NewItem{Title: "Already shipped", Status: "Done"})
	if err != nil || !f.tasks[it.ID].Completed || it.StatusCategory != "done" {
		t.Fatalf("%+v %v", it, err)
	}
	if f.bodies[0]["completed"] != true {
		t.Fatalf("create body %v", f.bodies[0])
	}
}

func wellFormed(s string) error {
	d := xml.NewDecoder(strings.NewReader(s))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func TestMarkdownCrossedSpans(t *testing.T) {
	for md, want := range map[string]string{
		"_a **b_ c**":              "<body>_a <strong>b_ c</strong></body>",
		"**a _b** c_":              "<body><strong>a _b</strong> c_</body>",
		"*a _b* c_":                "<body><em>a _b</em> c_</body>",
		"**a _b_ c**":              "<body><strong>a <em>b</em> c</strong></body>",
		"_a **b** c_":              "<body><em>a <strong>b</strong> c</em></body>",
		"**[x](https://a.b) `y`**": `<body><strong><a href="https://a.b">x</a> <code>y</code></strong></body>`,
	} {
		got := markdownToHTML(md)
		if got != want {
			t.Errorf("%q\n got %s\nwant %s", md, got, want)
		}
		if err := wellFormed(got); err != nil {
			t.Errorf("%q: %s is not well-formed: %v", md, got, err)
		}
	}
}

// Every short mix of the inline markers converts to well-formed XML without the escape fallback.
func TestMarkdownAlwaysWellFormed(t *testing.T) {
	parts := []string{"*", "**", "_", "a", " ", "`", "[x](https://a.b)", "\n", "- "}
	n := 0
	var walk func(prefix string, depth int)
	walk = func(prefix string, depth int) {
		if depth == 0 {
			n++
			if got := convert(prefix); wellFormed(got) != nil {
				t.Fatalf("%q -> %s: %v", prefix, got, wellFormed(got))
			}
			return
		}
		for _, p := range parts {
			walk(prefix+p, depth-1)
		}
	}
	walk("", 5)
	if n == 0 {
		t.Fatal("nothing checked")
	}
	// longer mixes, from a fixed seed
	r := rand.New(rand.NewPCG(7, 11))
	for range 20000 {
		var b strings.Builder
		for range 4 + r.IntN(10) {
			b.WriteString(parts[r.IntN(len(parts))])
		}
		if got := convert(b.String()); wellFormed(got) != nil {
			t.Fatalf("%q -> %s: %v", b.String(), got, wellFormed(got))
		}
	}
	// the safety net: a result that is not well-formed goes out as escaped text
	if got := safeBody("<body><em>a <strong>b</em></strong></body>", "_a **b_**"); got != "<body>_a **b_**</body>" {
		t.Fatal(got)
	}
}

func TestFirstSyncAcrossTheRateLimit(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, func(c *hubsync.Config) { c.AssignedTo = "anyone" })
	ctx := context.Background()
	// a budget that runs out partway through the details of the first attempt
	f.mu.Lock()
	f.limitAt = 14
	f.mu.Unlock()
	var rl *hubsync.RateLimitError
	if _, _, err := a.ListChanged(ctx, time.Time{}); !errors.As(err, &rl) {
		t.Fatalf("first attempt: %v", err)
	}
	f.mu.Lock()
	f.limitAt = 0
	f.mu.Unlock()
	f.reset()
	items, _, err := a.ListChanged(ctx, time.Time{})
	if err != nil || len(items) != 6 {
		t.Fatalf("retry: %v %v", items, err)
	}
	// the details read before the 429 are not read again: 6 items need 12 detail reads
	details := 0
	for _, r := range f.requests() {
		if strings.HasSuffix(r, "/stories") || r == "GET /api/1.0/attachments" {
			details++
		}
	}
	if details == 0 || details >= 12 {
		t.Fatalf("the retry read %d details: %v", details, f.requests())
	}
	if it := items[0]; it.ID != "1001" || len(it.Comments) != 2 || len(it.Attachments) != 1 {
		t.Fatalf("cached details: %+v", it)
	}
	// a changed task (new Rev) has its details read again
	f.mu.Lock()
	f.stories = append(f.stories, fakeStory{GID: "7004", Task: "1001", Subtype: "comment_added", Text: "third", Author: "tester",
		Created: time.Date(2026, 10, 20, 11, 0, 0, 0, time.UTC)})
	f.tasks["1001"].Modified = time.Date(2026, 10, 20, 11, 0, 0, 0, time.UTC)
	f.mu.Unlock()
	items, _, err = a.ListChanged(ctx, time.Time{})
	if err != nil || items[0].ID != "1001" || len(items[0].Comments) != 3 || items[0].Comments[0].Body != "third" {
		t.Fatalf("changed task: %+v %v", items, err)
	}
	// Get always reads fresh
	f.mu.Lock()
	f.stories = append(f.stories, fakeStory{GID: "7005", Task: "1001", Subtype: "comment_added", Text: "fourth", Author: "tester",
		Created: time.Date(2026, 10, 20, 11, 1, 0, 0, time.UTC)})
	f.mu.Unlock()
	if it, err := a.Get(ctx, "1001"); err != nil || len(it.Comments) != 4 {
		t.Fatalf("get: %+v %v", it, err)
	}
}

func TestDetailsCacheIsBounded(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, nil)
	for i := range maxCached + 10 {
		a.remember(hubsync.Item{ID: strconv.Itoa(i), Rev: "r"})
	}
	a.mu.Lock()
	n := len(a.cache)
	a.mu.Unlock()
	if n > maxCached {
		t.Fatalf("cache holds %d", n)
	}
}

func TestSetLabelsUsesTypeahead(t *testing.T) {
	f := newFake(t)
	f.tags["902"] = "bugfix" // a near match the typeahead also returns
	for i := range 6000 {    // more tags than the page cap: labels must still work
		f.tags[strconv.Itoa(10000+i)] = "noise-" + strconv.Itoa(i)
	}
	a := newTest(t, f, nil)
	if err := a.SetLabels(context.Background(), "1002", []string{"BUG", "frontend"}, []string{"agent-ready"}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range f.tasks["1002"].Tags {
		names = append(names, f.tags[g])
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"bug", "frontend"}) {
		t.Fatalf("tags %v", names)
	}
	for _, r := range f.requests() {
		if strings.HasPrefix(r, "GET") && strings.HasSuffix(r, "/tags") {
			t.Fatalf("listed every workspace tag: %s", r)
		}
	}
	if !slices.Equal(f.typeaheads, []string{"agent-ready", "BUG", "frontend"}) {
		t.Fatalf("typeaheads %v", f.typeaheads)
	}
}

func TestTruncatedStoriesDropTheComments(t *testing.T) {
	f := newFake(t)
	f.pageSize = 1
	a := newTest(t, f, nil)
	a.maxPages = 2 // 1001 has three stories: only the oldest two would be read
	it, err := a.Get(context.Background(), "1001")
	if err != nil || len(it.Comments) != 0 || len(it.Attachments) != 1 {
		t.Fatalf("%+v %v", it.Comments, err)
	}
}
