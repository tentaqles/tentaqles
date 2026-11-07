// Package identitystatus builds what `tentaqles status --json` prints: one client's setup as
// the onboarding skill reads it (ADR-0017). The skill cannot read the manifest or the Claude
// config, which are protected paths, so this is its only view of them.
//
// It is read-only and carries no secret: a credential is reported as present, missing or none,
// a connection by the server's name, and no environment value, token, or MCP entry value is
// ever read into the result (the .claude.json is read for its server names only).
package identitystatus

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/secret"
	"github.com/tentaqles/tentaqles/internal/verify"
)

// VerifyLimit bounds each provider's verify command (REQ-6).
const VerifyLimit = 5 * time.Second

// Values of Provider.Credential and Provider.Connection.
const (
	Present = "present"
	Missing = "missing"
	None    = "none"
)

// Status is the object `status --json` prints.
type Status struct {
	// Identity is the client's name, null in a neutral folder.
	Identity *string `json:"identity"`
	// Root is the client's folder, null in a neutral folder.
	Root *string `json:"root"`
	// Trusted is false for an identity whose manifest or integrity record is not trusted; it is
	// left out in a neutral folder.
	Trusted *bool `json:"trusted,omitempty"`
	// Reason says why the folder is neutral or the identity untrusted.
	Reason         string     `json:"reason,omitempty"`
	Scope          []string   `json:"scope"`
	Providers      []Provider `json:"providers"`
	Browsers       []Browser  `json:"browsers"`
	OtherProviders []string   `json:"other_providers"`
}

// Provider is one provider of the scope.
type Provider struct {
	ID       string  `json:"id"`
	Category string  `json:"category"`
	CLI      *string `json:"cli"`
	// CLIResolves is whether the CLI is on PATH; false when the provider has none.
	CLIResolves bool `json:"cli_resolves"`
	// Credential is present when a token is in the keychain for this client, a credential
	// variable of the provider is set, or the provider's verify command passed. Missing when none
	// of that holds and tq can check the provider: a tracker provider (its keychain token), one
	// with a verify command, or one that names credential variables and has no CLI login. None for
	// any other provider: a CLI login with no verify command, which tq cannot tell from logged out,
	// and a provider that takes no credential through tq.
	Credential string `json:"credential"`
	// Connection is present when an MCP server for the provider is registered in the client's
	// .claude.json under any of the names the provider is reached by (its catalog connect name, its
	// MCP name and its match list), missing when it is not and the provider is reached by an MCP
	// server, none for a provider no MCP server reaches.
	Connection string `json:"connection"`
	// Verified is true only when the provider's read-only verify command passed in this run.
	Verified bool `json:"verified"`
}

// Browser is a browser profile the client uses.
type Browser struct {
	Name    string `json:"name"`
	Profile string `json:"profile"`
}

// Deps are the seams of Build; a nil one takes the real thing.
type Deps struct {
	Env      func(string) (string, bool)
	LookPath func(string) (string, error)
	// RunCLI runs a verify command (as in internal/verify).
	RunCLI verify.Runner
	// Environ is the child environment carrying the client's identity.
	Environ func(*resolve.Workspace) []string
	// VerifyLimit overrides VerifyLimit (tests).
	VerifyLimit time.Duration
	// HasToken asks the keychain whether a token is stored for the client and tracker name;
	// nil is secret.Has. It is bounded by KeychainLimit.
	HasToken func(ws, tracker string) (bool, error)
	// KeychainLimit overrides KeychainLimit (tests).
	KeychainLimit time.Duration
	// Catalog is the merged provider catalog; nil is providers.MustLoad().
	Catalog *providers.Catalog
}

// Unanswered is the status of a folder whose trust could not be checked in time: the integrity
// key's keychain read did not answer within limit. With the folder's name (name, root from
// resolve.FolderName) it is the untrusted shape (REQ-4): identity, root, trusted false and a reason
// that names the timeout, and no provider data. Without a name the folder is treated as neutral.
func Unanswered(name, root string, limit time.Duration) Status {
	reason := "the integrity key did not answer within " + limit.String() +
		", so this folder's trust could not be checked; run tentaqles status --json again"
	s := Status{Scope: []string{}, Providers: []Provider{}, Browsers: []Browser{}, OtherProviders: []string{}}
	if name == "" {
		s.Reason = reason
		return s
	}
	f := false
	s.Identity, s.Root, s.Trusted, s.Reason = &name, &root, &f, reason
	return s
}

