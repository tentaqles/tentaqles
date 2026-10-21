package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func apiDo(t *testing.T, base, method, path string, body any, hdr map[string]string) (int, map[string]any, string) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TQ-Hub-Local", "lk")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	json.Unmarshal(raw, &m)
	return resp.StatusCode, m, string(raw)
}

func TestAPIAddListShow(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{LocalKey: "lk"})
	code, m, raw := apiDo(t, ts.URL, "POST", "/api/cards?ws=acme", map[string]any{"card": store.NewCard{Title: "From the CLI", Kind: "fix"}}, nil)
	if code != 200 || m["kind"] != "fix" {
		t.Fatalf("%d %s", code, raw)
	}
	id := m["id"].(string)
	code, _, raw = apiDo(t, ts.URL, "GET", "/api/cards?ws=acme&kind=fix", nil, nil)
	if code != 200 || !strings.Contains(raw, id) {
		t.Fatalf("%d %s", code, raw)
	}
	code, m, raw = apiDo(t, ts.URL, "GET", "/api/cards/"+id+"?ws=acme", nil, nil)
	if _, has := m["comments"]; code != 200 || m["title"] != "From the CLI" || !has {
		t.Fatalf("%d %s", code, raw)
	}
	if code, m, _ := apiDo(t, ts.URL, "GET", "/api/cards/c-aaaaaaaaaaaa?ws=acme", nil, nil); code != 404 || m["code"] != "not_found" {
		t.Fatalf("%d %v", code, m)
	}
}

func TestAPIMoveRunningNeedsConfirm(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}}, nil)
	out, _ := svc.Move(context.Background(), c.ID, "in progress", false)
	r := *out.Enqueued
	r.Status = store.StatusRunning
	svc.Store().SaveRun(r)
	ts := e.server(Options{LocalKey: "lk"})
	code, m, raw := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/move?ws=acme", map[string]any{"lane": "backlog"}, nil)
	if code != http.StatusConflict || m["code"] != "run_active" || !strings.Contains(raw, r.ID) {
		t.Fatalf("%d %s", code, raw)
	}
	if got, _ := svc.Store().Card(c.ID); got.Lane != "in progress" {
		t.Fatal("the card stays put without confirmation")
	}
	code, _, raw = apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/move?ws=acme", map[string]any{"lane": "backlog", "cancel_run": true}, nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	if got, _ := svc.Store().Run(r.ID); got.Status != store.StatusCancelled {
		t.Fatalf("%+v", got)
	}
	page := e.server(Options{})
	c2, _ := svc.Add(context.Background(), store.NewCard{Title: "y", Tags: []string{store.TagAgentReady}}, nil)
	out2, _ := svc.Move(context.Background(), c2.ID, "in progress", false)
	r2 := *out2.Enqueued
	r2.Status = store.StatusRunning
	svc.Store().SaveRun(r2)
	hdr := origin(page)
	hdr["X-TQ-Hub-Fetch"] = "1"
	code, body, _ := postForm(t, page, "/cards/"+c2.ID+"/move?ws=acme", map[string][]string{"lane": {"backlog"}}, hdr)
	if code != http.StatusConflict || !strings.Contains(body, "--cancel-run") {
		t.Fatalf("page move: %d %s", code, body)
	}
}

func TestAPIAgentHeaderRefusesHumanOnly(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}}, nil)
	e.pendingDecision("acme", c, store.KindPlanApproval)
	ts := e.server(Options{LocalKey: "lk"})
	agent := map[string]string{HeaderAgent: "1"}
	code, m, _ := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/decisions/d1a2b3c4/answer?ws=acme", map[string]any{"choice": "approve"}, agent)
	if code != http.StatusForbidden || m["code"] != "human_only" || len(e.gate.answers) != 0 {
		t.Fatalf("%d %v", code, m)
	}
	code, m, _ = apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/comments?ws=acme", map[string]any{"body": "PR: https://github.com/a/b/pull/1"}, agent)
	if code != 200 || m["author_kind"] != store.AuthorAgent {
		t.Fatalf("%d %v", code, m)
	}
	if code, m, _ := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/move?ws=acme", map[string]any{"lane": "done", "cancel_run": true}, agent); code != http.StatusForbidden {
		t.Fatalf("agent → done: %d %v", code, m)
	}
	_ = runner.ErrHumanOnly
}

type failingAnswerGate struct{ *webGate }

func (g failingAnswerGate) Answer(ctx context.Context, dir, slug, id, choice, note, actor string) (string, error) {
	msg, _ := g.webGate.Answer(ctx, dir, slug, id, choice, note, actor)
	return msg, errors.New("build-gate exited 2: recorded, but the resume hook failed")
}

func TestAPIAnswerShowsOutcomeAndError(t *testing.T) {
	e := newEnv(t, "acme")
	st := e.svcs["acme"].Store()
	svc, err := runner.New(runner.Config{Workspace: "acme", Root: e.root, Store: st, Gate: failingAnswerGate{e.gate}, Actor: "hub:tester"})
	if err != nil {
		t.Fatal(err)
	}
	e.svcs["acme"] = svc
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}}, nil)
	e.pendingDecision("acme", c, store.KindPlanApproval)
	ts := e.server(Options{LocalKey: "lk"})
	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())
	cl := NewClient(serveinfo.Info{Addr: u.Host, Port: port, LocalKey: "lk"}, "acme", false)
	out, err := cl.Answer(context.Background(), c.ID, "d1a2b3c4", "approve", "")
	if err == nil || !strings.Contains(err.Error(), "resume hook failed") || out.Decision.ID != "d1a2b3c4" || !strings.Contains(out.Message, "decision d1a2b3c4: approve") {
		t.Fatalf("client: %+v %v", out, err)
	}
	// once recorded, a second answer is reported as already answered, without an error
	code, m, raw := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/decisions/d1a2b3c4/answer?ws=acme", map[string]any{"choice": "approve"}, nil)
	if code != 200 || m["already_answered"] != true {
		t.Fatalf("%d %s", code, raw)
	}
}
