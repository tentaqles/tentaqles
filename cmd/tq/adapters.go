package main

// Tracker adapters register themselves with the sync registry (and their cli credential hook)
// from init, so the binary links them in here. Keep one import per adapter package.
import (
	_ "github.com/tentaqles/tentaqles/internal/hub/sync/jira"
	_ "github.com/tentaqles/tentaqles/internal/hub/sync/linear"
)