// Build describes the folder res resolved: neutral, untrusted, or a trusted client.
func Build(res resolve.Result, d Deps) Status {
	if d.Env == nil {
		d.Env = os.LookupEnv
	}
	if d.LookPath == nil {
		d.LookPath = exec.LookPath
	}
	if d.VerifyLimit <= 0 {
		d.VerifyLimit = VerifyLimit
	}
	if d.HasToken == nil {
		d.HasToken = secret.Has
	}
	if d.KeychainLimit <= 0 {
		d.KeychainLimit = KeychainLimit
	}
	s := Status{Scope: []string{}, Providers: []Provider{}, Browsers: []Browser{}, OtherProviders: []string{}}
	switch {
	case res.Workspace == nil && res.Untrusted == nil:
		s.Reason = res.Reason
		return s
	case res.Workspace == nil:
		w := res.Untrusted
		f := false
		s.Identity, s.Root, s.Trusted, s.Reason = &w.Name, &w.Root, &f, res.Reason
		return s
	}
	w := res.Workspace
	t := true
	s.Identity, s.Root, s.Trusted = &w.Name, &w.Root, &t
	m := w.Manifest
	s.Scope = m.Scope()
	for _, b := range m.Browsers {
		s.Browsers = append(s.Browsers, Browser{Name: b.Name, Profile: b.Profile})
	}
	s.OtherProviders = append(s.OtherProviders, m.OtherProviders...)

	cat := d.Catalog
	if cat == nil {
		cat = providers.MustLoad()
	}
	// connect: is defined by the embedded catalog only (a user file's is not loaded), so the
	// merged catalog, where a user override replaced the entry, never carries it.
	emb, err := providers.Embedded()
	if err != nil {
		emb = nil
	}
	var env []string
	if d.Environ != nil {
		env = d.Environ(w)
	} else {
		env = envplan.Environ(w, os.Environ())
	}
	// A connection lives in the identity's Claude config. An identity with no claude identity
	// has none: nothing is read, and a provider an MCP server reaches is missing.
	var servers []string
	if slices.Contains(m.IdentityNames(), "claude") {
		servers = doctor.MCPServerNames(filepath.Join(paths.IdentityDir(w.Name, "claude"), ".claude.json"), w.Root)
	}

	var ps []providers.Provider
	for _, id := range s.Scope {
		if p, ok := cat.Get(id); ok {
			ps = append(ps, p)
		}
	}
	verified := make([]bool, len(ps))
	token := make([]bool, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Add(2)
		go func() {
			defer wg.Done()
			verified[i] = verifyPasses(p, env, d)
		}()
		go func() {
			defer wg.Done()
			token[i] = tokenPresent(w.Name, p, d)
		}()
	}
	wg.Wait()
	for i, p := range ps {
		e := Provider{ID: p.ID, Category: p.Category, Verified: verified[i]}
		if p.CLI != nil {
			c := p.CLI.Command
			e.CLI = &c
			_, err := d.LookPath(c)
			e.CLIResolves = err == nil
		}
		e.Credential = credential(p, verified[i], token[i], d)
		var ec *providers.Connect
		if emb != nil {
			if ep, ok := emb.Get(p.ID); ok {
				ec = ep.Connect
			}
		}
		e.Connection = connection(p, ec, servers)
		s.Providers = append(s.Providers, e)
	}
	return s
}

// verifyPasses runs the provider's verify command, bounded by d.VerifyLimit, and reports
// whether it ran and passed. A command that hangs, even one that ignores its context, is
// abandoned at the limit and counts as not passed.
func verifyPasses(p providers.Provider, env []string, d Deps) bool {
	if p.Verify == nil || len(p.Verify.Args) == 0 || d.RunCLI == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), d.VerifyLimit)
	defer cancel()
	done := make(chan bool, 1)
	go func() {
		r := verify.Check(ctx, p, env, d.RunCLI)
		done <- r.Ran && r.Err == nil && r.LoggedIn
	}()
	select {
	case ok := <-done:
		return ctx.Err() == nil && ok
	case <-ctx.Done():
		return false
	}
}

