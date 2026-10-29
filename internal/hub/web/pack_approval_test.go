package web

import (
	"context"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// The decision view of a pack approval shows the gated action and the
// evidence list (spec C §2.11).
func TestPackApprovalShowsEvidence(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Promote churn"}, nil)
	svc.Store().AskHub(c.ID, "", "", "ml approval: promote churn@7 -> production", []string{"approve", "reject"}, map[string]any{
		"pack": "ml", "action": "promote", "subject": "churn@7", "target": "production", "expires_at": "2026-11-04T00:00:00Z",
		"evidence": map[string]any{"model_card": "sha256:aa11", "git_head": "sha256:bb22"},
	})
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/waiting?ws=acme", nil)
	for _, want := range []string{"ml pack approval: <b>promote</b>", `<code class="m">churn@7</code>`, `<code class="m">production</code>`,
		`<li><code class="m">git_head</code> <code class="m">sha256:bb22</code></li><li><code class="m">model_card</code>`,
		"Answer this in your own terminal"} {
		if !strings.Contains(body, want) {
			t.Errorf("pack approval view lacks %q", want)
		}
	}
	assertCSPSafe(t, "/waiting", body)
}
