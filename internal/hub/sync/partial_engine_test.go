package sync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// A Create that made the item and then failed a later step links the card once, marks the row
// sent with a note, and never creates the item again: not in retry (even when the later step was
// rate-limited) and not on a second pass.
func TestPartialCreateLinksOnceAndNeverCreatesAgain(t *testing.T) {
	cases := map[string]error{
		"later step fails":         errors.New("transition: 500"),
		"later step rate-limited":  &RateLimitError{Provider: "github", RetryAfter: time.Second},
		"wrapped rate limit cause": fmt.Errorf("parent link: %w", &RateLimitError{Provider: "github", RetryAfter: time.Second}),
	}
	for name, cause := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			if err := r.st.SetSyncPolicy(store.OutboxCreateCard, store.PolicyAuto); err != nil {
				t.Fatal(err)
			}
			r.fake.afterCreate = cause
			kid, err := r.st.AddCard(store.NewCard{Title: "Write docs", Body: "body"}, "tester")
			if err != nil {
				t.Fatal(err)
			}
			row, err := r.eng.Push(context.Background(), kid.ID)
			if err != nil {
				t.Fatal(err)
			}
			rep, err := r.eng.Flush(context.Background())
			if err != nil {
				t.Fatalf("flush: %v (%+v)", err, rep)
			}
			if rep.Sent != 1 {
				t.Fatalf("sent once: %+v", rep)
			}
			if len(r.fake.created) != 1 {
				t.Fatalf("created %d times: %v", len(r.fake.created), r.fake.callLog())
			}
			got, _ := r.st.Card(kid.ID)
			if got.ExternalProvider != "github" || got.ExternalID != "901" || got.ExternalURL == "" {
				t.Fatalf("not linked: %+v", got)
			}
			o, err := r.st.OutboxRow(row.ID)
			if err != nil {
				t.Fatal(err)
			}
			if o.Status != store.OutboxSent || !strings.Contains(o.LastError, "901") {
				t.Fatalf("row %+v", o)
			}
			if rep := r.flush(); rep.Sent != 0 {
				t.Fatalf("second pass sent: %+v", rep)
			}
			r.tick()
			if len(r.fake.created) != 1 {
				t.Fatalf("second pass created again: %v", r.fake.callLog())
			}
		})
	}
}
