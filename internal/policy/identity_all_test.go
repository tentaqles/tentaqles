package policy

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/protect"
	"github.com/tentaqles/tentaqles/internal/providers"
)

// scopeOver builds a Scope for the fixture home with an explicit scope and the whole embedded
// catalog, so the per-provider table can flip one provider in and out of scope.
func scopeOver(t *testing.T, home string, set *protect.Set, env func(string) string, scope []string) *Scope {
	t.Helper()
	cat, err := providers.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	embedded := map[string]providers.Match{}
	for _, p := range cat.All() {
		embedded[p.ID] = p.Match
	}
	return NewScope(ScopeInputs{
		Identity: "company-x", Scope: scope, Providers: cat.All(), Embedded: embedded,
		Set: set, UserHome: home, Env: env,
	})
}

// TestScope_EveryProvider: for all 62 catalog providers, every signal it declares (an
// executable or subcommand, a module, an env var, a credential path, an API host, an MCP
// server) is denied under an identity without it and allowed under one with it.
func TestScope_EveryProvider(t *testing.T) {
	f := newScopeFixture(t, "claude")
	env := func(k string) string { return map[string]string{"TQ_HOME": f.state, "APPDATA": f.appData}[k] }
	cat, err := providers.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range cat.All() {
		if p.Match.Empty() {
			continue
		}
		n++
		out := scopeOver(t, f.home, f.set, env, []string{})
		in := scopeOver(t, f.home, f.set, env, []string{p.ID})
		check := func(label string, call ToolCall) {
			if d := out.Evaluate(f.cwd, call); d.Action != Deny {
				t.Errorf("%s %s: not denied when %s is out of scope", p.ID, label, p.ID)
			}
			if d := in.Evaluate(f.cwd, call); d.Action != Allow {
				t.Errorf("%s %s: denied when %s is in scope: %s", p.ID, label, p.ID, d.Reason())
			}
		}
		if len(p.Match.Executables) > 0 {
			check("exe", bash(p.Match.Executables[0]+" something"))
		}
		for exe, subs := range p.Match.Subcommands {
			if len(subs) > 0 {
				check("subcommand", bash(exe+" "+subs[0]+" x"))
				break
			}
		}
		if len(p.Match.Modules) > 0 {
			check("module", bash("python -m "+p.Match.Modules[0]))
		}
		for _, e := range p.Match.Env {
			if !strings.HasSuffix(e, "*") {
				check("env", bash("echo $"+e))
				break
			}
		}
		if len(p.Match.Paths) > 0 {
			rest := p.Match.Paths[0]
			var abs string
			switch {
			case strings.HasPrefix(rest, "~/"):
				abs = filepath.Join(f.home, filepath.FromSlash(rest[2:]))
			case strings.HasPrefix(rest, "%APPDATA%/"):
				abs = filepath.Join(f.appData, filepath.FromSlash(rest[len("%APPDATA%/"):]))
			}
			if abs != "" {
				check("path", ToolCall{Tool: "Read", Path: filepath.Join(abs, "x")})
			}
		}
		if len(p.Match.Hosts) > 0 {
			check("host", ToolCall{Tool: "WebFetch", URL: "https://sub." + p.Match.Hosts[0] + "/x"})
		}
		if len(p.Match.MCP) > 0 {
			check("mcp", ToolCall{Tool: "mcp__" + p.Match.MCP[0] + "__x", Content: "{}"})
		}
	}
	if n < 50 {
		t.Fatalf("only %d providers had signals; expected most of the 62", n)
	}
}

// TestScope_SharedCLIPrecision: azure-devops owns only the `az devops|repos|...` subcommands;
// an identity with azure-devops (not az) runs those but not `az vm create`, and reads
// ~/.azure only with az in scope (ADR-0007 round 2, the biggest round-1 gap).
func TestScope_SharedCLIPrecision(t *testing.T) {
	f := newScopeFixture(t, "claude")
	env := func(k string) string { return map[string]string{"TQ_HOME": f.state, "APPDATA": f.appData}[k] }
	devopsOnly := scopeOver(t, f.home, f.set, env, []string{"claude", "azure-devops"})
	azOnly := scopeOver(t, f.home, f.set, env, []string{"claude", "az"})
	azure := filepath.Join(f.home, ".azure", "azureProfile.json")
	cases := []struct {
		s    *Scope
		call ToolCall
		deny bool
	}{
		{devopsOnly, bash("az devops project list"), false},
		{devopsOnly, bash("az repos pr list"), false},
		{devopsOnly, bash("az login"), false},
		{devopsOnly, bash("az vm create -g rg -n vm"), true},
		{devopsOnly, bash("az storage account list"), true},
		{devopsOnly, ToolCall{Tool: "Read", Path: azure}, true},
		{azOnly, bash("az vm create -g rg -n vm"), false},
		{azOnly, bash("az devops project list"), false},
		{azOnly, ToolCall{Tool: "Read", Path: azure}, false},
	}
	for _, c := range cases {
		d := c.s.Evaluate(f.cwd, c.call)
		if (d.Action == Deny) != c.deny {
			t.Errorf("%q %q: deny=%v, want %v (%s)", c.call.Command, c.call.Path, d.Action == Deny, c.deny, d.Reason())
		}
	}
}

