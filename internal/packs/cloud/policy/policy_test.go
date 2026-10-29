package policy

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/readers"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func lines(fs []packs.Finding) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.String())
	}
	return strings.Join(out, "\n") + "\n"
}

func readDoc(t *testing.T, name, env, pol string) Doc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	k, rs, err := readers.ReadAuto(name, raw, readers.Options{Env: env})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	d := Doc{Name: name, Kind: k, Raw: raw, Resources: rs, Env: env, Policy: pol}
	if env != "" {
		d.Plan = &Plan{ID: "p1", Env: env, Policy: pol, Kind: k, Name: name}
	}
	return d
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Errorf("%s:\n got:\n%s\nwant:\n%s", name, got, want)
	}
}

// TestGoldenRules runs every built-in rule over the golden fixtures, as a
// prod plan, a dev plan and IaC source.
func TestGoldenRules(t *testing.T) {
	cases := []struct {
		golden, file, env, policy string
	}{
		{"plan-prod", "plan.json", "prod", "approve"},
		{"plan-dev", "plan.json", "dev", "auto"},
		{"template-yaml", "template.yaml", "", ""},
		{"template-arm", "template.json", "", ""},
	}
	for _, c := range cases {
		o := Evaluate([]Doc{readDoc(t, c.file, c.env, c.policy)})
		golden(t, c.golden, lines(o.Findings))
	}
}

func rulesOf(fs []packs.Finding) map[string]string {
	m := map[string]string{}
	for _, f := range fs {
		m[f.Rule] = f.Level
	}
	return m
}

// TestRuleLevels pins the level of each rule per environment (spec D §7.2)
// and the negatives.
func TestRuleLevels(t *testing.T) {
	prod := Evaluate([]Doc{readDoc(t, "plan.json", "prod", "approve")})
	dev := Evaluate([]Doc{readDoc(t, "plan.json", "dev", "auto")})
	p, d := rulesOf(prod.Findings), rulesOf(dev.Findings)
	want := map[string][2]string{ // rule: prod, dev
		IAC001: {"error", "warn"},
		IAC002: {"error", "error"},
		IAC003: {"error", "error"},
		IAC004: {"error", "error"},
		IAC005: {"error", "warn"},
		IAC006: {"error", ""},
		SUP001: {"error", "error"},
	}
	for rule, lv := range want {
		if p[rule] != lv[0] || d[rule] != lv[1] {
			t.Errorf("%s: prod %q dev %q, want %q %q", rule, p[rule], d[rule], lv[0], lv[1])
		}
	}
	// Negatives: an internal-only group, a 0.0.0.0-only firewall rule (Azure
	// services), a no-op resource and a suppressed delete raise nothing.
	for _, f := range prod.Findings {
		for _, neg := range []string{"aws_security_group.internal", "azurerm_mssql_firewall_rule.azure", "aws_db_instance.ok", "aws_s3_bucket.kept"} {
			if strings.HasPrefix(f.Message, neg+":") {
				t.Errorf("negative %s raised %s", neg, f)
			}
		}
	}
	if prod.Suppressed[IAC001] != 1 {
		t.Errorf("suppressed = %v, want IAC001 once", prod.Suppressed)
	}
	// A suppression without a reason does not suppress.
	found := false
	for _, f := range prod.Findings {
		if f.Rule == IAC002 && strings.HasPrefix(f.Message, "aws_security_group.noreason:") {
			found = true
		}
	}
	if !found {
		t.Error("a reasonless tq-allow suppressed IAC002")
	}
}

func TestEveryRuleHasPositiveAndNegative(t *testing.T) {
	var all []packs.Finding
	for _, c := range [][3]string{{"plan.json", "prod", "approve"}, {"template.yaml", "", ""}, {"template.json", "", ""}} {
		all = append(all, Evaluate([]Doc{readDoc(t, c[0], c[1], c[2])}).Findings...)
	}
	got := rulesOf(all)
	for _, r := range Rules {
		if got[r] == "" {
			t.Errorf("%s has no positive fixture", r)
		}
	}
	// Negatives built in place: each rule's safe twin raises nothing.
	safe := []model.Resource{
		{Addr: "aws_db_instance.a", Type: "aws_db_instance", Actions: []string{model.ActUpdate}, Source: model.KindTFPlan,
			After: map[string]any{"storage_encrypted": true, "publicly_accessible": false, "deletion_protection": true}},
		{Addr: "aws_security_group.a", Type: "aws_security_group", Actions: []string{model.ActCreate}, Source: model.KindTFPlan,
			After: map[string]any{"ingress": []any{map[string]any{"from_port": 22, "to_port": 22, "protocol": "tcp", "cidr_blocks": []any{"10.0.0.0/16"}}}}},
		{Addr: "aws_s3_bucket_public_access_block.a", Type: "aws_s3_bucket_public_access_block", Actions: []string{model.ActCreate}, Source: model.KindTFPlan,
			After: map[string]any{"block_public_acls": true, "block_public_policy": true, "ignore_public_acls": true, "restrict_public_buckets": true}},
		{Addr: "aws_s3_bucket.a", Type: "aws_s3_bucket", Actions: []string{model.ActUpdate}, Source: model.KindTFPlan, After: map[string]any{"bucket": "acme"}},
		{Addr: "acme", Type: "Microsoft.Resources/deployments", Source: model.KindARM, Meta: map[string]string{"deployment_mode": "Incremental"},
			After: map[string]any{"properties": map[string]any{"mode": "Incremental"}}},
		{Addr: "azurerm_key_vault.a", Type: "azurerm_key_vault", Actions: []string{model.ActCreate}, Source: model.KindTFPlan,
			After: map[string]any{"purge_protection_enabled": true}},
		{Addr: "azurerm_mssql_server.a", Type: "azurerm_mssql_server", Actions: []string{model.ActCreate}, Source: model.KindTFPlan,
			After: map[string]any{"public_network_access_enabled": false}},
	}
	o := Evaluate([]Doc{{Name: "safe", Resources: safe, Env: "prod", Policy: "approve", Plan: &Plan{ID: "p"}}})
	if len(o.Findings) != 0 {
		t.Errorf("safe twins raised %v", o.Findings)
	}
}

