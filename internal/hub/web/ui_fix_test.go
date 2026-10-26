package web

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// TestCardFragmentCarriesLiveLogCursor: the live refresh of a card page whose run started after
// the page loaded hands hub.js the run id and the log offset, so it can reconnect the stream.
func TestCardFragmentCarriesLiveLogCursor(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	ctx := context.Background()
	c, _ := svc.Add(ctx, store.NewCard{Title: "Stream me", Tags: []string{store.TagAgentReady}}, nil)
	ts := e.server(Options{})
	_, before, _ := get(t, ts, "/cards/"+c.ID+"?ws=acme&fragment=main", nil)
	if strings.Contains(before, `id="log"`) {
		t.Fatal("no run yet: no live log")
	}
	r, _, err := svc.Enqueue(ctx, c.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	r.Status = store.StatusRunning
	svc.Store().SaveRun(r)
	logText := "round 1 · started\n"
	os.WriteFile(r.LogPath, []byte(logText), 0o600)
	code, frag, _ := get(t, ts, "/cards/"+c.ID+"?ws=acme&fragment=main", nil)
	want := `id="log" data-run="` + r.ID + `" data-from="` + strconv.Itoa(len(logText)) + `"`
	if code != 200 || !strings.Contains(frag, want) {
		t.Fatalf("fragment lacks %q (%d)", want, code)
	}
}

// TestPrimaryGradientUsesItsOwnStops: the primary button runs --pri-start to --pri-end in every
// theme block, and the start is darker than the end so it reads as a gradient.
func TestPrimaryGradientUsesItsOwnStops(t *testing.T) {
	css := readAsset(t, "static/hub.css")
	if !strings.Contains(css, ".btn.pri{background:linear-gradient(90deg,var(--pri-start),var(--pri-end));") {
		t.Fatal("the primary button gradient does not run --pri-start to --pri-end")
	}
	if n := strings.Count(css, "--pri-start:#0D47A1;"); n != 3 {
		t.Fatalf("--pri-start is #0D47A1 in %d of 3 theme blocks", n)
	}
	if !strings.Contains(css, "@media (forced-colors: active){:focus-visible{outline:") {
		t.Error("the focus ring has no outline under forced colours, where box-shadow is dropped")
	}
}

func TestShellPolish(t *testing.T) {
	e := newEnv(t, "acme", "globex")
	ts := e.server(Options{Bind: "0.0.0.0:7470"})
	_, body, _ := get(t, ts, "/runs?ws=acme", nil)
	for _, want := range []string{
		`href="/runs?ws=globex"`, // C13: the switcher keeps the page
		`<span title="listening on all interfaces">serving <b>0.0.0.0:7470</b></span>`,
		`<svg class="mark" viewBox="0 0 60 52" aria-hidden="true"`,
		`id="themebtn" type="button" aria-label="Switch theme" title="Switch theme" hidden>`,
		`href="#new-card" id="newcard"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/runs lacks %q", want)
		}
	}
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "x"}, nil)
	_, card, _ := get(t, ts, "/cards/"+c.ID+"?ws=acme", nil)
	if !strings.Contains(card, `href="/?ws=globex"`) || strings.Contains(card, `href="/cards/`+c.ID+`?ws=globex"`) {
		t.Error("a card id belongs to one workspace: its switcher links land on the board")
	}
	_, all, _ := get(t, ts, "/waiting", nil)
	if strings.Contains(all, `id="newcard"`) {
		t.Error("no workspace, no dialog: the New card button is not rendered")
	}
	for bind, want := range map[string]bool{"127.0.0.1:7470": false, "[::]:7470": true, ":7470": true, "0.0.0.0:1": true, "localhost:7470": false} {
		if got := allInterfaces(bind); got != want {
			t.Errorf("allInterfaces(%q) = %v", bind, got)
		}
	}
	ts2 := e.server(Options{Bind: "127.0.0.1:7470"})
	_, local, _ := get(t, ts2, "/?ws=acme", nil)
	if strings.Contains(local, "listening on all interfaces") {
		t.Error("a loopback bind is not all interfaces")
	}
}

// TestRestoredFormsStartClean: the browser does not restore old values into the forms whose
// unsaved state blocks the live refresh (a restored value would block it forever).
func TestRestoredFormsStartClean(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "x"}, nil)
	ts := e.server(Options{})
	_, card, _ := get(t, ts, "/cards/"+c.ID+"?ws=acme", nil)
	if !strings.Contains(card, `<select class="f" id="mv-lane" name="lane" autocomplete="off">`) {
		t.Error("the move select lacks autocomplete=off")
	}
	_, set, _ := get(t, ts, "/settings?ws=acme", nil)
	if n := strings.Count(set, `autocomplete="off" name="`); n != 7 {
		t.Errorf("settings: %d controls with autocomplete=off, want 7", n)
	}
}
