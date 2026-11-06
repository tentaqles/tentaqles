package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// The PRD v2 §6.1 reference scenario at layer 4 (the hook), ADR-0007: identity company-x
// declares Azure as its only cloud; an agent reaching AWS by any route the hook sees is
// denied in the identity category with a message naming the scope, and the denial is
// logged.

const companyXBody = "git: { email: dev@company-x.example, provider: azure-devops }\n" +
	"identities: { az: {}, claude: {} }\ncloud: { provider: azure }\n"

// setupScoped writes a trusted workspace in the protected layout (ADR-0002) under an isolated
// home with the manifest body given, plus a second registered identity, beta, in the same
// base. It returns the workspace root and beta's root.
func setupScoped(t *testing.T, name, body string) (root, beta string) {
	t.Helper()
	root = mkUntrusted(t, name)
	write := func(p string, b []byte) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, manifest.FileName), manifest.MarkerBytes(name))
	mp := paths.ManifestPath(name)
	write(mp, []byte("schema: tentaqles-client-v2\nclient: "+name+"\n"+body))
	beta = filepath.Join(filepath.Dir(root), "beta")
	write(filepath.Join(beta, manifest.FileName), manifest.MarkerBytes("beta"))
	write(paths.ManifestPath("beta"), []byte("schema: tentaqles-client-v2\nclient: beta\nidentities: { aws: {} }\n"))
	write(filepath.Join(beta, "src", "main.go"), []byte("package main\n"))
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TQ_WS", name)
	t.Setenv("CLAUDE_CONFIG_DIR", paths.IdentityDir(name, "claude"))
	if err := trust.Allow(name, h); err != nil {
		t.Fatal(err)
	}
	return root, beta
}

