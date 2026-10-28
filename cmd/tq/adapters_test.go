package main

import (
	"slices"
	"testing"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
)

// The binary must link every tracker adapter, or `tq hub sync` finds no provider.
func TestBinaryRegistersTrackerAdapters(t *testing.T) {
	got := hubsync.Registered()
	for _, want := range []string{"jira", "linear"} {
		if !slices.Contains(got, want) {
			t.Errorf("adapter %q not registered in the tq binary; registered: %v", want, got)
		}
	}
}
