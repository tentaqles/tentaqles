package web

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// doneLane is the done lane's markup on a board page (the last lane, up to </section>).
func doneLane(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `data-kind="done"`)
	j := strings.Index(body, `</section>`)
	if i < 0 || j < i {
		t.Fatalf("no done lane:\n%s", body)
	}
	return body[i:j]
}

// doneCards adds n cards straight to done; card i was last updated at minute i (higher is newer).
func doneCards(t *testing.T, st *store.Store, n int) []store.Card {
	t.Helper()
	var out []store.Card
	for i := 0; i < n; i++ {
		c, err := st.AddCard(store.NewCard{Title: fmt.Sprintf("Shipped %02d", i), Lane: "done"}, "tester")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.DB.Exec(`UPDATE cards SET updated_at = ? WHERE id = ?`, fmt.Sprintf("2026-10-01T10:%02d:00Z", i), c.ID); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func TestBoardDoneLaneCapsAtTheNewest(t *testing.T) {
	e := newEnv(t, "acme")
	st := e.svcs["acme"].Store()
	cards := doneCards(t, st, doneCap+5)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/?ws=acme", nil)
	lane := doneLane(t, body)
	if n := strings.Count(lane, `data-card="`); n != doneCap {
		t.Fatalf("done lane shows %d cards, want %d", n, doneCap)
	}
	for i, c := range cards {
		shown := strings.Contains(lane, `data-card="`+c.ID+`"`)
		if want := i >= 5; shown != want {
			t.Errorf("card %d (updated minute %d): shown %v, want %v", i, i, shown, want)
		}
	}
	if !strings.Contains(lane, fmt.Sprintf(`Done <span class="c">%d</span>`, doneCap+5)) {
		t.Errorf("the lane count is the true total:\n%s", lane)
	}
	if !strings.Contains(lane, `data-hidden="5"`) {
		t.Errorf("the lane tells the live count how many cards it hides:\n%s", lane)
	}
	if !strings.Contains(lane, `href="/?ws=acme&done=all"`) || !strings.Contains(lane, fmt.Sprintf(`Show all %d done`, doneCap+5)) {
		t.Errorf("a capped done lane links to the full lane:\n%s", lane)
	}
	assertCSPSafe(t, "/", body)

	_, all, _ := get(t, ts, "/?ws=acme&done=all", nil)
	lane = doneLane(t, all)
	if n := strings.Count(lane, `data-card="`); n != doneCap+5 {
		t.Fatalf("?done=all shows %d cards, want %d", n, doneCap+5)
	}
	if strings.Contains(lane, `Show all`) || strings.Contains(lane, `data-hidden`) {
		t.Errorf("the full lane has no show-all link:\n%s", lane)
	}

	// The live refresh can still read a done card the cap hides.
	code, tile, _ := get(t, ts, "/cards/"+cards[0].ID+"/tile?ws=acme", nil)
	if code != 200 || !strings.Contains(tile, `data-card="`+cards[0].ID+`"`) || !strings.Contains(tile, `data-lane="done"`) {
		t.Fatalf("tile of a hidden done card: %d %s", code, tile)
	}
}

func TestBoardDoneLaneAtTheCapHasNoLink(t *testing.T) {
	e := newEnv(t, "acme")
	doneCards(t, e.svcs["acme"].Store(), doneCap)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/?ws=acme", nil)
	lane := doneLane(t, body)
	if n := strings.Count(lane, `data-card="`); n != doneCap || strings.Contains(lane, "Show all") || strings.Contains(lane, "data-hidden") {
		t.Fatalf("a lane at the cap shows every card and no link (%d cards):\n%s", n, lane)
	}
}

func TestBoardShowsPRLinksFromTheBatchedQuery(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	st := svc.Store()
	ctx := context.Background()
	rv, _ := svc.Add(ctx, store.NewCard{Title: "In review", Lane: "review"}, nil)
	dn, _ := svc.Add(ctx, store.NewCard{Title: "Shipped", Lane: "done"}, nil)
	bl, _ := svc.Add(ctx, store.NewCard{Title: "Backlog"}, nil)
	st.AddComment(rv.ID, store.AuthorAgent, "agent", "PR https://example.test/o/r/pull/11", "")
	st.AddComment(dn.ID, store.AuthorAgent, "agent", "PR https://example.test/o/r/pull/12", "")
	st.AddComment(bl.ID, store.AuthorAgent, "agent", "PR https://example.test/o/r/pull/13", "")
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/?ws=acme", nil)
	for _, want := range []string{`href="https://example.test/o/r/pull/11"`, `href="https://example.test/o/r/pull/12"`} {
		if !strings.Contains(body, want) {
			t.Errorf("board lacks %q", want)
		}
	}
	if strings.Contains(body, `pull/13`) {
		t.Error("a backlog card shows no PR link")
	}
}

// countingConnector opens the store's database through the real driver and records every
// statement the board prepares. The wrapper exposes only driver.Conn, so database/sql prepares
// each query and the count sees all of them.
type countingConnector struct {
	drv driver.Driver
	dsn string
	mu  sync.Mutex
	qs  []string
}

func (c *countingConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.drv.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return countingConn{conn, c}, nil
}
func (c *countingConnector) Driver() driver.Driver { return c.drv }

func (c *countingConnector) reset() { c.mu.Lock(); c.qs = nil; c.mu.Unlock() }

func (c *countingConnector) count(substr string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, q := range c.qs {
		if strings.Contains(q, substr) {
			n++
		}
	}
	return n
}

type countingConn struct {
	driver.Conn
	c *countingConnector
}

func (k countingConn) Prepare(q string) (driver.Stmt, error) {
	k.c.mu.Lock()
	k.c.qs = append(k.c.qs, q)
	k.c.mu.Unlock()
	return k.Conn.Prepare(q)
}

func TestBoardReadsRunsAndCommentsOnceForManyCards(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	st := svc.Store()
	ctx := context.Background()
	lanes := []string{"backlog", "todo", "review", "done"}
	for i := 0; i < 50; i++ {
		c, err := svc.Add(ctx, store.NewCard{Title: fmt.Sprintf("Card %02d", i), Lane: lanes[i%len(lanes)]}, nil)
		if err != nil {
			t.Fatal(err)
		}
		r, err := st.CreateRun(store.Run{CardID: c.ID, Adapter: store.AdapterClaude, OnRamp: store.OnRampFeature})
		if err != nil {
			t.Fatal(err)
		}
		r.Status, r.CostUSD = store.StatusFinished, 0.25
		st.SaveRun(r)
		st.AddComment(c.ID, store.AuthorAgent, "agent", fmt.Sprintf("PR https://example.test/o/r/pull/%d", i), "")
	}
	cc := &countingConnector{drv: st.DB.Driver(), dsn: store.DBPath("acme") + "?_pragma=busy_timeout(5000)"}
	counted := sql.OpenDB(cc)
	orig := st.DB
	st.DB = counted
	t.Cleanup(func() { st.DB = orig; counted.Close() })

	ts := e.server(Options{})
	cc.reset()
	code, body, _ := get(t, ts, "/?ws=acme", nil)
	if code != 200 || strings.Count(body, `data-card="`) != 50 {
		t.Fatalf("board %d with %d cards", code, strings.Count(body, `data-card="`))
	}
	// The page shell has its own fixed reads (the nav's waiting count, active runs, settings);
	// what must not grow with the card count is the per-card work.
	if n := cc.count("FROM runs WHERE card_id"); n != 1 {
		t.Errorf("one board render ran %d per-card runs queries, want 1", n)
	}
	if n := cc.count("FROM comments"); n != 1 {
		t.Errorf("one board render ran %d comments queries, want 1", n)
	}
	if n := cc.count("FROM decisions"); n > 2 {
		t.Errorf("one board render ran %d decisions queries, want at most 2 (shell and board)", n)
	}
	if n := cc.count("FROM settings"); n > 2 {
		t.Errorf("one board render read settings %d times, want at most 2 (shell and board)", n)
	}
	if n := len(cc.qs); n > 12 {
		t.Errorf("one board render ran %d queries for 50 cards:\n%s", n, strings.Join(cc.qs, "\n"))
	}
}
