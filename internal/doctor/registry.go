package doctor

import (
	"fmt"
	"sort"
	"sync"

	"github.com/tentaqles/tentaqles/internal/resolve"
)

// AddFunc records one finding.
type AddFunc func(level, code, ws, msg, fix string)

// Check is a doctor check registered from its own file (spec E §17), so each
// workstream adds checks without editing Run. Either func may be nil.
// Checks must stay cheap: no network, no subprocess (RunForCwd backs the
// hook path; Run calls these only from tq doctor).
type Check struct {
	// Name is unique, e.g. "launcher" or "codex".
	Name string
	// Workspace runs once per trusted workspace.
	Workspace func(w *resolve.Workspace, d Deps, add AddFunc)
	// Global runs once per Run, after the workspace loop.
	Global func(d Deps, add AddFunc)
}

var (
	checksMu sync.Mutex
	checks   = map[string]Check{}
)

// Register adds c. Call it from an init func. It panics on an empty or
// duplicate name.
func Register(c Check) {
	checksMu.Lock()
	defer checksMu.Unlock()
	if c.Name == "" {
		panic("doctor.Register: check without name")
	}
	if _, dup := checks[c.Name]; dup {
		panic(fmt.Sprintf("doctor.Register: duplicate check %s", c.Name))
	}
	checks[c.Name] = c
}

// Registered returns the registered checks sorted by name.
func Registered() []Check {
	checksMu.Lock()
	defer checksMu.Unlock()
	out := make([]Check, 0, len(checks))
	for _, c := range checks {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// unregister removes a check (tests only).
func unregister(name string) {
	checksMu.Lock()
	defer checksMu.Unlock()
	delete(checks, name)
}

func runWorkspaceChecks(w *resolve.Workspace, d Deps, add AddFunc) {
	for _, c := range Registered() {
		if c.Workspace != nil {
			c.Workspace(w, d, add)
		}
	}
}

func runGlobalChecks(d Deps, add AddFunc) {
	for _, c := range Registered() {
		if c.Global != nil {
			c.Global(d, add)
		}
	}
}
