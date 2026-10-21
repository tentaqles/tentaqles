package web

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func readUntil(t *testing.T, br *bufio.Reader, want string) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		for {
			line, err := br.ReadString('\n')
			b.WriteString(line)
			if strings.Contains(b.String(), want) || err != nil {
				done <- b.String()
				return
			}
		}
	}()
	select {
	case s := <-done:
		if !strings.Contains(s, want) {
			t.Fatalf("stream ended without %q:\n%s", want, s)
		}
		return s
	case <-time.After(5 * time.Second):
		t.Fatalf("no %q within 5 s", want)
	}
	return ""
}

func TestSSEDeliversStoreChanges(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	ts := e.server(Options{PollEvery: 100 * time.Millisecond})
	resp, err := http.Get(ts.URL + "/events?ws=acme")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatal(ct)
	}
	br := bufio.NewReader(resp.Body)
	readUntil(t, br, ": connected")
	svc.Comment(context.Background(), c.ID, "hello")
	got := readUntil(t, br, "event: change")
	if !strings.Contains(got, `"workspace":"acme"`) {
		t.Fatal(got)
	}
}

func TestSSEStreamsTheRunLog(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	r, _, _ := svc.Enqueue(context.Background(), c.ID, "test")
	os.WriteFile(r.LogPath, []byte("=== round 1\nhello from the run\n"), 0o600)
	ts := e.server(Options{PollEvery: 100 * time.Millisecond})
	resp, err := http.Get(ts.URL + "/events?ws=acme&run=" + r.ID + "&from=0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got := readUntil(t, bufio.NewReader(resp.Body), "hello from the run")
	if !strings.Contains(got, "event: log") {
		t.Fatal(got)
	}
}

func TestSSEChangeCarriesIDsOnly(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	ts := e.server(Options{PollEvery: 100 * time.Millisecond})
	resp, err := http.Get(ts.URL + "/events?ws=acme")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	readUntil(t, br, ": connected")
	svc.Comment(context.Background(), c.ID, "secret-ish comment text")
	got := readUntil(t, br, "event: change")
	if strings.Contains(got, "secret-ish") {
		t.Fatal(got)
	}
}
