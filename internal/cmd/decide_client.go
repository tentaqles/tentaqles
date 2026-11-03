package cmd

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// The Jev client of tq decide. The hook does not call Jev.

// decideDir holds the Jev cache, cost log and judgment log.
func decideDir() string { return filepath.Join(paths.Home(), "decide") }

func judgmentLog() *decide.JudgmentLog {
	return &decide.JudgmentLog{Path: filepath.Join(decideDir(), "judgments.jsonl")}
}

// jevClientFor builds a client for ws's policy with the shared cache and
// cost log, or an error when the backend is off or no key is found.
func jevClientFor(ws *resolve.Workspace, purpose string, timeout time.Duration) (*decide.Client, error) {
	cl, err := ws.Manifest.Decision.Client(filepath.Dir(ws.ManifestPath), timeout)
	if err != nil {
		return nil, err
	}
	cl.Cache = decide.NewCache(filepath.Join(decideDir(), "cache"))
	cl.Log = &decide.Log{Path: filepath.Join(decideDir(), "log.jsonl")}
	cl.Purpose = purpose
	cl.Workspace = ws.Name
	return cl, nil
}

func policyMode(p decide.Policy) string {
	if p.Enforcing() {
		return "enforce"
	}
	return "shadow"
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
