package decide

import "testing"

func TestTriageQuestions_IncludeAgentic(t *testing.T) {
	qs := TriageQuestions()
	if _, ok := qs["agentic"]; !ok || len(qs) != 12 {
		t.Fatalf("want 12 questions incl. agentic, got %d", len(qs))
	}
}
