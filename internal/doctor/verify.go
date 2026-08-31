package doctor

import (
	"context"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/envplan"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/verify"
)

// Verify modes for Deps.VerifyMode.
const (
	// VerifyAuto runs a verify command only where it can answer a question
	// somebody actually asked: an expectation declared in the manifest, or a
	// login state tq cannot see any other way. Costs nothing on a setup that
	// declares nothing.
	VerifyAuto = "auto"
	// VerifyAll checks every identity that has a verify command.
	VerifyAll = "all"
	// VerifyOff runs none.
	VerifyOff = "off"
)

// expectationsFor returns the account and subscription this identity is
// supposed to be signed in as, or empty strings when nothing is declared.
//
// Account falls back to Git.ExpectedUser for the workspace's git provider:
// that field predates per-identity expectations and is already enforced
// inside Claude sessions, so honouring it here closes the gap where the same
// rule applied to an agent but not to the developer typing commands.
func expectationsFor(m *manifest.Manifest, id string) (account, subscription string) {
	if m == nil {
		return "", ""
	}
	if ident, ok := m.Identities[id]; ok {
		account = strings.TrimSpace(ident.ExpectedAccount)
		subscription = strings.TrimSpace(ident.ExpectedSubscription)
	}
	if account == "" && isGitProvider(m, id) {
		account = strings.TrimSpace(m.Git.ExpectedUser)
	}
	return account, subscription
}

// isGitProvider reports whether id is the CLI for this workspace's git host.
// git.provider names the host ("github", "gitlab"); the CLI is gh or glab.
//
// Getting this mapping wrong is not harmless. A workspace on Azure DevOps
// carries an expected_user that is an email address; comparing that against
// the username `gh` reports would flag a perfectly good GitHub login as the
// wrong account, and a check that cries wolf is worse than no check.
func isGitProvider(m *manifest.Manifest, id string) bool {
	switch strings.ToLower(strings.TrimSpace(m.Git.Provider)) {
	case "github", "":
		return id == "gh"
	case "gitlab":
		return id == "glab"
	case "azure-devops", "azuredevops":
		return id == "azure-devops"
	}
	return false
}

// shouldVerify decides whether to spend a subprocess on this identity.
//
// The default is not "check everything": these commands cost hundreds of
// milliseconds each and several make a network call, so a doctor run that
// checked a dozen identities unconditionally would turn a sub-second
// diagnostic into a five-second one that needs the internet. Under auto, tq
// pays only where the answer changes something -- a declared expectation, or
// a login state it has no cheaper way to observe.
func shouldVerify(mode string, m *manifest.Manifest, id string, p providers.Provider) bool {
	if mode == VerifyOff || p.Verify == nil || len(p.Verify.Args) == 0 {
		return false
	}
	if mode == VerifyAll {
		return true
	}
	acct, sub := expectationsFor(m, id)
	if acct != "" || sub != "" {
		return true
	}
	// macOS keeps Claude's credentials in the Keychain rather than in a file
	// inside the config dir, so the cheap file check is blind there and
	// `claude-not-logged-in` silently never fires. Asking the CLI is the only
	// way tq can answer it on a Mac.
	return id == "claude" && runtime.GOOS == "darwin"
}

// verifyJob is a job plus what it will be judged against.
type verifyJob struct {
	job          verify.Job
	account      string
	subscription string
}

// planVerify builds the job list for one workspace.
func planVerify(mode string, w *resolve.Workspace, ids []string, cat *providers.Catalog, env []string) []verifyJob {
	var out []verifyJob
	for _, id := range ids {
		p, ok := cat.Get(id)
		if !ok {
			continue
		}
		if !shouldVerify(mode, w.Manifest, id, p) {
			continue
		}
		acct, sub := expectationsFor(w.Manifest, id)
		out = append(out, verifyJob{
			job:          verify.Job{Workspace: w.Name, Identity: id, Provider: p, Env: env},
			account:      acct,
			subscription: sub,
		})
	}
	return out
}

// verifyFindings turns results into findings, in a stable order.
func verifyFindings(jobs []verifyJob, results map[string]verify.Result) []Finding {
	sorted := append([]verifyJob(nil), jobs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].job.Workspace != sorted[j].job.Workspace {
			return sorted[i].job.Workspace < sorted[j].job.Workspace
		}
		return sorted[i].job.Identity < sorted[j].job.Identity
	})

	var fs []Finding
	for _, vj := range sorted {
		r, ok := results[vj.job.Key()]
		if !ok || !r.Ran {
			continue
		}
		ws, id := vj.job.Workspace, vj.job.Identity
		if r.Err != nil {
			fs = append(fs, Finding{"warn", "verify-failed", ws,
				id + ": could not check which account is signed in: " + verify.SafeError(r.Err),
				"run `" + id + " " + strings.Join(vj.job.Provider.Verify.Args, " ") + "` yourself to see what it says"})
			continue
		}
		if !r.LoggedIn {
			fs = append(fs, Finding{"warn", "identity-logged-out", ws,
				id + " is not signed in for this workspace",
				"tq login " + ws + " " + id})
			continue
		}
		// A declared expectation that the CLI cannot report is worth saying
		// out loud: silently passing would imply it had been checked.
		if vj.account != "" && r.Account == "" {
			fs = append(fs, Finding{"warn", "verify-failed", ws,
				id + ": signed in, but tq could not read which account from its output, so expected_account was not checked",
				"file a provider-catalog issue, or clear expected_account for " + id})
		} else if vj.account != "" && !strings.EqualFold(r.Account, vj.account) {
			fs = append(fs, Finding{"error", "identity-wrong-account", ws,
				id + " is signed in as " + r.Account + ", but this workspace expects " + vj.account,
				"tq login " + ws + " " + id + " and sign in as " + vj.account})
		}
		if vj.subscription != "" && r.Subscription != "" && !strings.EqualFold(r.Subscription, vj.subscription) {
			fs = append(fs, Finding{"error", "identity-wrong-subscription", ws,
				id + " is on the " + r.Subscription + " plan here, but this workspace expects " + vj.subscription,
				"sign in with the account that carries the " + vj.subscription + " plan: tq login " + ws + " " + id})
		}
	}
	return fs
}

// runVerify executes the planned jobs. Split out so tests drive it directly.
func runVerify(d Deps, jobs []verifyJob) []Finding {
	if len(jobs) == 0 || d.RunCLI == nil {
		return nil
	}
	vjs := make([]verify.Job, 0, len(jobs))
	for _, j := range jobs {
		vjs = append(vjs, j.job)
	}
	results := verify.RunAll(context.Background(), vjs, d.RunCLI, verify.DefaultParallel)
	return verifyFindings(jobs, results)
}

// environFor builds the child environment for a workspace, falling back to the
// real process environment plus that workspace's identity variables.
func environFor(d Deps, w *resolve.Workspace) []string {
	if d.Environ != nil {
		return d.Environ(w)
	}
	return envplan.Environ(w, os.Environ())
}
