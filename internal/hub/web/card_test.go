package web

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestCardPageLayout(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	ctx := context.Background()
	c, _ := svc.Add(ctx, store.NewCard{Title: "Add --json", Body: "Print the same as `tq hub status`.", Tags: []string{store.TagAgentReady, "cli"}}, nil)
	r, _, _ := svc.Enqueue(ctx, c.ID, "test")
	r.Status, r.Rounds, r.CostUSD = store.StatusRunning, 2, 3.1
	r.RoundLog = []store.RoundRecord{{N: 1, StartedAt: "2026-10-22T14:02:00Z", Seconds: 60, CostUSD: 1.2, Outcome: "checkpoint-passed"}}
	svc.Store().SaveRun(r)
	os.WriteFile(r.LogPath, []byte("round 2 · claude -p\n"), 0o600)
	svc.Comment(ctx, c.ID, "Looks good")
	ts := e.server(Options{})
	code, body, _ := get(t, ts, "/cards/"+c.ID+"?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for _, want := range []string{
		`<div class="split">`, `<span class="pill p-run st-running">running</span><span>round 2 of 12</span>`,
		`<code>tq hub status</code>`, `<h3 class="sub">Timeline</h3>`, "Round 1: checkpoint passed", "$1.20 · 60 s", "A round is running",
		`<pre class="log" id="log" data-run="` + r.ID + `"`, "round 2 · claude -p", `<dt>Adapter</dt>`,
		`style="width:31%"`, "31% of the dollar budget", `action="/runs/` + r.ID + `/cancel?ws=acme"`,
		`action="/cards/` + c.ID + `/tags?ws=acme" data-tags-card="` + c.ID + `"`,
		`<span class="tag ready" data-tag="agent-ready">agent-ready<button type="button" class="x" aria-label="Remove tag agent-ready">×</button></span>`,
		`<input type="hidden" name="tags" value="agent-ready,cli">`,
		`<div class="cm" id="comments">`, "Looks good", `data-comment-card="` + c.ID + `"`, `data-move data-card="` + c.ID + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("card page lacks %q", want)
		}
	}
	assertCSPSafe(t, "/cards/"+c.ID, body)
}

func TestCardTagsFormRoute(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "x"}, nil)
	ts := e.server(Options{})
	code, _, hdr := postForm(t, ts, "/cards/"+c.ID+"/tags?ws=acme", url.Values{"tags": {"a, b"}, "add": {"c"}}, origin(ts))
	if code != http.StatusSeeOther || hdr.Get("Location") != "/cards/"+c.ID+"?ws=acme&flash=tags-saved" {
		t.Fatalf("%d %v", code, hdr)
	}
	if got, _ := e.svcs["acme"].Store().Card(c.ID); !reflect.DeepEqual(got.Tags, []string{"a", "b", "c"}) {
		t.Fatalf("%+v", got.Tags)
	}
	if code, _, _ := postForm(t, ts, "/cards/c-AAAA/tags?ws=acme", url.Values{"tags": {"a"}}, origin(ts)); code != http.StatusNotFound {
		t.Fatalf("bad id: %d", code)
	}
}

func TestAPIUpdateCard(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "x"}, nil)
	ts := e.server(Options{LocalKey: "lk"})
	code, m, raw := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"?ws=acme", map[string]any{"tags": []string{"security", "ops@example.com"}}, nil)
	if code != 200 || strings.Contains(raw, "@example.com") || !strings.Contains(raw, `"security"`) {
		t.Fatalf("tags are masked at write: %d %s", code, raw)
	}
	if code, m, _ = apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"?ws=acme", map[string]any{"title": "Renamed"}, nil); code != 200 || m["title"] != "Renamed" {
		t.Fatalf("%d %v", code, m)
	}
	if code, m, _ = apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"?ws=acme", map[string]any{}, nil); code != http.StatusBadRequest || m["code"] != "bad_request" {
		t.Fatalf("an empty update: %d %v", code, m)
	}
	if code, _, _ = apiDo(t, ts.URL, "POST", "/api/cards/c-aaaaaaaaaaaa?ws=acme", map[string]any{"title": "x"}, nil); code != http.StatusNotFound {
		t.Fatalf("unknown card: %d", code)
	}
}

func TestAgentTagEditsFollowLanePolicy(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	ts := e.server(Options{LocalKey: "lk"})
	agent := map[string]string{HeaderAgent: "1"}
	if code, _, raw := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"?ws=acme", map[string]any{"tags": []string{"needs-spec"}}, agent); code != 200 {
		t.Fatalf("an agent tags a backlog card: %d %s", code, raw)
	}
	if code, m, _ := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"?ws=acme", map[string]any{"title": "renamed"}, agent); code != http.StatusForbidden || m["code"] != "human_only" {
		t.Fatalf("an agent renames: %d %v", code, m)
	}
	if _, err := svc.Move(context.Background(), c.ID, "in progress", false); err != nil {
		t.Fatal(err)
	}
	if code, m, _ := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"?ws=acme", map[string]any{"tags": []string{"x"}}, agent); code != http.StatusForbidden || m["code"] != "human_only" {
		t.Fatalf("an agent tags a card in progress: %d %v", code, m)
	}
	hdr := origin(ts)
	hdr[HeaderAgent] = "1"
	if code, _, _ := postForm(t, ts, "/cards/"+c.ID+"/tags?ws=acme", url.Values{"tags": {"x"}}, hdr); code != http.StatusForbidden {
		t.Fatalf("the form route applies the same policy: %d", code)
	}
	if got, _ := svc.Store().Card(c.ID); !reflect.DeepEqual(got.Tags, []string{"needs-spec"}) {
		t.Fatalf("the tags are unchanged: %+v", got.Tags)
	}
}

func TestTimelineOrdersRunsAndDecisions(t *testing.T) {
	runs := []store.Run{{ID: "r-bbbbbbbbbbbb", Status: store.StatusFailed, OnRamp: "feature", QueuedAt: "2026-10-22T10:00:00Z", EndedAt: "2026-10-22T10:30:00Z", StopReason: "relocate loop",
		RoundLog: []store.RoundRecord{{N: 1, StartedAt: "2026-10-22T10:01:00Z", Seconds: 30, ExitCode: 1, CostUnknown: true}}}}
	decisions := []store.Decision{{ID: "d1", Kind: store.KindPlanApproval, Status: store.DecisionAnswered, Choice: "approve", Actor: "hub:tester",
		AskedAt: "2026-10-22T10:05:00Z", AnsweredAt: "2026-10-22T10:06:00+00:00"}}
	got := timeline(runs, decisions)
	var texts, classes []string
	for _, it := range got {
		texts = append(texts, it.Text)
		classes = append(classes, it.Class)
	}
	wantTexts := []string{"Run queued (feature)", "Round 1", "Plan approval asked", "Plan approval answered: approve", "Run failed"}
	if !reflect.DeepEqual(texts, wantTexts) || !reflect.DeepEqual(classes, []string{"", "x", "w", "ok", "x"}) {
		t.Fatalf("%q %q", texts, classes)
	}
	if got[1].Detail != "cost unknown · 30 s" || got[4].Detail != "relocate loop" || got[3].Detail != "hub:tester" {
		t.Fatalf("%+v", got)
	}
}
