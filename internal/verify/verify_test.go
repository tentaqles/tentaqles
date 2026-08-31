package verify

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/providers"
)

// claudeJSON is the real shape of `claude auth status`, captured on both
// Windows and macOS.
const claudeJSON = `{
  "loggedIn": true,
  "authMethod": "claude.ai",
  "apiProvider": "firstParty",
  "email": "dev@example.com",
  "orgId": "0ec70e27-cbc0-41de-b6bf-3ad7ec6d3ea7",
  "orgName": "Enteros",
  "subscriptionType": "team"
}`

const claudeLoggedOut = `{
  "loggedIn": false,
  "authMethod": "none",
  "apiProvider": "firstParty"
}`

// ghStatus is the real shape of `gh auth status`.
const ghStatus = "github.com\n  ✓ Logged in to github.com account rndomingues (keyring)\n  - Active account: true\n"

const ghLoggedOut = "You are not logged into any GitHub hosts. To log in, run: gh auth login"

func claudeProvider() providers.Provider {
	return providers.Provider{
		ID:  "claude",
		CLI: &providers.CLI{Command: "claude"},
		Verify: &providers.VerifyCmd{
			Cmd:           providers.Cmd{Args: []string{"auth", "status"}},
			LoggedOutWhen: `"loggedIn" *: *false`,
			Account:       &providers.Field{JSON: "email"},
			Subscription:  &providers.Field{JSON: "subscriptionType"},
		},
	}
}

func ghProvider() providers.Provider {
	return providers.Provider{
		ID:  "gh",
		CLI: &providers.CLI{Command: "gh"},
		Verify: &providers.VerifyCmd{
			Cmd:           providers.Cmd{Args: []string{"auth", "status"}},
			LoggedOutWhen: `not logged in|You are not logged`,
			Account:       &providers.Field{Regex: `account ([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)`},
		},
	}
}

func fixed(out string, err error) Runner {
	return func(context.Context, []string, string, []string) (string, error) { return out, err }
}

func TestCheck_Claude(t *testing.T) {
	r := Check(context.Background(), claudeProvider(), nil, fixed(claudeJSON, nil))
	if !r.Ran || !r.LoggedIn {
		t.Fatalf("want ran+logged in, got %+v", r)
	}
	if r.Account != "dev@example.com" {
		t.Errorf("account = %q", r.Account)
	}
	if r.Subscription != "team" {
		t.Errorf("subscription = %q", r.Subscription)
	}
}

func TestCheck_ClaudeLoggedOut(t *testing.T) {
	r := Check(context.Background(), claudeProvider(), nil, fixed(claudeLoggedOut, nil))
	if !r.Ran || r.LoggedIn {
		t.Fatalf("want ran+logged out, got %+v", r)
	}
	if r.Err != nil {
		t.Errorf("logged out is not an error: %v", r.Err)
	}
}

// gh exits non-zero when logged out. Reporting that as a failed check would
// hide the answer behind a shrug, so LoggedOutWhen is consulted first.
func TestCheck_LoggedOutBeatsExitCode(t *testing.T) {
	r := Check(context.Background(), ghProvider(), nil, fixed(ghLoggedOut, errors.New("exit status 1")))
	if !r.Ran {
		t.Fatal("want ran")
	}
	if r.LoggedIn {
		t.Error("want logged out")
	}
	if r.Err != nil {
		t.Errorf("want no error, got %v", r.Err)
	}
}

func TestCheck_GhAccountFromProse(t *testing.T) {
	r := Check(context.Background(), ghProvider(), nil, fixed(ghStatus, nil))
	if r.Account != "rndomingues" {
		t.Fatalf("account = %q, want rndomingues", r.Account)
	}
}

func TestCheck_RealFailureIsAnError(t *testing.T) {
	r := Check(context.Background(), claudeProvider(), nil, fixed("", errors.New("exec: \"claude\": executable file not found")))
	if !r.Ran || r.Err == nil {
		t.Fatalf("want ran with an error, got %+v", r)
	}
	if r.LoggedIn {
		t.Error("a failed command must not report logged in")
	}
}

func TestCheck_NoVerifyDeclared(t *testing.T) {
	p := providers.Provider{ID: "vercel", CLI: &providers.CLI{Command: "vercel"}}
	if r := Check(context.Background(), p, nil, fixed(claudeJSON, nil)); r.Ran {
		t.Fatal("a provider with no verify must not run one")
	}
}

// The environment is the whole point: without it the CLI answers for whatever
// config home the parent had, which is the mistake this package exists to
// catch.
func TestCheck_PassesEnvAndCommand(t *testing.T) {
	var gotEnv []string
	var gotName string
	var gotArgs []string
	run := func(_ context.Context, env []string, name string, args []string) (string, error) {
		gotEnv, gotName, gotArgs = env, name, args
		return claudeJSON, nil
	}
	Check(context.Background(), claudeProvider(), []string{"CLAUDE_CONFIG_DIR=/w/alpha"}, run)
	if gotName != "claude" {
		t.Errorf("name = %q", gotName)
	}
	if strings.Join(gotArgs, " ") != "auth status" {
		t.Errorf("args = %v", gotArgs)
	}
	if len(gotEnv) != 1 || gotEnv[0] != "CLAUDE_CONFIG_DIR=/w/alpha" {
		t.Errorf("env = %v", gotEnv)
	}
}

