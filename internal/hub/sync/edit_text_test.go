package sync

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func ptr(s string) *string { return &s }

// Spec §2: editing a linked card's title or description is a tracker write under
// sync.policy.edit_text. A local edit queues edit_text with the edited fields only.
func TestLocalTextEditQueuesEditText(t *testing.T) {
	r := newRig(t)
	if err := r.st.SetSyncPolicy(store.OutboxEditText, store.PolicyAuto); err != nil {
		t.Fatal(err)
	}
	c := r.linked("70", "To Do", CategoryTodo)
	r.flush() // the engine's first pass sets the outbox cursor
	if rows := r.outbox(store.OutboxEditText); len(rows) != 0 {
		t.Fatalf("sync's own import queued an edit: %+v", rows)
	}
	if _, err := r.st.UpdateCard(c.ID, store.CardUpdate{Title: ptr("Better title")}, "tester"); err != nil {
		t.Fatal(err)
	}
	rep := r.flush()
	if rep.Queued != 1 || rep.Sent != 1 {
		t.Fatalf("%+v", rep)
	}
	rows := r.outbox(store.OutboxEditText)
	if len(rows) != 1 || rows[0].Status != store.OutboxSent {
		t.Fatalf("%+v", rows)
	}
	var p editPayload
	if err := json.Unmarshal(rows[0].Payload, &p); err != nil || p.Title == nil || *p.Title != "Better title" || p.Body != nil {
		t.Fatalf("payload %s", rows[0].Payload)
	}
	if len(r.fake.updates) != 1 || r.fake.updates[0] != "70 title=Better title body=-" {
		t.Fatalf("%v", r.fake.updates)
	}
}

func TestEditTextIgnoresTagsUnlinkedAndSyncEdits(t *testing.T) {
	r := newRig(t)
	if err := r.st.SetSyncPolicy(store.OutboxEditText, store.PolicyAuto); err != nil {
		t.Fatal(err)
	}
	c := r.linked("71", "To Do", CategoryTodo)
	r.flush()
	local, err := r.st.AddCard(store.NewCard{Title: "Local only"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	tags := []string{"agent-ready"}
	if _, err := r.st.UpdateCard(c.ID, store.CardUpdate{Tags: &tags}, "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.st.UpdateCard(local.ID, store.CardUpdate{Body: ptr("new body")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.st.UpdateCard(c.ID, store.CardUpdate{Body: ptr("from the tracker")}, ActorSync); err != nil {
		t.Fatal(err)
	}
	if rep := r.flush(); rep.Queued != 0 {
		t.Fatalf("%+v", rep)
	}
	if len(r.fake.updates) != 0 {
		t.Fatalf("%v", r.fake.updates)
	}
}

// The default policy is ask: the decision shows the redacted text, and edits made before it is
// asked collapse into one row with the newest text.
func TestEditTextAskShowsRedactedTextAndCoalesces(t *testing.T) {
	r := newRig(t)
	c := r.linked("72", "To Do", CategoryTodo)
	r.flush()
	if _, err := r.st.UpdateCard(c.ID, store.CardUpdate{Body: ptr("first draft")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.st.UpdateCard(c.ID, store.CardUpdate{Body: ptr("mail tester@acme.example for access")}, "tester"); err != nil {
		t.Fatal(err)
	}
	rep := r.flush()
	if rep.Asked != 1 || rep.Sent != 0 {
		t.Fatalf("%+v", rep)
	}
	var pending []store.OutboxRow
	for _, o := range r.outbox(store.OutboxEditText) {
		if o.Status == store.OutboxPending {
			pending = append(pending, o)
		}
	}
	if len(pending) != 1 || pending[0].DecisionID == "" {
		t.Fatalf("%+v", r.outbox(store.OutboxEditText))
	}
	d, err := r.st.Decision(pending[0].DecisionID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d.Text, "tester@acme.example") || !strings.Contains(d.Text, "for access") {
		t.Fatalf("decision text not redacted or missing the body: %q", d.Text)
	}
	if _, err := r.st.AnswerHub(d.ID, "send", "", "hub:tester"); err != nil {
		t.Fatal(err)
	}
	if rep := r.flush(); rep.Sent != 1 {
		t.Fatalf("%+v", rep)
	}
	if len(r.fake.updates) != 1 || strings.Contains(r.fake.updates[0], "tester@acme.example") ||
		!strings.Contains(r.fake.updates[0], "for access") || !strings.Contains(r.fake.updates[0], "title=-") {
		t.Fatalf("%v", r.fake.updates)
	}
}