// credEnvRe picks the credential variables out of a provider's match.env list: not its config
// directory or host variables.
var credEnvRe = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSWORD|API_?KEY|ACCESS_?KEY|(^|_)PAT$)`)

func credentialVars(p providers.Provider) []string {
	var out []string
	for _, e := range p.Match.Env {
		if !strings.HasSuffix(e, "*") && credEnvRe.MatchString(e) {
			out = append(out, e)
		}
	}
	return out
}

// trackerName is the name tq secret keeps a provider's token under.
func trackerName(id string) string {
	if id == "gh" {
		return "github"
	}
	return id
}

func isTracker(id string) bool {
	n := trackerName(id)
	for _, t := range []string{"github", "azure-devops", "jira", "linear", "asana"} {
		if t == n {
			return true
		}
	}
	return false
}

// KeychainLimit bounds each keychain lookup of status --json, like VerifyLimit bounds a verify
// command: a keychain that blocks must not hang the read.
const KeychainLimit = 5 * time.Second

// tokenPresent asks the keychain whether the client has a token for p, bounded by
// d.KeychainLimit. A lookup that errors, or has not answered by the limit (it is abandoned),
// counts as no token. No lookup is made for a provider with no tracker token, or when a
// credential variable of the provider is already set.
func tokenPresent(ws string, p providers.Provider, d Deps) bool {
	if !isTracker(p.ID) || envCredential(p, d) {
		return false
	}
	done := make(chan bool, 1)
	go func() {
		ok, err := d.HasToken(ws, trackerName(p.ID))
		done <- ok && err == nil
	}()
	t := time.NewTimer(d.KeychainLimit)
	defer t.Stop()
	select {
	case ok := <-done:
		return ok
	case <-t.C:
		return false
	}
}

// envCredential reports whether a credential variable of p is set in the environment (the
// value is tested for being non-empty and never kept).
func envCredential(p providers.Provider, d Deps) bool {
	for _, v := range credentialVars(p) {
		if val, ok := d.Env(v); ok && val != "" {
			return true
		}
	}
	return false
}

// credential is presence only: no token, variable value or file content is returned.
// token is tokenPresent's answer.
func credential(p providers.Provider, verified, token bool, d Deps) string {
	switch {
	case verified, token, envCredential(p, d):
		return Present
	}
	// A provider tq can check a credential for (a tracker token in the keychain, or a verify
	// command) and has none: missing. A provider that names credential variables and has no CLI
	// login (no login block) takes its credential through those variables, which tq can check:
	// missing when none is set (L-20). A CLI login with no verify command (az, gcloud) is
	// unknowable here: tq cannot tell a login from none, so the reading is none, and the skill
	// does not log in again forever (the router's decision on this ticket). Any other provider
	// takes no credential through tq: none.
	if isTracker(p.ID) || p.Verify != nil {
		return Missing
	}
	if p.Login == nil && len(credentialVars(p)) > 0 {
		return Missing
	}
	return None
}

// connection says whether an MCP server for p is registered, by name. ec is the embedded
// catalog's connect entry for p (nil when it has none): the merged catalog's copy is gone once
// a user file overrides the provider.
func connection(p providers.Provider, ec *providers.Connect, servers []string) string {
	// Every name the provider's MCP server may be registered under, the match list included: a
	// registered server reads present whatever the catalog's reach of the provider.
	want := []string{p.MCPName()}
	if ec != nil {
		want = append(want, ec.Name)
	}
	if p.Connect != nil {
		want = append(want, p.Connect.Name)
	}
	want = append(want, p.Match.MCP...)
	for _, s := range servers {
		for _, w := range want {
			if strings.EqualFold(s, w) {
				return Present
			}
		}
	}
	reached := ec != nil || p.Connect != nil || p.MCP != "" || p.Category == "chat" || p.Category == "email"
	if !reached {
		return None
	}
	return Missing
}
