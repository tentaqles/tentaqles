package cmd

import (
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

// The PRD v2 §6.1 reference scenario at layer 4 (the hook), ADR-0006: identity company-x
// declares Azure as its only cloud; an agent reaching AWS by any route the hook sees is
// denied in the identity category with a message naming the scope, and the denial is
// logged. Without guard: v2 the same identity keeps the v1 rules.

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
	root, beta := setupScoped(t, "company-x", "guard: v2\n"+companyXBody)
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
			!strings.Contains(errOut, "scope: az, azure-devops, claude") || !strings.Contains(errOut, "do not retry")) {
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
	if first.Kind != policy.CategoryIdentity || first.Rule != policy.OutOfScopeCLIID || first.Layer != audit.LayerHook ||
		first.Workspace != "company-x" || first.Command != "aws" {
		t.Fatalf("denial log line = %+v", first)
	}
	if strings.Contains(string(raw), "AKIDEXAMPLE") || strings.Contains(string(raw), "ami-12345678") {
		t.Fatalf("the denial log holds command arguments: %s", raw)
	}
}

// TestPreToolUse_ScopeRuleOrder: an AWS credential read under guard: v2 is the identity
// category's (it runs before secrets, §6.3); without the flag it is the secret rule's, as
// before.
func TestPreToolUse_ScopeRuleOrder(t *testing.T) {
	for _, v2 := range []bool{true, false} {
		body := companyXBody
		if v2 {
			body = "guard: v2\n" + body
		}
		root, _ := setupScoped(t, "company-x", body)
		home, _ := os.UserHomeDir()
		_, category, rule, _ := hookDecision(t, root, "Read", pathIn("file_path", filepath.Join(home, ".aws", "credentials")))
		want := policy.CategoryIdentity + "/" + policy.OutOfScopePathID
		if !v2 {
			want = policy.CategorySecrets + "/tq/secret-store-read"
		}
		if category+"/"+rule != want {
			t.Errorf("v2=%v: %s/%s, want %s", v2, category, rule, want)
		}
	}
}

// TestPreToolUse_ScopeV1Unchanged: without guard: v2 the identity keeps the v1 rules: an AWS
// CLI is wrong-cloud, and nothing else of the scope is judged.
func TestPreToolUse_ScopeV1Unchanged(t *testing.T) {
	root, beta := setupScoped(t, "company-x", companyXBody)
	code, category, rule, _ := hookDecision(t, root, "Bash", cmdIn("aws ec2 run-instances"))
	if code != 2 || category != "identity" || rule != "wrong-cloud" {
		t.Fatalf("v1 aws: code=%d %s/%s", code, category, rule)
	}
	for _, c := range []struct{ tool, input string }{
		{"Bash", cmdIn("python -m awscli ec2 run-instances")},
		{"Bash", cmdIn("curl https://ec2.amazonaws.com")},
		{"mcp__aws__run_instances", `{}`},
		{"Bash", cmdIn("gh pr list")},
		{"Read", pathIn("file_path", filepath.Join(beta, "src", "main.go"))},
	} {
		if code, _, _, errOut := hookDecision(t, root, c.tool, c.input); code != 0 {
			t.Errorf("v1 %s %s: denied: %s", c.tool, c.input, errOut)
		}
	}
}

// TestPreToolUse_ScopeNeutralIsV1: a cwd in no workspace has no scope: the v2 rules do not
// run there, and the v1 neutral-remote rule still refuses a cloud CLI.
func TestPreToolUse_ScopeNeutralIsV1(t *testing.T) {
	setupScoped(t, "company-x", "guard: v2\n"+companyXBody)
	neutral := t.TempDir()
	if code, _, rule, _ := hookDecision(t, neutral, "Bash", cmdIn("aws s3 ls")); code != 2 || rule != "neutral-remote" {
		t.Fatalf("neutral aws: code=%d rule=%q", code, rule)
	}
	if code, _, _, errOut := hookDecision(t, neutral, "Bash", cmdIn("curl https://ec2.amazonaws.com")); code != 0 {
		t.Fatalf("neutral curl was judged by a scope: %s", errOut)
	}
}
