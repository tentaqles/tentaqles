package doctor

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/verify"
)

func vProvider(id string) providers.Provider {
	return providers.Provider{
		ID:  id,
		CLI: &providers.CLI{Command: id},
		Verify: &providers.VerifyCmd{
			Cmd:           providers.Cmd{Args: []string{"auth", "status"}},
			LoggedOutWhen: `"loggedIn" *: *false`,
			Account:       &providers.Field{JSON: "email"},
			Subscription:  &providers.Field{JSON: "subscriptionType"},
		},
	}
}

func mf(mod func(*manifest.Manifest)) *manifest.Manifest {
	m := &manifest.Manifest{
		Client:     "acme",
		Git:        manifest.Git{Name: "Dev", Email: "dev@acme.test"},
		Identities: map[string]manifest.Identity{"claude": {}, "gh": {}},
	}
	if mod != nil {
		mod(m)
	}
	return m
}

// The default must not fork a process per identity just because it can: that
// is what keeps `tq doctor` a sub-second offline command.
func TestShouldVerify_AutoCostsNothingWhenNothingIsDeclared(t *testing.T) {
	m := mf(nil)
	got := shouldVerify(VerifyAuto, m, "gh", vProvider("gh"))
	if got {
		t.Error("auto must not verify gh when no expectation is declared")
	}
	// claude off macOS has a cheap file check, so it is not needed either.
	if runtime.GOOS != "darwin" {
		if shouldVerify(VerifyAuto, m, "claude", vProvider("claude")) {
			t.Error("auto must not verify claude when the file check can answer")
		}
	}
}

func TestShouldVerify_AutoRunsWhenSomethingIsDeclared(t *testing.T) {
	for _, tc := range []struct {
		name string
		mod  func(*manifest.Manifest)
		id   string
	}{
		{"expected_account", func(m *manifest.Manifest) {
			m.Identities["claude"] = manifest.Identity{ExpectedAccount: "dev@acme.test"}
		}, "claude"},
		{"expected_subscription", func(m *manifest.Manifest) {
			m.Identities["claude"] = manifest.Identity{ExpectedSubscription: "team"}
		}, "claude"},
		{"git expected_user falls through to gh", func(m *manifest.Manifest) {
			m.Git.ExpectedUser = "someone"
		}, "gh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !shouldVerify(VerifyAuto, mf(tc.mod), tc.id, vProvider(tc.id)) {
				t.Error("auto should verify when an expectation is declared")
			}
		})
	}
}

func TestShouldVerify_ModesAndMissingVerify(t *testing.T) {
	m := mf(nil)
	if !shouldVerify(VerifyAll, m, "gh", vProvider("gh")) {
		t.Error("all should verify everything with a verify command")
	}
	if shouldVerify(VerifyOff, mf(func(m *manifest.Manifest) { m.Git.ExpectedUser = "x" }), "gh", vProvider("gh")) {
		t.Error("off must never verify")
	}
	noVerify := providers.Provider{ID: "vercel", CLI: &providers.CLI{Command: "vercel"}}
	if shouldVerify(VerifyAll, m, "vercel", noVerify) {
		t.Error("a provider with no verify command cannot be verified")
	}
}

// Git.ExpectedUser predates per-identity expectations and was only ever
// enforced inside Claude sessions. Honouring it here is what closes the gap.
func TestExpectationsFor_GitExpectedUserFallback(t *testing.T) {
	m := mf(func(m *manifest.Manifest) { m.Git.ExpectedUser = "rndomingues" })
	if acct, _ := expectationsFor(m, "gh"); acct != "rndomingues" {
		t.Errorf("gh account = %q, want the git expected_user", acct)
	}
	// but not for an unrelated identity
	if acct, _ := expectationsFor(m, "claude"); acct != "" {
		t.Errorf("claude must not inherit the git user, got %q", acct)
	}
	// gitlab workspaces use glab
	gl := mf(func(m *manifest.Manifest) {
		m.Git.Provider = "gitlab"
		m.Git.ExpectedUser = "someone"
	})
	if acct, _ := expectationsFor(gl, "glab"); acct != "someone" {
		t.Errorf("glab account = %q", acct)
	}
	if acct, _ := expectationsFor(gl, "gh"); acct != "" {
		t.Errorf("gh must not claim a gitlab workspace's user, got %q", acct)
	}
	// an explicit per-identity account wins over the git fallback
	both := mf(func(m *manifest.Manifest) {
		m.Git.ExpectedUser = "from-git"
		m.Identities["gh"] = manifest.Identity{ExpectedAccount: "explicit"}
	})
	if acct, _ := expectationsFor(both, "gh"); acct != "explicit" {
		t.Errorf("explicit expected_account should win, got %q", acct)
	}
}

func job(ws, id, account, sub string) verifyJob {
	return verifyJob{
		job:          verify.Job{Workspace: ws, Identity: id, Provider: vProvider(id)},
		account:      account,
		subscription: sub,
	}
}

