package linear

import (
	"context"
	"errors"
	"testing"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
)

// issueCreate succeeded but a field of the read-back selection errored: GraphQL returns the data
// and the errors together. The issue exists, so Create is a PartialCreateError carrying it.
func TestCreateWithPartialDataIsPartial(t *testing.T) {
	f, srv := newFake(t)
	f.on("LinearIssueCreate", reply{Body: `{"data":{"issueCreate":{"success":true,"issue":` +
		issueJSON(childID, "ACME-9", "Child", "2026-10-27T09:00:00.000Z", "") + `}},` +
		`"errors":[{"message":"assignee could not be loaded","path":["issueCreate","issue","assignee"],"extensions":{"code":"INTERNAL_SERVER_ERROR"}}]}`})
	a := newAdapter(t, srv, map[string]string{"team": teamID}, "me")
	_, err := a.Create(context.Background(), hubsync.NewItem{Title: "Child"})
	var pc *hubsync.PartialCreateError
	if !errors.As(err, &pc) {
		t.Fatalf("want a PartialCreateError, got %v", err)
	}
	it, ok := hubsync.CreatedItem(err)
	if !ok || it.ID != childID || it.Key != "ACME-9" || it.URL != "https://linear.app/acme/issue/ACME-9" {
		t.Fatalf("CreatedItem %+v %v", it, ok)
	}
	n := 0
	for _, op := range f.ops() {
		if op == "LinearIssueCreate" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("issueCreate sent %d times", n)
	}
}

// Errors with no issue in the data: nothing was created, a plain error.
func TestCreateErrorWithoutIssueIsPlain(t *testing.T) {
	f, srv := newFake(t)
	f.on("LinearIssueCreate", reply{Body: `{"data":{"issueCreate":null},"errors":[{"message":"title too long","extensions":{"code":"BAD_USER_INPUT"}}]}`})
	a := newAdapter(t, srv, map[string]string{"team": teamID}, "me")
	_, err := a.Create(context.Background(), hubsync.NewItem{Title: "Child"})
	if err == nil {
		t.Fatal("want an error")
	}
	if _, ok := hubsync.CreatedItem(err); ok {
		t.Fatalf("not a partial create: %v", err)
	}
}