func TestExtract_JSONBeatsRegexAndHandlesPreamble(t *testing.T) {
	out := "Checking...\n" + claudeJSON
	if got := Extract(out, &providers.Field{JSON: "email"}); got != "dev@example.com" {
		t.Errorf("with preamble: %q", got)
	}
	// dotted path, for CLIs that nest
	nested := `{"user":{"name":"someone@example.com"},"name":"Sub"}`
	if got := Extract(nested, &providers.Field{JSON: "user.name"}); got != "someone@example.com" {
		t.Errorf("dotted: %q", got)
	}
	// falls back to regex when the JSON key is absent
	f := &providers.Field{JSON: "nope", Regex: `account ([a-z]+)`}
	if got := Extract("account someone", f); got != "someone" {
		t.Errorf("fallback: %q", got)
	}
	if got := Extract("anything", nil); got != "" {
		t.Errorf("nil field: %q", got)
	}
	if got := Extract("not json at all", &providers.Field{JSON: "email"}); got != "" {
		t.Errorf("non-json: %q", got)
	}
}

func TestRunAll_BoundedAndKeyed(t *testing.T) {
	var mu sync.Mutex
	var live, peak int
	run := func(context.Context, []string, string, []string) (string, error) {
		mu.Lock()
		live++
		if live > peak {
			peak = live
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		live--
		mu.Unlock()
		return claudeJSON, nil
	}
	var jobs []Job
	for _, ws := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		jobs = append(jobs, Job{Workspace: ws, Identity: "claude", Provider: claudeProvider()})
	}
	res := RunAll(context.Background(), jobs, run, 2)
	if len(res) != len(jobs) {
		t.Fatalf("got %d results, want %d", len(res), len(jobs))
	}
	if _, ok := res["c/claude"]; !ok {
		t.Error("results must be keyed workspace/identity")
	}
	if peak > 2 {
		t.Errorf("parallelism cap ignored: peak %d, want <= 2", peak)
	}
}

func TestRunAll_NoRunnerNoJobs(t *testing.T) {
	if got := RunAll(context.Background(), nil, fixed("", nil), 4); len(got) != 0 {
		t.Error("no jobs, no results")
	}
	jobs := []Job{{Workspace: "a", Identity: "claude", Provider: claudeProvider()}}
	if got := RunAll(context.Background(), jobs, nil, 4); len(got) != 0 {
		t.Error("no runner, no results")
	}
}

func TestSafeError_RedactsAndBounds(t *testing.T) {
	err := errors.New("failed with token ghp_abcdef1234567890abcdef and more\nsecond line")
	got := SafeError(err)
	if strings.Contains(got, "ghp_abcdef1234567890abcdef") {
		t.Errorf("token survived: %q", got)
	}
	if strings.Contains(got, "second line") {
		t.Errorf("should be one line: %q", got)
	}
	long := errors.New(strings.Repeat("x", 500))
	if len([]rune(SafeError(long))) > 210 {
		t.Error("should be bounded")
	}
	if SafeError(nil) != "" {
		t.Error("nil error is empty")
	}
}

func TestCheck_TimeoutIsBounded(t *testing.T) {
	run := func(ctx context.Context, _ []string, _ string, _ []string) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(30 * time.Second):
			return claudeJSON, nil
		}
	}
	// A context already past its deadline stands in for a hung CLI without
	// making the test wait for the real Timeout.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	r := Check(ctx, claudeProvider(), nil, run)
	if r.Err == nil {
		t.Fatal("a hung command must surface as an error, not a hang")
	}
}

// ghMultiAccount is the real shape when one gh config holds several accounts
// for the same host -- taken from the development machine, which had four.
// Only one is active, and the marker is on the line AFTER the account.
const ghMultiAccount = `github.com
  ✓ Logged in to github.com account rdominguesds (keyring)
  - Active account: false
  ✓ Logged in to github.com account rndomingues (keyring)
  - Active account: true
  ✓ Logged in to github.com account tentaqles (keyring)
  - Active account: false
`

// Reporting the first account printed rather than the active one would name
// a signed-in-but-unused account as the current identity, which is exactly
// the wrong answer for a check about which client you are acting as.
func TestCheck_GhPicksTheActiveAccountNotTheFirst(t *testing.T) {
	p := ghProvider()
	p.Verify.Account = &providers.Field{
		Regex: `account ([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)[^\n]*\n\s*- Active account: true|account ([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)`,
	}
	r := Check(context.Background(), p, nil, fixed(ghMultiAccount, nil))
	if r.Account != "rndomingues" {
		t.Fatalf("account = %q, want rndomingues (the active one, not the first listed)", r.Account)
	}
}

// Older gh does not print the active marker at all; the alternation's second
// branch has to carry that case.
func TestCheck_GhFallsBackWhenNoActiveMarker(t *testing.T) {
	p := ghProvider()
	p.Verify.Account = &providers.Field{
		Regex: `account ([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)[^\n]*\n\s*- Active account: true|account ([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)`,
	}
	r := Check(context.Background(), p, nil, fixed(ghStatus, nil))
	if r.Account != "rndomingues" {
		t.Fatalf("account = %q", r.Account)
	}
}
