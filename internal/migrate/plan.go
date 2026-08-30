package migrate

import (
	"fmt"
	"sort"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/registry"
)

// Change is one thing a step would do (dry run) or did (apply).
//
// Kind is a short verb the command layer prints verbatim, e.g. "move-dir",
// "make-link", "unset-global", "remove-includeif", "rewrite-ws-file",
// "adopt-hook", "clear-autorun". Danger marks a change the user should read
// twice before applying — anything that moves or deletes real data.
type Change struct {
	Step   string
	Kind   string
	Path   string
	Detail string
	Danger bool
}

// Plan is what a step intends to do. Warnings are things the user should know
// but that do not stop the step; Skipped records what the step deliberately
// left alone, each entry carrying its own reason.
type Plan struct {
	Changes  []Change
	Warnings []string
	Skipped  []string
}

// Deps is everything a step needs from the outside world. Every field is
// injectable so steps are testable without touching the real machine.
type Deps struct {
	// Cfg is the loaded tq registry (the configured base directories).
	Cfg *registry.Config
	// Git runs the git binary. Fakes must emit the same shape the real git
	// does for the flags gitcfg uses — in particular --show-origin --null
	// output is NUL-separated, not tab-separated.
	Git gitcfg.Run
	// Reg runs reg.exe; same signature as Runner.Reg. Nil off Windows.
	Reg func(action, key, name string, v RegValue) (string, error)
	// Env reads an environment variable.
	Env func(string) (string, bool)
	// Processes returns the names (and, where cheap, command lines) of running
	// processes, so a step can refuse to move a directory that is in use.
	Processes func() ([]string, error)
	// Force lets a step proceed past a refusal that is advisory rather than
	// structural. It never disables a precondition that protects data.
	Force bool
}

// Step is one migration step. Plan must not write anything; Apply must journal
// every mutation *before* performing it, so a crash mid-step stays reversible.
type Step interface {
	Name() string
	Plan(d Deps) (Plan, error)
	Apply(d Deps, p Plan, j *Journal) error
}

// stepOrder is the order steps always run in, regardless of the order the user
// lists them: identity moves the directories, git rewrites config that may
// point at them, then the shell and cmd steps adopt the launchers.
var stepOrder = []string{"identity", "git", "shell", "cmd"}

// registry of available steps, populated by each step file's init so that
// adding a step never requires editing this file.
var stepRegistry = map[string]Step{}

// register adds a step. It panics on a duplicate or unknown name, both of
// which are programmer errors caught at startup.
func register(s Step) {
	name := s.Name()
	if _, dup := stepRegistry[name]; dup {
		panic("migrate: duplicate step " + name)
	}
	known := false
	for _, n := range stepOrder {
		if n == name {
			known = true
			break
		}
	}
	if !known {
		panic("migrate: step " + name + " is not in stepOrder")
	}
	stepRegistry[name] = s
}

// KnownSteps returns the registered step names in run order.
func KnownSteps() []string {
	out := make([]string, 0, len(stepRegistry))
	for _, n := range stepOrder {
		if _, ok := stepRegistry[n]; ok {
			out = append(out, n)
		}
	}
	return out
}

// Steps resolves names to steps, rejecting unknown ones and returning them in
// stepOrder regardless of the order given. Duplicates collapse.
func Steps(names []string) ([]Step, error) {
	want := map[string]bool{}
	for _, n := range names {
		if _, ok := stepRegistry[n]; !ok {
			return nil, fmt.Errorf("unknown step %q (known: %v)", n, KnownSteps())
		}
		want[n] = true
	}
	var out []Step
	for _, n := range stepOrder {
		if want[n] {
			out = append(out, stepRegistry[n])
		}
	}
	return out, nil
}

// Run plans every step and, when apply is true, applies them in order. It
// always returns the plans it computed, including for a step that then failed,
// so the caller can show the user how far it got. On the first Apply error it
// stops and returns that error alongside the plans.
func Run(d Deps, steps []Step, apply bool, j *Journal) (map[string]Plan, error) {
	plans := map[string]Plan{}
	for _, s := range steps {
		p, err := s.Plan(d)
		if err != nil {
			return plans, fmt.Errorf("%s: %w", s.Name(), err)
		}
		plans[s.Name()] = p
	}
	if !apply {
		return plans, nil
	}
	if j == nil {
		return plans, fmt.Errorf("apply requires a journal")
	}
	for _, s := range steps {
		if err := s.Apply(d, plans[s.Name()], j); err != nil {
			return plans, fmt.Errorf("%s: %w", s.Name(), err)
		}
	}
	return plans, nil
}

// SortedStepNames returns the keys of a plan map in run order, for stable
// output.
func SortedStepNames(plans map[string]Plan) []string {
	out := make([]string, 0, len(plans))
	for _, n := range stepOrder {
		if _, ok := plans[n]; ok {
			out = append(out, n)
		}
	}
	// Anything unexpected sorts last so nothing is silently dropped.
	for n := range plans {
		found := false
		for _, k := range out {
			if k == n {
				found = true
				break
			}
		}
		if !found {
			out = append(out, n)
		}
	}
	sort.SliceStable(out[len(out):], func(i, j int) bool { return out[i] < out[j] })
	return out
}
