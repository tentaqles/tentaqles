package cmd

import (
	"testing"
)

// tq hub serve hands the pages the same engine it runs (the Connections page reads its overview).
func TestSyncEnginesGivesThePagesTheEngine(t *testing.T) {
	syncWS(t)
	ws, err := hubWorkspace("acme")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := newHubService(ws, func(string) {}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Store().Close()
	r := syncEngines(func(string) {})(ws, svc)
	if r == nil {
		t.Fatal("no engine for a workspace with a tracker")
	}
	ov, err := r.Overview()
	if err != nil || ov.Workspace != "acme" || ov.Provider == "" {
		t.Fatalf("%+v %v", ov, err)
	}
	other := *ws
	other.Manifest.Tracker = nil
	if syncEngines(func(string) {})(&other, svc) != nil {
		t.Fatal("an engine for a workspace without a tracker")
	}
}
