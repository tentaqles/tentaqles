package sync

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// noSessionRig is a rig whose credential chain has nothing: the MCP fallback's situation.
func noSessionRig(t *testing.T) *rig {
	return newRig(t, func(c *EngineConfig) {
		c.Adapter = func(context.Context) (Adapter, error) { return nil, cred.ErrNeedsSession }
	})
}

func TestParseImportAcceptsArrayAndObject(t *testing.T) {
	in, err := ParseImport([]byte(`[{"id":"1","title":"a"}]`))
	if err != nil || len(in.Items) != 1 || in.Items[0].ID != "1" {
		t.Fatalf("%+v %v", in, err)
	}
	in, err = ParseImport([]byte(`{"items":[{"id":"2","title":"b","status_category":"todo"}],"watermark":"2026-10-27T12:00:00Z"}`))
	if err != nil || len(in.Items) != 1 || in.Watermark == "" || in.Items[0].StatusCategory != "todo" {
		t.Fatalf("%+v %v", in, err)
	}
	if _, err := ParseImport([]byte("  ")); err == nil {
		t.Fatal("empty input accepted")
	}
}

func TestAgentImportCreatesOnlyInPlanningLanes(t *testing.T) {
	r := noSessionRig(t)
	rep, err := r.eng.Import(ImportInput{Items: []ImportItem{
		{ID: "11", Title: "Todo item", Status: "To Do", StatusCategory: "todo", URL: "https://tracker.example/acme/11"},
		{ID: "12", Title: "Active item", Status: "In Progress", StatusCategory: "active"},
		{ID: "13", Title: "Child", Status: "To Do", StatusCategory: "todo", ParentID: "11"},
	}, Watermark: "2026-10-27T12:00:00Z"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Created != 3 {
		t.Fatalf("%+v", rep)
	}
	if c := r.card("github", "11"); c.Lane != "todo" || c.ExternalURL == "" || c.ExternalLane != "To Do" {
		t.Fatalf("todo item: %+v", c)
	}
	active := r.card("github", "12")
	if active.Lane != "backlog" || active.ExternalLane != "In Progress" {
		t.Fatalf("an agent never creates into an active lane: %+v", active)
	}
	if !strings.Contains(rep.Items[1].Note, "backlog") {
		t.Fatalf("no note for the held lane: %+v", rep.Items[1])
	}
	if c := r.card("github", "13"); c.ParentID != r.card("github", "11").ID {
		t.Fatalf("child not under its parent: %+v", c)
	}
	st, _, _ := r.st.SyncState("github", r.eng.Scope())
	if st.Watermark != "2026-10-27T12:00:00Z" || st.LastOKAt == "" {
		t.Fatalf("state %+v", st)
	}
	// No run starts from a sync move: nothing was queued.
	if runs, _ := r.st.RunsWithStatus("queued", "running"); len(runs) != 0 {
		t.Fatalf("runs %+v", runs)
	}
}

func TestAgentImportHoldsMovesOutOfPlanningLanes(t *testing.T) {
	r := noSessionRig(t)
	if _, err := r.eng.Import(ImportInput{Items: []ImportItem{{ID: "21", Title: "Item", Status: "To Do", StatusCategory: "todo"}}}, true); err != nil {
		t.Fatal(err)
	}
	// planning -> planning moves; planning -> active is held.
	rep, err := r.eng.Import(ImportInput{Items: []ImportItem{{ID: "21", Title: "Item renamed", Status: "In Progress", StatusCategory: "active"}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	c := r.card("github", "21")
	if rep.Held != 1 || rep.Items[0].Result != "held" || c.Lane != "todo" || c.ExternalLane != "To Do" {
		t.Fatalf("rep %+v card %+v", rep, c)
	}
	if c.Title != "Item renamed" {
		t.Fatalf("the text still refreshes: %q", c.Title)
	}
	// A human import makes the move.
	rep, err = r.eng.Import(ImportInput{Items: []ImportItem{{ID: "21", Title: "Item renamed", Status: "In Progress", StatusCategory: "active"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if c := r.card("github", "21"); c.Lane != "in progress" || rep.Moved != 1 {
		t.Fatalf("rep %+v card %+v", rep, c)
	}
}

func TestImportRefusesItemsWithoutID(t *testing.T) {
	r := noSessionRig(t)
	rep, err := r.eng.Import(ImportInput{Items: []ImportItem{{Title: "no id"}, {ID: "31", Title: "ok", StatusCategory: "todo"}}}, true)
	if err == nil || rep.Refused != 1 || rep.Created != 1 {
		t.Fatalf("rep %+v err %v", rep, err)
	}
	if st, ok, _ := r.st.SyncState("github", r.eng.Scope()); ok && st.LastOKAt != "" {
		t.Fatal("a partly refused import is not a good poll")
	}
}

// The fallback loop: the board moves a linked card (move_on_start, auto) and pushes a local card
// (create_card, ask). Sendable lists only the auto row until the human answers send.
func TestSendableAndAgentAck(t *testing.T) {
	r := noSessionRig(t)
	sts := []ImportStatus{{"To Do", "todo"}, {"In Progress", "active"}, {"In Review", "review"}, {"Done", "done"}}
	if _, err := r.eng.Import(ImportInput{Statuses: sts, Items: []ImportItem{{ID: "41", Title: "Item", Status: "To Do", StatusCategory: "todo"}}}, true); err != nil {
		t.Fatal(err)
	}
	r.flush() // starts the activity cursor; the adapter error is only for sending
	c := r.card("github", "41")
	if _, err := r.st.MoveCard(c.ID, "in progress", "tester"); err != nil {
		t.Fatal(err)
	}
	local, err := r.st.AddCard(storeNewCard("Push me", "mail tester@acme.example"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	push, err := r.eng.Push(context.Background(), local.ID)
	if err != nil {
		t.Fatal(err)
	}

	rows, rep, err := r.eng.Sendable()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Action != "move_on_start" || rows[0].ExternalID != "41" || rows[0].BaseStatus != "To Do" ||
		!containsFold(rows[0].Statuses, "In Progress") || rep.Asked != 1 {
		t.Fatalf("rows %+v rep %+v", rows, rep)
	}
	// The agent may not ack the ask row before the human answers.
	if _, err := r.eng.Ack(push.ID, AckInput{Sent: true, ItemID: "999"}, true); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("ack of an unanswered ask: %v", err)
	}
	moved, err := r.eng.Ack(rows[0].ID, AckInput{Sent: true, Status: "In Progress"}, true)
	if err != nil || moved.Status != "sent" {
		t.Fatalf("%+v %v", moved, err)
	}
	if c := r.card("github", "41"); c.ExternalLane != "In Progress" {
		t.Fatalf("the move's status is recorded: %+v", c)
	}
	if _, err := r.eng.Ack(rows[0].ID, AckInput{Sent: true}, true); err == nil {
		t.Fatal("a sent row was acked twice")
	}

	// The human answers send: the create row becomes sendable, with redacted text.
	row, _ := r.st.OutboxRow(push.ID)
	if _, err := r.st.AnswerHub(row.DecisionID, "send", "", "tester"); err != nil {
		t.Fatal(err)
	}
	rows, _, err = r.eng.Sendable()
	if err != nil || len(rows) != 1 || rows[0].Action != "create_card" || !rows[0].Approved || rows[0].Body == nil {
		t.Fatalf("rows %+v err %v", rows, err)
	}
	if strings.Contains(*rows[0].Body, "tester@acme.example") {
		t.Fatalf("outbound text is not redacted: %q", *rows[0].Body)
	}
	if _, err := r.eng.Ack(push.ID, AckInput{Sent: true}, true); err == nil {
		t.Fatal("a create ack needs the new item id")
	}
	sent, err := r.eng.Ack(push.ID, AckInput{Sent: true, ItemID: "950", URL: "https://tracker.example/acme/950"}, true)
	if err != nil || sent.Status != "sent" {
		t.Fatalf("%+v %v", sent, err)
	}
	if c := r.card("github", "950"); c.ID != local.ID {
		t.Fatalf("the pushed card is linked: %+v", c)
	}
}

func TestAgentAckFailedAndPolicyOff(t *testing.T) {
	r := noSessionRig(t)
	if _, err := r.eng.Import(ImportInput{Items: []ImportItem{{ID: "51", Title: "Item", Status: "To Do", StatusCategory: "todo"}}}, true); err != nil {
		t.Fatal(err)
	}
	c := r.card("github", "51")
	auto, err := r.st.EnqueueOutbox(c.ID, "comment_results", map[string]any{"text": "done"})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := r.eng.Ack(auto.ID, AckInput{Why: "connector returned 403"}, true)
	if err != nil || failed.Status != "failed" || !strings.Contains(failed.LastError, "403") {
		t.Fatalf("%+v %v", failed, err)
	}
	if err := r.st.SetSyncPolicy("edit_text", "off"); err != nil {
		t.Fatal(err)
	}
	off, err := r.st.EnqueueOutbox(c.ID, "edit_text", map[string]any{"title": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.eng.Ack(off.ID, AckInput{Sent: true}, true); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("an agent acked a row whose policy is off: %v", err)
	}
	// A human may ack it (they wrote it by hand).
	if got, err := r.eng.Ack(off.ID, AckInput{Sent: true}, false); err != nil || got.Status != "sent" {
		t.Fatalf("%+v %v", got, err)
	}
}

func storeNewCard(title, body string) store.NewCard { return store.NewCard{Title: title, Body: body} }