func TestPreToolUse_ScopeReferenceScenario(t *testing.T) {
	root, beta := setupScoped(t, "company-x", companyXBody)
	home, _ := os.UserHomeDir()
	creds := filepath.Join(home, ".aws", "credentials")
	cases := []struct {
		name, tool, input, rule string // rule "" = allowed
	}{
		{"aws run-instances", "Bash", cmdIn("aws ec2 run-instances --image-id ami-12345678"), policy.OutOfScopeCLIID},
		{"aws by its install path", "PowerShell", cmdIn(`C:\Program Files\Amazon\AWSCLIV2\aws.exe ec2 run-instances --image-id ami-1`), policy.OutOfScopeCLIID},
		{"python -m awscli", "Bash", cmdIn("python -m awscli ec2 run-instances"), policy.OutOfScopeCLIID},
		{"curl to the EC2 endpoint with a credential", "Bash", cmdIn(`curl https://ec2.amazonaws.com -H "Authorization: AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE"`), policy.OutOfScopeHostID},
		{"read the AWS credentials", "Read", pathIn("file_path", creds), policy.OutOfScopePathID},
		{"cat the AWS credentials", "Bash", cmdIn("cat ~/.aws/credentials"), policy.OutOfScopePathID},
		{"an AWS MCP tool", "mcp__aws__run_instances", `{"image":"ami-1"}`, policy.OutOfScopeMCPID},
		{"an AWS variable", "Bash", cmdIn("AWS_PROFILE=prod make deploy"), policy.OutOfScopeEnvID},
		{"another identity's file", "Read", pathIn("file_path", filepath.Join(beta, "src", "main.go")), policy.OtherIdentityID},
		{"another identity's file in the shell", "Bash", cmdIn("cat ../beta/src/main.go"), policy.OtherIdentityID},
		{"another identity's Claude home", "Write", pathIn("file_path", filepath.Join(paths.IdentityDir("beta", "claude"), "projects", "x.md")), policy.OtherIdentityID},
		{"WebFetch of an AWS API", "WebFetch", `{"url":"https://sts.amazonaws.com/","prompt":"x"}`, policy.OutOfScopeHostID},
		{"az", "Bash", cmdIn("az vm list -o table"), ""},
		{"git status", "Bash", cmdIn("git status"), ""},
		{"go test", "Bash", cmdIn("go test ./..."), ""},
		{"npm install", "Bash", cmdIn("npm install"), ""},
		{"ls", "Bash", cmdIn("ls -la"), ""},
		{"grep in the repo", "Bash", cmdIn("grep -rn TODO ."), ""},
		{"read a repo file", "Read", pathIn("file_path", filepath.Join(root, "README.md")), ""},
		{"an Azure MCP tool", "mcp__azure__subscription_list", `{}`, ""},
	}
	for _, c := range cases {
		code, category, rule, errOut := hookDecision(t, root, c.tool, c.input)
		if c.rule == "" {
			if code != 0 {
				t.Errorf("%s: denied: %s", c.name, errOut)
			}
			continue
		}
		if code != 2 || category != policy.CategoryIdentity || rule != c.rule {
			t.Errorf("%s: code=%d category=%q rule=%q, want identity/%s (%s)", c.name, code, category, rule, c.rule, errOut)
			continue
		}
		if c.rule != policy.OtherIdentityID && (!strings.Contains(errOut, "BLOCKED [identity/"+c.rule+"]") ||
			!strings.Contains(errOut, "Scope of identity company-x: az, azure-devops, claude") || !strings.Contains(errOut, "do not retry")) {
			t.Errorf("%s: the deny message does not name the category and the scope: %q", c.name, errOut)
		}
	}

	// Every deny is one line in the log with the identity, the rule and the layer; the
	// command is the verb and the subject only.
	raw, err := os.ReadFile(filepath.Join(paths.Home(), "denials.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var first audit.Denial
	if err := json.Unmarshal([]byte(strings.SplitN(string(raw), "\n", 2)[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Category != policy.CategoryIdentity || first.Rule != policy.OutOfScopeCLIID || first.Layer != audit.LayerHook ||
		first.Identity != "company-x" || first.Subject != "aws" {
		t.Fatalf("denial log line = %+v", first)
	}
	if strings.Contains(string(raw), "AKIDEXAMPLE") || strings.Contains(string(raw), "ami-12345678") {
		t.Fatalf("the denial log holds command arguments: %s", raw)
	}
	// identity denies log layer hook, other-identity paths layer filesystem (P2.2 log fields).
	sawOther := false
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var d audit.Denial
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			t.Fatal(err)
		}
		if d.Category != policy.CategoryIdentity {
			t.Errorf("an identity deny logged category %q: %+v", d.Category, d)
		}
		want := audit.LayerHook
		if d.Rule == policy.OtherIdentityID {
			want, sawOther = audit.LayerFilesystem, true
		}
		if d.Layer != want {
			t.Errorf("rule %s logged layer %q, want %q", d.Rule, d.Layer, want)
		}
	}
	if !sawOther {
		t.Error("no other-identity deny was logged")
	}
}

// TestPreToolUse_ScopeRuleOrder: an AWS credential read is the identity category's: it runs
// before secrets (§6.3).
func TestPreToolUse_ScopeRuleOrder(t *testing.T) {
	root, _ := setupScoped(t, "company-x", companyXBody)
	home, _ := os.UserHomeDir()
	_, category, rule, _ := hookDecision(t, root, "Read", pathIn("file_path", filepath.Join(home, ".aws", "credentials")))
	if want := policy.CategoryIdentity + "/" + policy.OutOfScopePathID; category+"/"+rule != want {
		t.Errorf("%s/%s, want %s", category, rule, want)
	}
}

// TestPreToolUse_ManifestNeedsNoFlag: there is one guard. A manifest with no guard key gets
// all of it: every route to another provider, and another identity's folder, is denied.
func TestPreToolUse_ManifestNeedsNoFlag(t *testing.T) {
	root, beta := setupScoped(t, "company-x", companyXBody)
	for _, c := range []struct{ tool, input string }{
		{"Bash", cmdIn("aws ec2 run-instances")},
		{"Bash", cmdIn("python -m awscli ec2 run-instances")},
		{"Bash", cmdIn("curl https://ec2.amazonaws.com")},
		{"mcp__aws__run_instances", `{}`},
		{"Read", pathIn("file_path", filepath.Join(beta, "src", "main.go"))},
	} {
		if code, category, _, errOut := hookDecision(t, root, c.tool, c.input); code != 2 || category != policy.CategoryIdentity {
			t.Errorf("%s %s: code=%d category=%q: %s", c.tool, c.input, code, category, errOut)
		}
	}
	if code, _, _, errOut := hookDecision(t, root, "Bash", cmdIn("az group list")); code != 0 {
		t.Errorf("the identity's own cloud is denied: %s", errOut)
	}
}

// TestPreToolUse_UnreadableManifestDeniesAll: a workspace folder whose protected manifest
// cannot be read (an unknown scope id, bad YAML) denies every tool with
// tq/guard-error, instead of falling back to neutral (ADR-0007 round 2).
func TestPreToolUse_UnreadableManifestDeniesAll(t *testing.T) {
	for _, bad := range []string{"scope: [awss]\n", "client: company-x\n:\n"} {
		root := mkUntrusted(t, "company-x")
		if err := os.WriteFile(filepath.Join(root, manifest.FileName), manifest.MarkerBytes("company-x"), 0o600); err != nil {
			t.Fatal(err)
		}
		mp := paths.ManifestPath("company-x")
		if err := os.MkdirAll(filepath.Dir(mp), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: company-x\n"+bad), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct{ tool, input string }{
			{"Read", pathIn("file_path", filepath.Join(root, "README.md"))},
			{"Bash", cmdIn("cat ~/.aws/config")},
		} {
			code, _, rule, errOut := hookDecision(t, root, c.tool, c.input)
			if code != 2 || rule != policy.GuardErrorID {
				t.Fatalf("%q %s: code=%d rule=%q, want guard-error (%s)", bad, c.tool, code, rule, errOut)
			}
		}
	}
}

// TestPreToolUse_BadProviderFiles: a user provider file that fails to parse or validate
// (ADR-0007 round 3). One that overrides a built-in id leaves the built-in lists in force and
// the hook warns; one with a new id refuses calls only in a workspace whose scope names that
// id; a new id that claims a built-in's entry is refused at load. Every message names the
// full path, the first error and the fix.
func TestPreToolUse_BadProviderFiles(t *testing.T) {
	const broken = "name: [broken\n"
	cases := []struct {
		name, file, body string
		scopeExtra       string // appended to company-x's manifest
		tool, input      string
		code             int
		rule             string // the denying rule, or "" when allowed
		warn             bool   // the hook prints the bad-file warning
	}{
		{"a broken override keeps the built-in lists: gh still denied", "gh.yaml", "id: gh\n" + broken, "",
			"Bash", cmdIn("gh pr list"), 2, policy.OutOfScopeCLIID, true},
		{"a broken override: ordinary work goes on, with a warning", "gh.yaml", "id: gh\n" + broken, "",
			"Bash", cmdIn("git status"), 0, "", true},
		{"an invalid override (bad match entry) keeps the built-in lists", "aws.yaml",
			"id: aws\nname: AWS\ncategory: cloud\nmatch:\n  executables: [\"a b\"]\n", "",
			"Bash", cmdIn("aws s3 ls"), 2, policy.OutOfScopeCLIID, true},
		{"a broken new id outside the scope: work goes on, with a warning", "zz.yaml", "id: zz\n" + broken, "",
			"Bash", cmdIn("git status"), 0, "", true},
		{"an unknown key is an error with its line, column and a suggestion", "acme.yaml",
			"id: acme\nname: Acme\ncategory: cloud\nmatch:\n  executable: [acmectl]\n", "",
			"Bash", cmdIn("git status"), 0, "", true},
		{"a broken new id the scope names refuses every call", "zz.yaml", "id: zz\n" + broken, "scope: [zz]\n",
			"Bash", cmdIn("git status"), 2, policy.GuardErrorID, false},
		{"a new id claiming a built-in's executable is refused at load", "acme.yaml",
			"id: acme\nname: Acme\ncategory: cloud\nmatch:\n  executables: [aws]\n", "",
			"Bash", cmdIn("aws s3 ls"), 2, policy.OutOfScopeCLIID, true},
		{"a refused new id the scope names refuses every call", "acme.yaml",
			"id: acme\nname: Acme\ncategory: cloud\nmatch:\n  executables: [aws]\n", "scope: [acme]\n",
			"Read", pathIn("file_path", "README.md"), 2, policy.GuardErrorID, false},
	}
	for _, c := range cases {
		root, _ := setupScoped(t, "company-x", companyXBody+c.scopeExtra)
		dir := filepath.Join(paths.Home(), "providers")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, c.file)
		if err := os.WriteFile(file, []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		code, _, rule, errOut := hookDecision(t, root, c.tool, c.input)
		if code != c.code || rule != c.rule {
			t.Errorf("%s: code=%d rule=%q, want %d %q (%s)", c.name, code, rule, c.code, c.rule, errOut)
			continue
		}
		const fix = "fix or delete it, then run tq providers list"
		if c.rule == policy.GuardErrorID {
			if !strings.Contains(errOut, file) || !strings.Contains(errOut, fix) {
				t.Errorf("%s: the refusal does not name the file and the fix: %q", c.name, errOut)
			}
			if n := strings.Count(errOut, file); n != 1 {
				t.Errorf("%s: the refusal names the file %d times, want once: %q", c.name, n, errOut)
			}
		}
		hasWarn := strings.Contains(errOut, "warning") && strings.Contains(errOut, file) && strings.Contains(errOut, fix)
		if hasWarn != c.warn {
			t.Errorf("%s: warning printed=%v, want %v: %q", c.name, hasWarn, c.warn, errOut)
		}
		if strings.Contains(c.body, "executable:") && (!strings.Contains(errOut, file+":5:3") || !strings.Contains(errOut, `did you mean "executables"`)) {
			t.Errorf("%s: the unknown key is not located and explained: %q", c.name, errOut)
		}
	}
}

// TestProviders_BadFilesReported: tq providers list exits non-zero on a bad user file and
// names it with the first error and the fix; tq doctor lists it as a finding; an overlap
// between host suffixes is reported (ADR-0007 round 3).
func TestProviders_BadFilesReported(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", home)
	dir := filepath.Join(home, "providers")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "zz.yaml")
	if err := os.WriteFile(bad, []byte("id: zz\nname: [broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// two valid new ids, one host under the other's: a suffix overlap
	for name, body := range map[string]string{
		"acme.yaml":   "id: acme\nname: Acme\ncategory: cloud\nmatch:\n  hosts: [acme.example]\n",
		"acme-2.yaml": "id: acme-2\nname: Acme 2\ncategory: cloud\nmatch:\n  hosts: [api.acme.example]\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root := NewRoot()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"providers", "list"})
	err := root.Execute()
	if err == nil {
		t.Fatal("tq providers list exited zero with a bad provider file")
	}
	all := errOut.String() + err.Error()
	for _, want := range []string{bad, "fix or delete it, then run tq providers list", "api.acme.example"} {
		if !strings.Contains(all, want) {
			t.Errorf("providers list does not report %q: %s", want, all)
		}
	}

	prevExit := exitFunc
	exitFunc = func(int) {}
	t.Cleanup(func() { exitFunc = prevExit })
	root = NewRoot()
	out.Reset()
	root.SetOut(&out)
	root.SetArgs([]string{"doctor", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var findings []struct{ Code, Msg, Fix string }
	if err := json.Unmarshal(out.Bytes(), &findings); err != nil {
		t.Fatalf("doctor --json: %v (%s)", err, out.String())
	}
	found := false
	for _, f := range findings {
		if f.Code == "provider-file-invalid" && strings.Contains(f.Msg, bad) && strings.Contains(f.Fix, "tq providers list") {
			found = true
			// no workspace names zz: no hint about one, and the fix is said once, in Fix
			if strings.Contains(f.Msg, "scope names") || strings.Contains(f.Msg, "refused") || strings.Contains(f.Msg, "tq providers list") {
				t.Errorf("doctor's message carries a hint that does not apply or repeats the fix: %q", f.Msg)
			}
		}
	}
	if !found {
		t.Fatalf("doctor does not list the bad provider file: %+v", findings)
	}
}

// TestPreToolUse_OldGuardKeysStillGuarded: there is one guard. A manifest with an old guard key
// (the dropped modes mapping, or guard: v1) gets it like any other, and tq doctor still reports
// the dropped mapping (manifest.Ignored).
func TestPreToolUse_OldGuardKeysStillGuarded(t *testing.T) {
	root, _ := setupScoped(t, "company-x", "guard:\n  mode: strict\n"+companyXBody)
	if code, _, _, _ := hookDecision(t, root, "Bash", cmdIn("python -m awscli s3 ls")); code != 2 {
		t.Fatal("a guard mapping must not switch the guard off")
	}
	m, err := manifest.Load(paths.ManifestPath("company-x"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Ignored) == 0 {
		t.Fatalf("guard mapping: Ignored=%v", m.Ignored)
	}
	root, _ = setupScoped(t, "company-y", "guard: v1\n"+companyXBody)
	if code, _, _, _ := hookDecision(t, root, "Bash", cmdIn("python -m awscli s3 ls")); code != 2 {
		t.Fatal("guard: v1 must not switch the guard off")
	}
}

// TestPreToolUse_ScopeNeutralIsV1: a cwd in no workspace has no scope: the v2 rules do not
// run there, and the v1 neutral-remote rule still refuses a cloud CLI.
func TestPreToolUse_ScopeNeutralIsV1(t *testing.T) {
	setupScoped(t, "company-x", companyXBody)
	neutral := t.TempDir()
	if code, _, rule, _ := hookDecision(t, neutral, "Bash", cmdIn("aws s3 ls")); code != 2 || rule != "neutral-remote" {
		t.Fatalf("neutral aws: code=%d rule=%q", code, rule)
	}
	if code, _, _, errOut := hookDecision(t, neutral, "Bash", cmdIn("curl https://ec2.amazonaws.com")); code != 0 {
		t.Fatalf("neutral curl was judged by a scope: %s", errOut)
	}
}
