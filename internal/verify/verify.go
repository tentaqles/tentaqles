// Package verify asks a CLI who it is logged in as, inside a workspace's own
// config home.
//
// tq's core mechanism points a CLI at a private config directory. That routes
// the tool; it does not prove the right account is in it. A workspace can be
// wired up perfectly and still be signed in as the wrong client -- and because
// every path and variable looks correct, nothing else in tq would notice.
// This package is what turns "tq manages your identities" into "tq can tell
// you the identity is wrong".
//
// Everything here shells out, so it is deliberately kept off tq's hot paths:
// the per-prompt env diff and the pre-tool-use hook must never call it. See
// the note on doctor.RunForCwd.
package verify

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/providers"
)

// Timeout bounds a single verify command. Several of these CLIs make a network
// call to validate a token, so the budget has to survive a slow link without
// letting a hung process stall a diagnostic command indefinitely.
const Timeout = 10 * time.Second

// DefaultParallel is how many verify commands run at once. Measured on the
// development machine: `claude auth status` is ~600ms and `gh auth status`
// ~100ms, against a `tq doctor` that otherwise takes ~590ms in total. Run
// serially, a dozen identities would turn a sub-second command into five
// seconds; run all at once, a large setup would fork thirty processes.
const DefaultParallel = 8

// Runner executes a verify command with env and returns its combined output.
// Injected so tests never shell out.
type Runner func(ctx context.Context, env []string, name string, args []string) (string, error)

// Result is what one verify call learned. Ran is false when there was nothing
// to run; check it before trusting the other fields.
type Result struct {
	Ran          bool
	LoggedIn     bool
	Account      string
	Subscription string
	// Err is set when the command could not be run or failed in a way that
	// says nothing about login state. A CLI that exits non-zero *because* it
	// is logged out is not an error: LoggedOutWhen catches that first.
	Err error
}

// Job is one (workspace, identity) pair to check.
type Job struct {
	Workspace string
	Identity  string
	Provider  providers.Provider
	// Env is the full environment for the child process, already carrying the
	// workspace's identity variables. Without it the CLI would answer for
	// whatever config home the parent happens to have, which is exactly the
	// mistake this package exists to catch.
	Env []string
}

// Key identifies a job's result in the map RunAll returns.
func (j Job) Key() string { return j.Workspace + "/" + j.Identity }

// Check runs one provider's verify command and interprets the output.
func Check(ctx context.Context, p providers.Provider, env []string, run Runner) Result {
	if p.Verify == nil || run == nil {
		return Result{}
	}
	name := p.Verify.Command
	if name == "" && p.CLI != nil {
		name = p.CLI.Command
	}
	if name == "" {
		name = p.ID
	}
	if len(p.Verify.Args) == 0 {
		return Result{}
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	out, err := run(ctx, env, name, p.Verify.Args)

	// Logged-out detection comes first and beats the exit code: `gh auth
	// status` exits non-zero when logged out, and reporting that as a failed
	// check would hide the very thing we asked about.
	if p.Verify.LoggedOutWhen != "" {
		if re, reErr := regexp.Compile(p.Verify.LoggedOutWhen); reErr == nil && re.MatchString(out) {
			return Result{Ran: true, LoggedIn: false}
		}
	}
	if err != nil {
		return Result{Ran: true, Err: err}
	}

	return Result{
		Ran:          true,
		LoggedIn:     true,
		Account:      Extract(out, p.Verify.Account),
		Subscription: Extract(out, p.Verify.Subscription),
	}
}

// RunAll checks every job, bounded to parallel at a time, and returns results
// keyed by Job.Key.
func RunAll(ctx context.Context, jobs []Job, run Runner, parallel int) map[string]Result {
	out := make(map[string]Result, len(jobs))
	if len(jobs) == 0 || run == nil {
		return out
	}
	if parallel <= 0 {
		parallel = DefaultParallel
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, parallel)
	for _, j := range jobs {
		wg.Add(1)
		go func(j Job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r := Check(ctx, j.Provider, j.Env, run)
			mu.Lock()
			out[j.Key()] = r
			mu.Unlock()
		}(j)
	}
	wg.Wait()
	return out
}

// Extract pulls one value out of a command's output.
func Extract(out string, f *providers.Field) string {
	if f == nil {
		return ""
	}
	if f.JSON != "" {
		if v := fromJSON(out, f.JSON); v != "" {
			return v
		}
	}
	if f.Regex != "" {
		re, err := regexp.Compile(f.Regex)
		if err != nil {
			return ""
		}
		// Group 1 is the preferred form; any later group is a fallback, used
		// only when no match filled group 1.
		//
		// Alternation alone cannot express that, because matching is leftmost:
		// given a gh config holding several accounts, a loose fallback branch
		// matches at the FIRST account listed and wins outright, before the
		// precise branch is ever tried against the active one further down.
		// So every match is considered, and the preferred form wins wherever
		// in the output it happens to be.
		ms := re.FindAllStringSubmatch(out, -1)
		for _, m := range ms {
			if len(m) > 1 {
				if g := strings.TrimSpace(m[1]); g != "" {
					return g
				}
			}
		}
		for _, m := range ms {
			for _, g := range m[1:] {
				if g = strings.TrimSpace(g); g != "" {
					return g
				}
			}
		}
	}
	return ""
}

// fromJSON walks a dotted path through a JSON object. A CLI may print a banner
// before its JSON, so the first '{' is found rather than assuming the output
// starts with one.
func fromJSON(out, path string) string {
	i := strings.IndexByte(out, '{')
	if i < 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out[i:]), &m); err != nil {
		return ""
	}
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = obj[part]
		if !ok {
			return ""
		}
	}
	switch v := cur.(type) {
	case string:
		return strings.TrimSpace(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return strings.TrimSuffix(strings.TrimRight(formatFloat(v), "0"), ".")
	}
	return ""
}

func formatFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// tokenish redacts anything that looks like a credential. Verify output is
// meant to be an identity, not a secret, but a CLI's error text is not under
// our control and findings are printed, logged and sent to --json.
var tokenish = regexp.MustCompile(`(?i)(gh[pousr]_|sk-|xox[baprs]-|eyJ)[A-Za-z0-9._\-]{6,}`)

// SafeError renders an error for display: one line, bounded, redacted.
func SafeError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = tokenish.ReplaceAllString(s, "<REDACTED>")
	const max = 200
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}
