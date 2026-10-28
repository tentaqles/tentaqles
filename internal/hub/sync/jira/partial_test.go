package jira

import (
	"errors"
	"testing"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// Once the POST made the issue, a failing status transition or read-back is a PartialCreateError
// carrying the created issue, so the engine links it and never creates it again.
func TestCreateAfterPostFailsIsPartial(t *testing.T) {
	cases := map[string]func(f *fakeJira){
		// no transitions route: the status step 404s
		"status transition fails": func(f *fakeJira) {},
		// transitions work, the read-back 404s
		"read-back fails": func(f *fakeJira) {
			f.json("GET", "/rest/api/3/issue/ACME-10/transitions", 200, `{"transitions":[{"id":"11","name":"Start","to":{"name":"In Progress"}}]}`)
			f.json("POST", "/rest/api/3/issue/ACME-10/transitions", 204, ``)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeJira(t)
			f.json("POST", "/rest/api/3/issue", 201, `{"id":"10010","key":"ACME-10"}`)
			setup(f)
			a := newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
			_, err := a.Create(ctx, hubsync.NewItem{Title: "Top", Status: "In Progress"})
			var pc *hubsync.PartialCreateError
			if !errors.As(err, &pc) {
				t.Fatalf("want a PartialCreateError, got %v", err)
			}
			it, ok := hubsync.CreatedItem(err)
			if !ok || it.ID != "ACME-10" || it.Key != "ACME-10" || it.URL != f.srv.URL+"/browse/ACME-10" || it.Title != "Top" {
				t.Fatalf("CreatedItem %+v %v", it, ok)
			}
			posts := 0
			for _, c := range f.seen() {
				if c.Method == "POST" && c.Path == "/rest/api/3/issue" {
					posts++
				}
			}
			if posts != 1 {
				t.Fatalf("posted %d times", posts)
			}
		})
	}
}

// A failed POST is a plain error: nothing was created.
func TestCreatePostFailsIsPlain(t *testing.T) {
	f := newFakeJira(t)
	f.json("POST", "/rest/api/3/issue", 400, `{"errorMessages":["bad"],"errors":{}}`)
	a := newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
	_, err := a.Create(ctx, hubsync.NewItem{Title: "Top"})
	if err == nil {
		t.Fatal("want an error")
	}
	if _, ok := hubsync.CreatedItem(err); ok {
		t.Fatalf("a failed POST is not a partial create: %v", err)
	}
}