// TestScope_UserOverrideCannotAllow: a user file can only widen denial. Overriding an
// in-scope provider to claim another vendor's CLI does not make that CLI allowed; adding a
// new out-of-scope provider or a match entry to an out-of-scope one does deny.
func TestScope_UserOverrideCannotAllow(t *testing.T) {
	f := newScopeFixture(t, "claude")
	cat, _ := providers.Embedded()
	embedded := map[string]providers.Match{}
	merged := []providers.Provider{}
	for _, p := range cat.All() {
		embedded[p.ID] = p.Match
		merged = append(merged, p)
	}
	// az is in scope; a user file tries to make az claim aws (so aws would be "allowed").
	for i := range merged {
		if merged[i].ID == "az" {
			merged[i].Match = merged[i].Match.Union(providers.Match{Executables: []string{"aws"}})
		}
	}
	env := func(k string) string { return map[string]string{"TQ_HOME": f.state}[k] }
	s := NewScope(ScopeInputs{
		Identity: "company-x", Scope: []string{"claude", "az"}, Providers: merged, Embedded: embedded,
		Set: f.set, UserHome: f.home, Env: env,
	})
	if d := s.Evaluate(f.cwd, bash("aws s3 ls")); d.Action != Deny {
		t.Fatal("a user override of in-scope az claiming aws made aws allowed")
	}
}

// TestScope_MCPExactAllowAndPrecedence: an out-of-scope token denies even when another token
// is an in-scope provider's (aws_on_azure), unless the exact server is in the identity's own
// bundle.mcp (ADR-0007 round 2).
func TestScope_MCPExactAllowAndPrecedence(t *testing.T) {
	f := newScopeFixture(t) // az, azure-devops, claude
	mcp := func(server string) ToolCall { return ToolCall{Tool: "mcp__" + server + "__call", Content: "{}"} }
	for server, deny := range map[string]bool{
		"aws_on_azure": true, "azure": false, "aws": true, "amazon-q": true, "bedrock": true,
		"google-cloud": true, "github": true, "memory": false, "microsoft-learn": false,
	} {
		if d := f.s.Evaluate(f.cwd, mcp(server)); (d.Action == Deny) != deny {
			t.Errorf("mcp %s: deny=%v, want %v", server, d.Action == Deny, deny)
		}
	}
	cat, _ := providers.Embedded()
	embedded := map[string]providers.Match{}
	for _, p := range cat.All() {
		embedded[p.ID] = p.Match
	}
	env := func(k string) string { return map[string]string{"TQ_HOME": f.state}[k] }
	withBundle := NewScope(ScopeInputs{
		Identity: "company-x", Scope: []string{"claude", "az", "azure-devops"}, Providers: cat.All(), Embedded: embedded,
		BundleMCP: []string{"aws_on_azure"}, Set: f.set, UserHome: f.home, Env: env,
	})
	if d := withBundle.Evaluate(f.cwd, mcp("aws_on_azure")); d.Action != Allow {
		t.Fatal("a server declared in the identity's own bundle.mcp must be allowed")
	}
}

// TestScope_Mutants pins the seven round-1 mutants named by the critic.
func TestScope_Mutants(t *testing.T) {
	f := newScopeFixture(t) // az, azure-devops, claude
	deny := func(name string, call ToolCall) {
		if d := f.s.Evaluate(f.cwd, call); d.Action != Deny {
			t.Errorf("%s: not denied (%s)", name, d.Reason())
		}
	}
	allow := func(name string, call ToolCall) {
		if d := f.s.Evaluate(f.cwd, call); d.Action != Allow {
			t.Errorf("%s: denied (%s)", name, d.Reason())
		}
	}
	// 1. MCP precedence: out-of-scope token denies despite an in-scope token.
	deny("mcp precedence", ToolCall{Tool: "mcp__aws_on_azure__call_aws", Content: "{}"})
	// 2. uv tool run.
	deny("uv tool run", bash("uv tool run awscli s3 ls"))
	allow("uv run non-vendor", bash("uv run pytest"))
	// 3. env prefix in-scope check: ARM_* is az's (in scope), TF_TOKEN_* is terraform's (out).
	allow("in-scope ARM assignment", bash("export ARM_CLIENT_ID=x"))
	deny("out-of-scope prefix env", bash("export TF_TOKEN_app_terraform_io=secret"))
	// 4. path in-scope with az, out without azure-devops.
	azure := filepath.Join(f.home, ".azure", "x")
	allow("az path in scope", ToolCall{Tool: "Read", Path: azure})
	devopsOnly := scopeOver(t, f.home, f.set, func(k string) string { return map[string]string{"TQ_HOME": f.state}[k] }, []string{"claude", "azure-devops"})
	if d := devopsOnly.Evaluate(f.cwd, ToolCall{Tool: "Read", Path: azure}); d.Action != Deny {
		t.Error("~/.azure must deny for an azure-devops-only identity (it belongs to az)")
	}
	// 5. %APPDATA% expansion in mentionHit (shell mention of a credential path).
	deny("appdata mention", bash(`type %APPDATA%\gcloud\credentials.db`))
	// 6. MSYS spelling.
	msysAws := "/" + strings.ToLower(filepath.VolumeName(f.home)[:1]) + filepath.ToSlash(f.home[2:]) + "/.aws/credentials"
	deny("MSYS spelling", bash("cat "+msysAws))
	// 7. mention word boundary in the shell: a longer name is not the credential path.
	allow("word boundary", bash("cat ~/.aws-notes/readme"))
}
