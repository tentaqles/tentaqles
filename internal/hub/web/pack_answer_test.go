package web

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// R2-1: the board never answers a pack approval, whatever the cookie, the
// token, the local key or the path spelling; it shows the terminal command.
func TestBoardNeverAnswersAPackApproval(t *testing.T) {
	t.Setenv("TQ_HOME", filepath.Join(t.TempDir(), ".tentaqles"))
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Promote churn"}, nil)
	d, err := svc.Store().AskHub(c.ID, "", "", "ml approval", []string{"approve", "reject"}, map[string]any{
		"pack": "ml", "action": "promote", "subject": "churn@7", "target": "production", "expires_at": "2099-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	ts := e.server(Options{LocalKey: "lk"})
	pending := func() bool { got, _ := svc.Store().Decision(d.ID); return got.Status == store.DecisionPending }

	_, body, _ := get(t, ts, "/waiting?ws=acme", nil)
	if !strings.Contains(body, "tq hub answer "+c.ID+" "+d.ID+" --choice approve") || strings.Contains(body, `value="approve">`) {
		t.Fatal("the board must show the terminal command and no answer buttons")
	}
	code, _, _ := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/decisions/"+d.ID+"/answer?ws=acme", map[string]any{"choice": "approve"}, map[string]string{serveinfo.HeaderLocal: "lk"})
	if code != http.StatusForbidden || !pending() {
		t.Fatalf("api: %d", code)
	}
	for _, p := range []string{"/cards/" + c.ID + "/decisions/" + d.ID + "/answer?ws=acme", "/cards/" + c.ID + "/%64ecisions/" + d.ID + "/%61nswer?ws=acme"} {
		for _, h := range []map[string]string{origin(ts), {serveinfo.HeaderLocal: "lk"}} {
			if code, _, _ := postForm(t, ts, p, url.Values{"choice": {"approve"}}, h); code != http.StatusForbidden || !pending() {
				t.Fatalf("%s %v: %d", p, h, code)
			}
		}
	}
}
