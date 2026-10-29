package decide

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// Pack questions join the same Jev call; a raised one becomes a flag, and a
// core question is never replaced.
func TestTriageWithQuestions(t *testing.T) {
	var asked map[string]any
	f := newFake(t, func(w http.ResponseWriter, body map[string]any) {
		asked = body["questions"].(map[string]any)
		ans := map[string]any{}
		for id := range asked {
			p := 0.1
			if id == "pipeline" {
				p = 0.9
			}
			ans[id] = map[string]any{"noul": p}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": ans})
	})
	extra := map[string]Question{"pipeline": Noul("Does this change alter a data pipeline?"), "auth": Noul("replaced?")}
	r := TriageWithQuestions(context.Background(), client(f), Policy{Backend: "typesafe"}, "diff --git a/x.py b/x.py\n+x = 1\n", []string{"x.py"}, extra)
	if r.Risk != "high" || len(r.Flags) != 1 || r.Flags[0] != "pipeline" {
		t.Fatalf("%+v", r)
	}
	if len(asked) != 13 {
		t.Fatalf("asked %d questions, want 12 core + 1 pack", len(asked))
	}
	if q, _ := asked["auth"].(map[string]any); q["instructions"] == "replaced?" {
		t.Fatal("a pack question replaced a core one")
	}
}