// The headline case: everything is wired correctly and the developer is
// signed into the wrong client.
func TestVerifyFindings_WrongAccountIsAnError(t *testing.T) {
	jobs := []verifyJob{job("acme", "claude", "dev@acme.test", "")}
	res := map[string]verify.Result{
		"acme/claude": {Ran: true, LoggedIn: true, Account: "dev@globex.test"},
	}
	fs := verifyFindings(jobs, res)
	if len(fs) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(fs), fs)
	}
	if fs[0].Level != "error" || fs[0].Code != "identity-wrong-account" {
		t.Fatalf("got %+v", fs[0])
	}
	// Both accounts must appear: which one is wrong and which is wanted.
	if !strings.Contains(fs[0].Msg, "dev@globex.test") || !strings.Contains(fs[0].Msg, "dev@acme.test") {
		t.Errorf("message should name both accounts: %q", fs[0].Msg)
	}
}

func TestVerifyFindings_WrongSubscription(t *testing.T) {
	jobs := []verifyJob{job("acme", "claude", "", "max")}
	res := map[string]verify.Result{
		"acme/claude": {Ran: true, LoggedIn: true, Subscription: "team"},
	}
	fs := verifyFindings(jobs, res)
	if len(fs) != 1 || fs[0].Code != "identity-wrong-subscription" || fs[0].Level != "error" {
		t.Fatalf("got %+v", fs)
	}
}

func TestVerifyFindings_MatchIsSilent(t *testing.T) {
	jobs := []verifyJob{job("acme", "claude", "dev@acme.test", "max")}
	res := map[string]verify.Result{
		// case differs deliberately: an account is not case-sensitive here
		"acme/claude": {Ran: true, LoggedIn: true, Account: "Dev@Acme.Test", Subscription: "MAX"},
	}
	if fs := verifyFindings(jobs, res); len(fs) != 0 {
		t.Fatalf("a match must produce nothing, got %+v", fs)
	}
}

func TestVerifyFindings_LoggedOut(t *testing.T) {
	jobs := []verifyJob{job("acme", "claude", "dev@acme.test", "")}
	res := map[string]verify.Result{"acme/claude": {Ran: true, LoggedIn: false}}
	fs := verifyFindings(jobs, res)
	if len(fs) != 1 || fs[0].Code != "identity-logged-out" || fs[0].Level != "warn" {
		t.Fatalf("got %+v", fs)
	}
	// Logged out is not also "wrong account": one problem, one finding.
	if strings.Contains(fs[0].Msg, "expects") {
		t.Errorf("logged out should not also complain about the account: %q", fs[0].Msg)
	}
}

// A declared expectation that could not actually be checked must say so.
// Silently passing would imply it had been verified.
func TestVerifyFindings_UnreadableAccountIsNotSilentPass(t *testing.T) {
	jobs := []verifyJob{job("acme", "claude", "dev@acme.test", "")}
	res := map[string]verify.Result{"acme/claude": {Ran: true, LoggedIn: true, Account: ""}}
	fs := verifyFindings(jobs, res)
	if len(fs) != 1 || fs[0].Code != "verify-failed" {
		t.Fatalf("got %+v", fs)
	}
	if !strings.Contains(fs[0].Msg, "not checked") {
		t.Errorf("must be explicit that nothing was checked: %q", fs[0].Msg)
	}
}

func TestVerifyFindings_CommandFailureIsAWarningNotAnError(t *testing.T) {
	jobs := []verifyJob{job("acme", "claude", "dev@acme.test", "")}
	res := map[string]verify.Result{
		"acme/claude": {Ran: true, Err: errors.New("dial tcp: lookup api: no such host")},
	}
	fs := verifyFindings(jobs, res)
	if len(fs) != 1 || fs[0].Code != "verify-failed" || fs[0].Level != "warn" {
		t.Fatalf("a network blip must not fail doctor: %+v", fs)
	}
}

func TestVerifyFindings_StableOrderAndSkipsUnrun(t *testing.T) {
	jobs := []verifyJob{
		job("zeta", "gh", "want", ""),
		job("acme", "gh", "want", ""),
		job("acme", "claude", "want", ""),
		job("never", "gh", "want", ""),
	}
	res := map[string]verify.Result{
		"zeta/gh":     {Ran: true, LoggedIn: true, Account: "other"},
		"acme/gh":     {Ran: true, LoggedIn: true, Account: "other"},
		"acme/claude": {Ran: true, LoggedIn: true, Account: "other"},
		"never/gh":    {Ran: false},
	}
	fs := verifyFindings(jobs, res)
	if len(fs) != 3 {
		t.Fatalf("want 3 (the unrun one is skipped), got %d", len(fs))
	}
	got := []string{
		fs[0].Workspace + "/" + strings.Fields(fs[0].Msg)[0],
		fs[1].Workspace + "/" + strings.Fields(fs[1].Msg)[0],
		fs[2].Workspace + "/" + strings.Fields(fs[2].Msg)[0],
	}
	want := []string{"acme/claude", "acme/gh", "zeta/gh"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestRunVerify_NoRunnerIsANoOp(t *testing.T) {
	jobs := []verifyJob{job("acme", "claude", "dev@acme.test", "")}
	if fs := runVerify(Deps{}, jobs); fs != nil {
		t.Fatalf("without a runner there is nothing to report, got %+v", fs)
	}
}
