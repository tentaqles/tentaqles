package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
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

// TestScope_EveryProvider: for every catalog provider, every entry of every match list (each
// executable, subcommand, image, module, PowerShell module and cmdlet, env name, credential
// path including %LOCALAPPDATA% ones, API host through WebFetch and through a shell command,
// and MCP token) is denied under an identity without the provider and allowed under one with
// it. The counts are exact, so a catalog change updates this test.
func TestScope_EveryProvider(t *testing.T) {
	f := newScopeFixture(t, "claude")
	env := func(k string) string {
		return map[string]string{"TQ_HOME": f.state, "APPDATA": f.appData, "LOCALAPPDATA": f.localAppData}[k]
	}
	cat, err := providers.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	out := scopeOver(t, f.home, f.set, env, []string{})
	providersWithSignals, checks := 0, 0
	for _, p := range cat.All() {
		if p.Match.Empty() {
			continue
		}
		providersWithSignals++
		in := scopeOver(t, f.home, f.set, env, []string{p.ID})
		check := func(label string, call ToolCall) {
			checks++
			if d := out.Evaluate(f.cwd, call); d.Action != Deny {
				t.Errorf("%s %s %+v: not denied when %s is out of scope", p.ID, label, call, p.ID)
			}
			if d := in.Evaluate(f.cwd, call); d.Action != Allow {
				t.Errorf("%s %s %+v: denied when %s is in scope: %s", p.ID, label, call, p.ID, d.Reason())
			}
		}
		m := p.Match
		for _, e := range m.Executables {
			check("exe "+e, bash(e+" something"))
		}
		for exe, subs := range m.Subcommands {
			for _, sub := range subs {
				check("subcommand "+exe+" "+sub, bash(exe+" "+sub+" x"))
			}
		}
		for _, img := range m.Images {
			check("image "+img, bash("docker run --rm "+img+" x"))
		}
		for _, mod := range m.Modules {
			check("module "+mod, bash("python -m "+mod))
		}
		for _, mod := range m.PSModules {
			check("psmodule "+mod, pwsh("Import-Module "+mod))
		}
		for _, c := range m.Cmdlets {
			name := strings.TrimSuffix(c, "*")
			if strings.HasSuffix(c, "*") {
				name += "Thing"
			}
			name = strings.Replace(name, "*-", "Get-", 1)
			check("cmdlet "+c, pwsh(name+" -Name x"))
		}
		for _, e := range m.Env {
			name := strings.TrimSuffix(e, "*")
			if strings.HasSuffix(e, "*") {
				name += "X"
			}
			check("env "+e, bash("echo $"+name))
		}
		for _, spec := range m.Paths {
			var abs string
			switch {
			case strings.HasPrefix(spec, "~/"):
				abs = filepath.Join(f.home, filepath.FromSlash(spec[2:]))
			case strings.HasPrefix(spec, "%APPDATA%/"):
				abs = filepath.Join(f.appData, filepath.FromSlash(spec[len("%APPDATA%/"):]))
			case strings.HasPrefix(spec, "%LOCALAPPDATA%/"):
				abs = filepath.Join(f.localAppData, filepath.FromSlash(spec[len("%LOCALAPPDATA%/"):]))
			default:
				t.Fatalf("%s: path %q has no known root", p.ID, spec)
			}
			check("path "+spec, ToolCall{Tool: "Read", Path: abs})
		}
		for _, h := range m.Hosts {
			host, path, _ := strings.Cut(h, "/")
			url := "https://sub." + host + "/" + path + "x"
			check("host "+h+" (WebFetch)", ToolCall{Tool: "WebFetch", URL: url})
			check("host "+h+" (shell)", bash("curl -s "+url))
		}
		for _, tok := range m.MCP {
			check("mcp "+tok, ToolCall{Tool: "mcp__" + tok + "__x", Content: "{}"})
		}
	}
	if providersWithSignals != wantProvidersWithSignals || checks != wantEveryProviderChecks {
		t.Fatalf("%d providers with signals and %d checks; want %d and %d (update the counts with the catalog)",
			providersWithSignals, checks, wantProvidersWithSignals, wantEveryProviderChecks)
	}

	// Two routes the per-list loop does not spell: the LS tool on another identity's folder,
	// and a PowerShell path with a quoted segment under %APPDATA%.
	runScopeCases(t, scopeFixture{s: out, home: f.home, cwd: f.cwd}, []scopeCase{
		{"Get-ChildItem of a quoted %APPDATA% segment", pwsh(`Get-ChildItem $env:APPDATA\'GitHub CLI'`), OutOfScopePathID},
		{"Get-ChildItem of a double-quoted %APPDATA% path", pwsh(`Get-ChildItem "$env:APPDATA\GitHub CLI\hosts.yml"`), OutOfScopePathID},
	})
	runScopeCases(t, f, []scopeCase{
		{"LS on another identity's folder", ToolCall{Tool: "LS", Path: f.betaRoot}, OtherIdentityID},
		{"LS on an out-of-scope credential folder", ToolCall{Tool: "LS", Path: filepath.Join(f.home, ".aws")}, OutOfScopePathID},
		{"LS on its own folder", ToolCall{Tool: "LS", Path: f.cwd}, ""},
	})
}

