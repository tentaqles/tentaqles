package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

func runTQStdin(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	return runTQIn(t, strings.NewReader(stdin), args...)
}

// fallbackWS is syncWS whose credential chain has nothing (the MCP fallback's situation).
func fallbackWS(t *testing.T) {
	t.Helper()
	syncWS(t)
	syncAdapterHook = func(context.Context, *resolve.Workspace) (hubsync.Adapter, error) { return nil, cred.ErrNeedsSession }
}

func TestHubSyncImportPendingAckAsAgent(t *testing.T) {
	fallbackWS(t)
	t.Setenv("CLAUDECODE", "1")
	in := `{"statuses":[{"name":"Todo","category":"todo"},{"name":"In Progress","category":"active"},{"name":"Done","category":"done"}],
	"items":[{"id":"7","title":"Export invoices","status":"Todo","status_category":"todo","url":"https://tracker.example/acme/7"},
	{"id":"8","title":"Started elsewhere","status":"In Progress","status_category":"active"}]}`
	out, errOut, err := runTQStdin(t, in, "hub", "sync", "import", "-", "--json")
	if err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	var rep hubsync.ImportReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil || rep.Created != 2 {
		t.Fatalf("%v %s", err, out)
	}
	if rep.Items[1].Lane != "backlog" {
		t.Fatalf("an agent import lands outside planning lanes: %+v", rep.Items[1])
	}

	// The human starts the card (a move_on_start, auto) and pushes a local one (create_card, ask).
	unsetenv(t, "CLAUDECODE")
	cardID := rep.Items[0].CardID
	if _, errOut, err := runTQStdin(t, "", "hub", "move", cardID, "in progress"); err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	if out, errOut, err := runTQStdin(t, "", "hub", "add", "Local card", "--json"); err != nil {
		t.Fatalf("%v %s", err, errOut)
	} else {
		var c store.Card
		if err := json.Unmarshal([]byte(out), &c); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		if _, errOut, err := runTQStdin(t, "", "hub", "push", c.ID); err != nil {
			t.Fatalf("%v %s", err, errOut)
		}
	}

	t.Setenv("CLAUDECODE", "1")
	out, errOut, err = runTQStdin(t, "", "hub", "sync", "pending", "--json")
	if err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	var rows []hubsync.Sendable
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var moveID string
	for _, r := range rows {
		if r.Action == store.OutboxCreateCard {
			t.Fatalf("an unanswered ask row is listed as sendable: %+v", r)
		}
		if r.Action == store.OutboxMoveOnStart && r.ExternalID == "7" {
			moveID = r.ID
		}
	}
	if moveID == "" {
		t.Fatalf("no move_on_start row: %s", out)
	}
	_, out, _, _ = runTQ(t, "hub", "sync", "outbox", "--json", "--status", "pending")
	var pend []store.OutboxRow
	_ = json.Unmarshal([]byte(out), &pend)
	var createID string
	for _, r := range pend {
		if r.Action == store.OutboxCreateCard {
			createID = r.ID
		}
	}
	if createID == "" {
		t.Fatalf("no create row: %s", out)
	}
	if _, _, err := runTQStdin(t, "", "hub", "sync", "ack", createID, "--sent", "--item-id", "900"); err == nil || !strings.Contains(err.Error(), "approved") {
		t.Fatalf("an agent acked an ask row the human did not approve: %v", err)
	}
	if _, _, err := runTQStdin(t, "", "hub", "sync", "ack", moveID); err == nil {
		t.Fatal("ack without --sent or --failed")
	}
	if _, _, err := runTQStdin(t, "", "hub", "sync", "ack", moveID, "--sent", "--failed", "x"); err == nil {
		t.Fatal("ack with both --sent and --failed")
	}
	out, errOut, err = runTQStdin(t, "", "hub", "sync", "ack", moveID, "--sent", "--status", "In Progress")
	if err != nil || !strings.Contains(out, "is sent") {
		t.Fatalf("%v %s %s", err, out, errOut)
	}
}

func TestHubSyncImportHumanTextOutput(t *testing.T) {
	fallbackWS(t)
	out, errOut, err := runTQStdin(t, `[{"id":"9","title":"Plain","status_category":"todo"}]`, "hub", "sync", "import")
	if err != nil || !strings.Contains(out, "created 1") {
		t.Fatalf("%v %q %s", err, out, errOut)
	}
	if _, _, err := runTQStdin(t, `{"items":[{"title":"no id"}]}`, "hub", "sync", "import"); err == nil {
		t.Fatal("an item without an id is refused")
	}
}
