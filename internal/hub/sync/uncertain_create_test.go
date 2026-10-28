package sync

import (
	"context"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// A Create that may have made the item but knows no id fails the row at once, with a note that
// tells the human to check the tracker, and is never sent again by the engine.
func TestUncertainCreateFailsTheRowAndNeverCreatesAgain(t *testing.T) {
	r := newRig(t)
	if err := r.st.SetSyncPolicy(store.OutboxCreateCard, store.PolicyAuto); err != nil {
		t.Fatal(err)
	}
	r.fake.uncertainCreate = true
	kid, err := r.st.AddCard(store.NewCard{Title: "Write docs", Body: "body"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	row, err := r.eng.Push(context.Background(), kid.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = r.eng.Flush(context.Background())
	o, err := r.st.OutboxRow(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != store.OutboxFailed || !strings.Contains(o.LastError, "check the tracker") {
		t.Fatalf("row %+v", o)
	}
	r.flush()
	r.tick()
	if len(r.fake.created) != 1 {
		t.Fatalf("created %d times: %v", len(r.fake.created), r.fake.callLog())
	}
}