// The exact sizes of the catalog's match lists that TestScope_EveryProvider walks.
const (
	wantProvidersWithSignals = 57
	wantEveryProviderChecks  = 584
)

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

	// A user file with a new id may not claim what a built-in provider lists: it is refused
	// at load (with the file, the entry, the built-in that owns it and the fix), so an
	// identity that scopes the new id gains nothing. One table row per list.
	prev := paths.AllowHomeOverride
	paths.AllowHomeOverride = true
	t.Cleanup(func() { paths.AllowHomeOverride = prev })
	for _, c := range []struct{ list, entry, owner, call string }{
		{"executables", "aws", "aws", "aws s3 ls"},
		{"subcommands", "{az: [vm]}", "az", "az vm list"},
		{"images", "amazon/aws-cli", "aws", "docker run amazon/aws-cli s3 ls"},
		{"modules", "boto3", "aws", `python -c "import boto3"`},
		{"env", "AWS_PROFILE", "aws", "echo $AWS_PROFILE"},
		{"paths", "~/.aws/sso", "aws", "cat ~/.aws/sso/cache/x.json"},
		{"hosts", "ec2.amazonaws.com", "aws", "curl https://ec2.amazonaws.com/"},
		{"mcp", "aws", "aws", ""},
		{"psmodules", "AWS.Tools.S3", "aws", "Import-Module AWS.Tools.S3"},
		{"cmdlets", "Get-S3Bucket", "aws", "Get-S3Bucket"},
	} {
		home := t.TempDir()
		t.Setenv("TQ_HOME", home)
		dir := filepath.Join(home, "providers")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "acme.yaml")
		entry := "[" + c.entry + "]"
		if c.list == "subcommands" {
			entry = c.entry
		}
		body := "id: acme\nname: Acme\ncategory: cloud\nmatch:\n  " + c.list + ": " + entry + "\n"
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		uc, errs := providers.LoadAll()
		if len(errs) != 1 {
			t.Fatalf("%s: a new id claiming the built-in %s's %q loaded (errors %v)", c.list, c.owner, c.entry, errs)
		}
		msg := errs[0].Error()
		for _, want := range []string{file, c.owner, "fix or delete " + file + ", then run tq providers list"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: the refusal %q does not name %q", c.list, msg, want)
			}
		}
		if _, ok := uc.Get("acme"); ok {
			t.Errorf("%s: the refused provider is in the catalog", c.list)
		}
		if c.call == "" {
			continue
		}
		us := NewScope(ScopeInputs{
			Identity: "company-x", Scope: []string{"claude", "acme"}, Providers: uc.All(), Embedded: embedded,
			Set: f.set, UserHome: f.home, Env: env,
		})
		call := bash(c.call)
		if c.list == "psmodules" || c.list == "cmdlets" {
			call = pwsh(c.call)
		}
		if d := us.Evaluate(f.cwd, call); d.Action != Deny {
			t.Errorf("%s: %q allowed through a refused user provider", c.list, c.call)
		}
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
