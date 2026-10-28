package azdo

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
)

// manyChildren gives item 1 n tasks (ids 1001..1000+n), all changed long before since, so
// they reach ListChanged only through the children-of-a-parent link query.
func manyChildren(f *fakeADO, n int) {
	for i := 0; i < n; i++ {
		id := 1001 + i
		f.addItem(id, 1, "2026-10-01T09:00:00Z", map[string]any{
			"System.Title": fmt.Sprintf("task %d", id), "System.WorkItemType": "Task", "System.State": "New", "System.Parent": 1,
		})
		f.links = append(f.links, [2]int{1, id})
	}
}

func TestListChangedLinkQueriesAreNeverTruncated(t *testing.T) {
	for _, since := range []time.Time{{}, time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)} {
		f, srv := newFake(t)
		seed(f)
		// 600 children: 1200 link rows (a source row and a link row each), past the flat $top.
		manyChildren(f, 600)
		items, _, err := newAdapter(t, srv).ListChanged(context.Background(), since)
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		for _, it := range items {
			ids[it.ID] = true
		}
		for i := 0; i < 600; i++ {
			if id := fmt.Sprint(1001 + i); !ids[id] {
				t.Fatalf("since %v: child %s dropped (%d items)", since, id, len(items))
			}
		}
		for i, q := range f.queries {
			if strings.Contains(q, "WorkItemLinks") && strings.Contains(f.wiqlURLs[i], "%24top") {
				t.Fatalf("link query carries $top: %s", f.wiqlURLs[i])
			}
		}
		if !strings.Contains(f.wiqlURLs[0], "%24top=1000") {
			t.Fatalf("flat query without $top: %s", f.wiqlURLs[0])
		}
	}
}

func TestListChangedFirstSyncReadsChildrenPerParent(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	if _, _, err := newAdapter(t, srv).ListChanged(context.Background(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	// No unbounded "every child of everything ever assigned to me" query on the first sync:
	// children come per parent, by id.
	for _, q := range f.queries {
		if strings.Contains(q, "WorkItemLinks") && !strings.Contains(q, "[Source].[System.Id] IN (") {
			t.Fatalf("unbounded link query on first sync: %s", q)
		}
	}
}

func TestListChangedCappedFlatMovesTheMarkOnlyToItsOwnItems(t *testing.T) {
	f, srv := newFake(t)
	base := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	// 1001 assigned items changed one second apart; the flat query returns the first 1000.
	for i := 0; i < wiqlTop+1; i++ {
		id := 2000 + i
		f.addItem(id, 1, base.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano), map[string]any{
			"System.Title": "x", "System.WorkItemType": "Task", "System.State": "New",
			"System.AssignedTo": map[string]any{"uniqueName": "tester@acme.test"},
		})
		f.flat = append(f.flat, id)
	}
	// A child of the first item that changed after all of them.
	f.addItem(9000, 1, "2026-10-28T08:00:00Z", map[string]any{"System.Title": "late", "System.WorkItemType": "Task", "System.State": "New"})
	f.links = [][2]int{{2000, 9000}}
	items, mark, err := newAdapter(t, srv).ListChanged(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if want := base.Add(time.Duration(wiqlTop-1) * time.Second); !mark.Equal(want) {
		t.Fatalf("mark %v, want the newest flat item %v", mark, want)
	}
	if len(items) != wiqlTop+1 { // 1000 flat items and the child
		t.Fatalf("items %d", len(items))
	}
}

func TestWIQLTimeKeepsMilliseconds(t *testing.T) {
	if got := wiqlTime(time.Date(2026, 10, 27, 8, 0, 5, 123456789, time.UTC)); got != "2026-10-27T08:00:05.123Z" {
		t.Fatalf("got %s", got)
	}
	if got := wiqlTime(time.Date(2026, 10, 27, 8, 0, 5, 0, time.FixedZone("x", 3600))); got != "2026-10-27T07:00:05Z" {
		t.Fatalf("got %s", got)
	}
}

func TestRateLimitDelayHoldsTheNextCallBriefly(t *testing.T) {
	for _, tc := range []struct {
		delay string
		want  time.Duration
	}{{"2.5", 2500 * time.Millisecond}, {"60", shortWait}} {
		f, srv := newFake(t)
		seed(f)
		f.hook = func(w http.ResponseWriter, r *http.Request) bool {
			if strings.HasSuffix(r.URL.Path, "/comments") {
				w.Header().Set("X-RateLimit-Delay", tc.delay)
			}
			return false
		}
		a := newAdapter(t, srv)
		var slept []time.Duration
		clock := time.Now()
		a.now = func() time.Time { return clock }
		a.sleep = func(ctx context.Context, d time.Duration) error {
			slept = append(slept, d)
			clock = clock.Add(d)
			return nil
		}
		if _, err := a.Get(context.Background(), "1"); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Get(context.Background(), "1"); err != nil {
			t.Fatalf("delay %s: %v", tc.delay, err)
		}
		if len(slept) != 1 || slept[0] != tc.want {
			t.Fatalf("delay %s: slept %v", tc.delay, slept)
		}
	}
}

func TestCreateWithoutAUniqueNameLeavesAssignedToUnset(t *testing.T) {
	f, srv := newFake(t)
	f.noAcct = true
	a := newAdapter(t, srv)
	acct, err := a.Check(context.Background())
	if err != nil || acct != "Tester" {
		t.Fatalf("check %q %v", acct, err)
	}
	if _, err := a.Create(context.Background(), hubsync.NewItem{Title: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, op := range f.created[0]["ops"].([]map[string]any) {
		if op["path"] == "/fields/System.AssignedTo" {
			t.Fatalf("assigned to a display name: %v", op["value"])
		}
	}
}
