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
	// variable of the provider is set, or the provider's verify command passed; missing when the
	// provider takes one and none is there; none when it takes none through tq.
	Credential string `json:"credential"`
	// Connection is present when an MCP server for the provider is registered in the client's
	// .claude.json, missing when it is not, none for a provider reached by no MCP server.
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

	cat := providers.MustLoad()
	var env []string
	if d.Environ != nil {
		env = d.Environ(w)
	} else {
		env = envplan.Environ(w, os.Environ())
	}
	servers := doctor.MCPServerNames(filepath.Join(paths.IdentityDir(w.Name, "claude"), ".claude.json"), w.Root)

	var ps []providers.Provider
	for _, id := range s.Scope {
		if p, ok := cat.Get(id); ok {
			ps = append(ps, p)
		}
	}
	verified := make([]bool, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			verified[i] = verifyPasses(p, env, d)
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
		e.Credential = credential(w.Name, p, verified[i], d)
		e.Connection = connection(p, servers)
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

// credential is presence only: no token, variable value or file content is returned.
func credential(ws string, p providers.Provider, verified bool, d Deps) string {
	vars := credentialVars(p)
	takes := isTracker(p.ID) || len(vars) > 0 || p.Login != nil || p.Verify != nil
	if !takes {
		return None
	}
	if verified {
		return Present
	}
	if isTracker(p.ID) {
		if ok, _ := secret.Has(ws, trackerName(p.ID)); ok {
			return Present
		}
	}
	for _, v := range vars {
		if val, ok := d.Env(v); ok && val != "" {
			return Present
		}
	}
	return Missing
}

// connection says whether an MCP server for p is registered, by name.
func connection(p providers.Provider, servers []string) string {
	reached := p.Connect != nil || p.MCP != "" || p.Category == "chat" || p.Category == "email"
	if !reached {
		return None
	}
	want := []string{p.MCPName()}
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
	return Missing
}
