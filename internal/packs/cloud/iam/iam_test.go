package iam

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var testOpts = Options{
	Accounts: []string{"111111111111"},
	Tenants:  []string{"00000000-0000-0000-0000-000000000000"},
}

func lintFixture(t *testing.T, name string) Result {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	res, handled, err := File(name, raw, testOpts)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if !handled {
		t.Fatalf("%s: not handled", name)
	}
	return res
}

func render(fs []packs.Finding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// byAddr groups "level RULE" by the address that prefixes each message.
func byAddr(fs []packs.Finding) map[string][]string {
	m := map[string][]string{}
	for _, f := range fs {
		addr, _, _ := strings.Cut(f.Message, ": ")
		m[addr] = append(m[addr], f.Level+" "+f.Rule)
	}
	for k := range m {
		sort.Strings(m[k])
	}
	return m
}

func TestGolden(t *testing.T) {
	for _, name := range []string{"plan.json", "template.yaml", "azuredeploy.json", "whatif.json", "preview.json",
		"policies/github-trust.json", "policies/identity.json", "policies/safe.json"} {
		t.Run(name, func(t *testing.T) {
			got := render(lintFixture(t, name).Findings)
			gold := filepath.Join("testdata", filepath.FromSlash(name)+".golden.txt")
			if *update {
				if err := os.WriteFile(gold, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(gold)
			if err != nil {
				t.Fatal(err)
			}
			if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
				t.Errorf("findings differ from %s:\n%s", gold, got)
			}
		})
	}
}

// TestPlanRules pins every rule's positive and negative cases in the plan
// fixture, independently of the golden text.
func TestPlanRules(t *testing.T) {
	res := lintFixture(t, "plan.json")
	got := byAddr(res.Findings)
	want := map[string][]string{
		"aws_iam_policy.star":                  {"error IAM001", "error IAM002"},
		"aws_iam_policy.s3all":                 {"error IAM001"},
		"aws_iam_policy.write_any":             {"error IAM002"},
		"aws_iam_policy.read_any":              {"warn IAM002"},
		"aws_iam_policy.secret_any":            {"error IAM002"},
		"aws_iam_policy.notaction":             {"error IAM003"},
		"aws_iam_role_policy_attachment.admin": {"error IAM004"},
		"aws_iam_policy.passrole":              {"error IAM002", "error IAM005"},
		"aws_iam_policy.passrole_cond":         {"error IAM002"},
		"aws_iam_policy.attach_any":            {"error IAM005"},
		"aws_iam_role.open":                    {"error IAM006"},
		"aws_iam_role.foreign":                 {"error IAM006"},
		"aws_iam_role.lambda":                  {"error IAM001", "error IAM002", "error IAM004"},
		"aws_iam_role.gh_noaud":                {"error IAM006", "error IAM006"},
		// D-CM redacts values under keys containing "token", so the sub
		// value cannot be verified in a plan: it fails closed.
		"aws_iam_role.gh_wild":                     {"error IAM006"},
		"aws_s3_bucket_policy.public":              {"error IAM007"},
		"aws_iam_policy.noreason":                  {"error IAM002", "error SUP001"},
		"azurerm_role_assignment.owner_sub":        {"error AZR001"},
		"azurerm_role_assignment.uaa_mg":           {"error AZR001"},
		"azurerm_role_assignment.contrib_rg":       {"warn AZR001"},
		"azurerm_role_definition.god":              {"error AZR002", "error AZR003"},
		"azurerm_role_definition.contrib_copy":     {"error AZR002"},
		"azurerm_role_definition.granter":          {"error AZR003"},
		"azurerm_role_definition.wide_write":       {"error AZR002"},
		"azurerm_lighthouse_definition.msp":        {"error AZR001", "warn AZR004"},
		"google_project_iam_member.owner":          {"error GCP001"},
		"google_storage_bucket_iam_binding.public": {"error GCP001"},
	}
	if !reflect.DeepEqual(got, want) {
		for k, v := range want {
			if !reflect.DeepEqual(got[k], v) {
				t.Errorf("%s: got %v, want %v", k, got[k], v)
			}
		}
		for k, v := range got {
			if _, ok := want[k]; !ok {
				t.Errorf("unexpected findings on %s: %v", k, v)
			}
		}
	}
	if res.Suppressed["IAM002"] != 1 || res.Suppressed["AZR001"] != 1 || len(res.Suppressed) != 2 {
		t.Errorf("suppressed = %v, want IAM002:1 AZR001:1", res.Suppressed)
	}
	for _, f := range res.Findings {
		if f.File != "plan.json" || f.Line < 1 {
			t.Errorf("bad position %s", f)
		}
	}
}

func TestTemplateRules(t *testing.T) {
	got := byAddr(lintFixture(t, "template.yaml").Findings)
	want := map[string][]string{
		"AdminRole":        {"error IAM001", "error IAM002", "error IAM004", "error IAM006"},
		"SiteBucketPolicy": {"error IAM007"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestARMAndWhatIf(t *testing.T) {
	got := byAddr(lintFixture(t, "azuredeploy.json").Findings)
	owner, ops := "[guid(subscription().id, 'owner')]", "[guid(subscription().id, 'ops')]"
	msp := "[guid('acme-msp')]"
	want := map[string][]string{
		owner: {"error AZR001"},
		ops:   {"error AZR002", "error AZR003"},
		msp:   {"warn AZR004"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("arm: got %v, want %v", got, want)
	}
	wi := lintFixture(t, "whatif.json").Findings
	levels := []string{}
	for _, f := range wi {
		levels = append(levels, f.Level+" "+f.Rule)
	}
	sort.Strings(levels)
	if !reflect.DeepEqual(levels, []string{"error AZR001", "warn AZR001"}) {
		t.Errorf("whatif: %v", levels)
	}
}

func TestPulumi(t *testing.T) {
	got := byAddr(lintFixture(t, "preview.json").Findings)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	for addr, rules := range got {
		switch {
		case strings.HasSuffix(addr, "Policy::admin"):
			if !reflect.DeepEqual(rules, []string{"error IAM001", "error IAM002"}) {
				t.Errorf("%s: %v", addr, rules)
			}
		case strings.HasSuffix(addr, "RolePolicyAttachment::admin"):
			if !reflect.DeepEqual(rules, []string{"error IAM004"}) {
				t.Errorf("%s: %v", addr, rules)
			}
		default:
			t.Errorf("unexpected %s", addr)
		}
	}
}

func TestStandalone(t *testing.T) {
	fs := lintFixture(t, "policies/github-trust.json").Findings
	var lines []string
	for _, f := range fs {
		lines = append(lines, f.Level+" "+f.Rule+" "+strconv.Itoa(f.Line))
	}
	// Statement 0 (repo:acme/*) at line 4, statement 1 (no sub) at line 19;
	// statement 2 pins one repository.
	want := []string{"error IAM006 4", "error IAM006 19"}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("got %v, want %v\n%s", lines, want, render(fs))
	}
	if fs := lintFixture(t, "policies/safe.json").Findings; len(fs) != 0 {
		t.Errorf("safe.json: %s", render(fs))
	}
	raw, _ := os.ReadFile(filepath.Join("testdata", "policies", "not-a-policy.json"))
	if _, handled, _ := File("not-a-policy.json", raw, testOpts); handled {
		t.Error("a package.json-like file was handled as a policy")
	}
}

func TestNoAccountsSkipsCrossAccount(t *testing.T) {
	doc := `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::222222222222:root"},"Action":"sts:AssumeRole"}]}`
	// Without accounts the cross-account error is not judged; a warn says so.
	if res, _, _ := Standalone("t.json", []byte(doc), Options{}); len(res.Findings) != 1 || res.Findings[0].Level != packs.LevelWarn {
		t.Errorf("without accounts: %s", render(res.Findings))
	}
	if res, _, _ := Standalone("t.json", []byte(doc), testOpts); len(res.Findings) != 1 {
		t.Errorf("with accounts: %s", render(res.Findings))
	}
}

func TestTables(t *testing.T) {
	for a, want := range map[string]bool{"s3:Get*": true, "ec2:DescribeInstances": true, "s3:G*": false, "s3:*": false,
		"secretsmanager:GetSecretValue": false, "secretsmanager:Get*": false, "ssm:GetParameters": false, "s3:PutObject": false, "*": false} {
		if got := readOnly(a); got != want {
			t.Errorf("readOnly(%q) = %v", a, got)
		}
	}
	for _, c := range []struct {
		p, s string
		want bool
	}{{"iam:Attach*", "iam:AttachRolePolicy", true}, {"iam:*Policy", "iam:PutUserPolicy", true}, {"Microsoft.Authorization/*/Write", "Microsoft.Authorization/roleAssignments/write", true},
		{"iam:Get*", "iam:PassRole", false}, {"a?c", "abc", true}, {"a*b*c", "axxbyyc", true}, {"a*b*c", "axxbyy", false}} {
		if got := wildMatch(c.p, c.s); got != c.want {
			t.Errorf("wildMatch(%q, %q) = %v", c.p, c.s, got)
		}
	}
	for v, want := range map[string]bool{"repo:*": true, "repo:acme/*": true, "repo:acme/app*": true, "*": true, "repo:acme/app:*": false,
		"repo:acme/app:ref:refs/heads/main": false, "repo:acme": true} {
		if got := repoWildcard(v); got != want {
			t.Errorf("repoWildcard(%q) = %v", v, got)
		}
	}
	for s, want := range map[string]Level{"/": ScopeTenant, "/subscriptions/1": ScopeSubscription, "/subscriptions/1/resourceGroups/rg": ScopeResourceGroup,
		"/subscriptions/1/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/a": ScopeResource,
		"/providers/Microsoft.Management/managementGroups/acme":                            ScopeManagementGroup, "[subscription().id]": ScopeSubscription,
		"[resourceGroup().id]": ScopeResourceGroup, "[format('x')]": ScopeUnknown, "": ScopeUnknown} {
		if got := ScopeOf(s); got != want {
			t.Errorf("ScopeOf(%q) = %v, want %v", s, got, want)
		}
	}
	for s, want := range map[string]string{"Owner": "Owner", "owner": "Owner",
		"/subscriptions/1/providers/Microsoft.Authorization/roleDefinitions/8e3af657-a8ff-443c-a75c-2fe8c4bcb635":     "Owner",
		"[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'f58310d9-a9f6-439a-9e8d-f62e7b41a168')]": "Role Based Access Control Administrator",
		"Reader": ""} {
		r, _ := privilegedRole(s)
		if r.Name != want {
			t.Errorf("privilegedRole(%q) = %q", s, r.Name)
		}
	}
}

// TestFindingsCarryNoValues: a finding names rules, actions and addresses,
// never a principal, account, tenant or condition value.
func TestFindingsCarryNoValues(t *testing.T) {
	for _, name := range []string{"plan.json", "azuredeploy.json", "policies/github-trust.json", "cdk.out/AcmeStack.template.json", "unknown-plan.json", "nested.json"} {
		for _, f := range lintFixture(t, name).Findings {
			for _, v := range []string{"222222222222", "33333333-3333", "44444444-4444", "55555555-5555", "tester@", "acme-ext", "sts.amazonaws.com"} {
				if strings.Contains(f.Message, v) {
					t.Errorf("%s: finding carries %q: %s", name, v, f)
				}
			}
		}
	}
}

func TestAZR004OwnTenant(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "azuredeploy.json"))
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Tenants: []string{"44444444-4444-4444-4444-444444444444"}}
	res, _, err := File("azuredeploy.json", raw, o)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Findings {
		if f.Rule == "AZR004" {
			t.Errorf("own tenant flagged: %s", f)
		}
	}
	// Without tenants configured, AZR004 is not judged.
	res, _, _ = File("azuredeploy.json", raw, Options{})
	for _, f := range res.Findings {
		if f.Rule == "AZR004" {
			t.Errorf("AZR004 without tenants: %s", f)
		}
	}
}
