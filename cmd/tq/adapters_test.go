package main

import (
	"slices"
	"testing"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// The binary must link every tracker adapter, or `tq hub sync` finds no provider.
func TestBinaryRegistersTrackerAdapters(t *testing.T) {
	got := hubsync.Registered()
	for _, want := range []string{"jira", "linear", "github", "azure-devops", "asana"} {
		if !slices.Contains(got, want) {
			t.Errorf("adapter %q not registered in the tq binary; registered: %v", want, got)
		}
	}
}

// Spec §7: the cli credential source exists for the trackers that have a CLI login
// (acli for Jira, gh for GitHub, az for Azure DevOps).
func TestBinaryRegistersTrackerCLICredHooks(t *testing.T) {
	for _, want := range []string{"jira", "github", "azure-devops"} {
		if _, ok := cred.CLIHookFor(want); !ok {
			t.Errorf("no cli credential hook registered for %q", want)
		}
	}
}