func TestPublicNetworkAccessOnlyInProd(t *testing.T) {
	r := model.Resource{Addr: "azurerm_mssql_server.a", Type: "azurerm_mssql_server", Actions: []string{model.ActCreate}, Source: model.KindTFPlan,
		After: map[string]any{"public_network_access_enabled": true}}
	if fs := Evaluate([]Doc{{Name: "x", Resources: []model.Resource{r}, Env: "dev", Policy: "ask", Plan: &Plan{}}}).Findings; len(fs) != 0 {
		t.Errorf("dev: %v", fs)
	}
	fs := Evaluate([]Doc{{Name: "x", Resources: []model.Resource{r}, Env: "prod", Policy: "approve", Plan: &Plan{}}}).Findings
	if len(fs) != 1 || fs[0].Rule != IAC005 || fs[0].Level != packs.LevelError {
		t.Errorf("prod: %v", fs)
	}
}

func TestUnknownActionOnStatefulFailsClosed(t *testing.T) {
	r := model.Resource{Addr: "x", Type: "Microsoft.Sql/servers/databases", Actions: []string{model.ActUnknown}, Source: model.KindWhatIf}
	fs := Evaluate([]Doc{{Name: "w", Resources: []model.Resource{r}, Env: "prod", Policy: "approve", Plan: &Plan{}}}).Findings
	if len(fs) != 1 || fs[0].Rule != IAC001 || fs[0].Level != packs.LevelError || !strings.Contains(fs[0].Message, "may be deleted") {
		t.Errorf("unknown action: %v", fs)
	}
}

func TestPolicyFor(t *testing.T) {
	var s packs.Settings
	for _, c := range [][3]string{{"prod", "", "approve"}, {"staging", "", "ask"}, {"dev", "", "ask"}, {"dev", "AUTO", "auto"}, {"", "", ""}} {
		if got := PolicyFor(s, c[0], c[1]); got != c[2] {
			t.Errorf("PolicyFor(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func TestPlanFileKind(t *testing.T) {
	bad := []string{"prod.tfplan", "infra/terraform.tfstate", "infra/terraform.tfstate.backup", "infra/.terraform/providers/x",
		"infra/.terraform/environment", "backups/main.dump", "db/acme.bacpac", "x/tq_drill_main_1.sql"}
	ok := []string{"infra/.terraform.lock.hcl", "infra/main.tf", "docs/tfstate.md", "dump.go"}
	for _, p := range bad {
		if PlanFileKind(p) == "" {
			t.Errorf("%s not flagged", p)
		}
	}
	for _, p := range ok {
		if k := PlanFileKind(p); k != "" {
			t.Errorf("%s flagged: %s", p, k)
		}
	}
}

func TestBundle(t *testing.T) {
	files := BundleFiles()
	var names []string
	for n := range files {
		names = append(names, n)
		if strings.HasSuffix(n, "_test.rego") {
			t.Errorf("test file %s in the bundle", n)
		}
		if !strings.Contains(string(files[n]), "import rego.v1") {
			t.Errorf("%s is not Rego v1", n)
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "arm.rego,cfn.rego,lib.rego,terraform.rego" {
		t.Errorf("bundle = %v", names)
	}
	// Every namespace has an opa test file, and every rule id a test.
	for _, ns := range []string{"terraform", "arm", "cfn"} {
		b, err := bundleFS.ReadFile("rego/" + ns + "_test.rego")
		if err != nil {
			t.Fatalf("no opa test for %s", ns)
		}
		src := string(files[ns+".rego"])
		for i := 1; i <= 6; i++ {
			id := "TQR00" + string(rune('0'+i))
			if strings.Contains(src, `"`+id+`"`) && !strings.Contains(string(b), `"`+id+`"`) {
				t.Errorf("%s: %s has no opa test", ns, id)
			}
		}
	}
	dir := t.TempDir()
	got, err := ExtractBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != BundleHash() {
		t.Errorf("extracted to %s", got)
	}
	again, err := ExtractBundle(dir)
	if err != nil || again != got {
		t.Fatalf("second extract: %s %v", again, err)
	}
	ents, _ := os.ReadDir(got)
	if len(ents) != 4 {
		t.Errorf("extracted %d files", len(ents))
	}
	if _, err := ExtractBundle(""); err == nil {
		t.Error("extract without a state dir")
	}
}

func TestConftestInput(t *testing.T) {
	for _, c := range []struct{ file, ns, want string }{
		{"plan.json", NamespaceTerraform, `"address":"aws_security_group.web"`},
		{"template.yaml", NamespaceCFN, `"AdminSg":{"Properties"`},
		{"template.json", NamespaceARM, `"tq_addr":"acmesa"`},
	} {
		d := readDoc(t, c.file, "", "")
		ns, body, err := ConftestInput(d)
		if err != nil || ns != c.ns || !strings.Contains(string(body), c.want) {
			t.Errorf("%s: %s %v %s", c.file, ns, err, body)
		}
	}
	ns, body, err := ConftestInput(Doc{Kind: model.KindPulumi})
	if ns != "" || body != nil || err != nil {
		t.Errorf("pulumi: %q %s %v", ns, body, err)
	}
}
