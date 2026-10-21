package store

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestCheckoutIsAtomicAcrossConnections(t *testing.T) {
	isolate(t)
	s1 := mustOpen(t, "acme")
	s2 := mustOpen(t, "acme") // a second process: its own connection pool on the same file
	c, _ := s1.AddCard(NewCard{Title: "a"}, "tester")
	var wins int32
	var winner atomic.Value
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st := s1
			if i%2 == 1 {
				st = s2
			}
			run := NewID("r")
			ok, err := st.Checkout(c.ID, run)
			if err != nil {
				t.Error(err)
				return
			}
			if ok {
				atomic.AddInt32(&wins, 1)
				winner.Store(run)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d checkouts won, want exactly 1", wins)
	}
	got, _ := s1.Card(c.ID)
	if got.LockedByRun != winner.Load().(string) {
		t.Fatalf("locked by %q, winner %q", got.LockedByRun, winner.Load())
	}
	if err := s1.Release(c.ID, "r-notthewinner"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s1.Card(c.ID); got.LockedByRun == "" {
		t.Fatal("release by another run must not unlock")
	}
	s1.Release(c.ID, winner.Load().(string))
	if got, _ := s1.Card(c.ID); got.LockedByRun != "" {
		t.Fatal("release by the owner unlocks")
	}
}
