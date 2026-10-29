package policy

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"gopkg.in/yaml.v3"
)

// TestDefaultOffWhenAbsent: ARM templates, what-if payloads and CFN
// templates carry only what the author wrote, so a missing default-off
// setting (purge protection, storage encryption, deletion protection) is
// off. Terraform plans fill in the defaults, so absence there says nothing;
// an unresolved intrinsic or ARM expression is skipped (review r0 #2).
func TestDefaultOffWhenAbsent(t *testing.T) {
	type tc struct {
		name string
		r    model.Resource
		env  string
		want string // rule:level list, "" for none
	}
	cfn := func(typ string, props map[string]any, meta map[string]string) model.Resource {
		return model.Resource{Addr: "R", Type: typ, Source: model.KindCFN, After: props, Meta: meta}
	}
	snap := map[string]string{"deletion_policy": "Snapshot"}
	cases := []tc{
		{"whatif vault no purge prod", model.Resource{Addr: "kv", Type: "Microsoft.KeyVault/vaults", Source: model.KindWhatIf, Actions: []string{model.ActCreate},
			After: map[string]any{"properties": map[string]any{"enableSoftDelete": true}}}, "prod", "IAC006:error"},
		{"arm vault no purge", model.Resource{Addr: "kv", Type: "Microsoft.KeyVault/vaults", Source: model.KindARM,
			After: map[string]any{"properties": map[string]any{}}}, "", "IAC006:warn"},
		{"arm vault no properties", model.Resource{Addr: "kv", Type: "Microsoft.KeyVault/vaults", Source: model.KindARM,
			After: map[string]any{}}, "", "IAC006:warn"},
		{"arm vault expression", model.Resource{Addr: "kv", Type: "Microsoft.KeyVault/vaults", Source: model.KindARM,
			After: map[string]any{"properties": map[string]any{"enablePurgeProtection": "[parameters('purge')]"}}}, "", ""},
		{"arm vault protected", model.Resource{Addr: "kv", Type: "Microsoft.KeyVault/vaults", Source: model.KindARM,
			After: map[string]any{"properties": map[string]any{"enablePurgeProtection": true}}}, "", ""},
		{"tfplan vault absent", model.Resource{Addr: "kv", Type: "azurerm_key_vault", Source: model.KindTFPlan, Actions: []string{model.ActCreate},
			After: map[string]any{"name": "acme"}}, "prod", ""},
		{"whatif delete has no after", model.Resource{Addr: "kv", Type: "Microsoft.KeyVault/vaults", Source: model.KindWhatIf, Actions: []string{model.ActDelete}},
			"dev", "IAC001:warn"},
		{"cfn rds no StorageEncrypted", cfn("AWS::RDS::DBInstance", map[string]any{"DeletionProtection": true}, snap), "", "IAC004:error"},
		{"cfn rds no properties", cfn("AWS::RDS::DBInstance", nil, snap), "", "IAC004:error,IAC006:warn"},
		{"cfn rds intrinsic", cfn("AWS::RDS::DBInstance", map[string]any{"StorageEncrypted": map[string]any{"Ref": "Enc"}, "DeletionProtection": true}, snap), "", ""},
		{"cfn rds kms key", cfn("AWS::RDS::DBInstance", map[string]any{"KmsKeyId": "k", "DeletionProtection": true}, snap), "", ""},
		{"cfn rds in cluster", cfn("AWS::RDS::DBInstance", map[string]any{"DBClusterIdentifier": map[string]any{"Ref": "C"}}, nil), "", ""},
		{"cfn rds explicit false in cluster", cfn("AWS::RDS::DBInstance", map[string]any{"DBClusterIdentifier": "c", "StorageEncrypted": false}, nil), "", "IAC004:error"},
		{"cfn rds no DeletionProtection", cfn("AWS::RDS::DBInstance", map[string]any{"StorageEncrypted": true}, snap), "", "IAC006:warn"},
		{"cfn cluster no StorageEncrypted", cfn("AWS::RDS::DBCluster", map[string]any{"DeletionProtection": true}, nil), "", "IAC004:error"},
		{"cfn cluster from snapshot", cfn("AWS::RDS::DBCluster", map[string]any{"SnapshotIdentifier": "s", "DeletionProtection": true}, nil), "", ""},
		{"cfn volume no Encrypted", cfn("AWS::EC2::Volume", map[string]any{"Size": 10}, snap), "", "IAC004:error"},
		{"cfn volume from snapshot", cfn("AWS::EC2::Volume", map[string]any{"SnapshotId": "snap"}, snap), "", ""},
		{"cfn dynamodb no DeletionProtectionEnabled", cfn("AWS::DynamoDB::Table", map[string]any{"TableName": "acme"}, map[string]string{"deletion_policy": "Retain"}), "", "IAC006:warn"},
		{"cfn dynamodb protected", cfn("AWS::DynamoDB::Table", map[string]any{"DeletionProtectionEnabled": true}, map[string]string{"deletion_policy": "RetainExceptOnCreate"}), "", ""},
	}
	for _, c := range cases {
		d := Doc{Name: "x", Resources: []model.Resource{c.r}, Env: c.env}
		if c.env != "" {
			d.Policy = "approve"
			if c.env != "prod" {
				d.Policy = "auto"
			}
			d.Plan = &Plan{ID: "p", Env: c.env, Policy: d.Policy}
		}
		var got []string
		for _, f := range Evaluate([]Doc{d}).Findings {
			got = append(got, f.Rule+":"+f.Level)
		}
		if g := strings.Join(got, ","); g != c.want {
			t.Errorf("%s: got %q, want %q", c.name, g, c.want)
		}
	}
}

// TestCFNRDSDefaultSnapshot: the CloudFormation default DeletionPolicy is
// Snapshot for AWS::RDS::DBCluster and for a DBInstance without
// DBClusterIdentifier, so neither raises the DeletionPolicy message
// (review r0 #8).
func TestCFNRDSDefaultSnapshot(t *testing.T) {
	for _, typ := range []string{"AWS::RDS::DBCluster", "AWS::RDS::DBInstance"} {
		r := model.Resource{Addr: "R", Type: typ, Source: model.KindCFN,
			After: map[string]any{"StorageEncrypted": true, "DeletionProtection": true}}
		if fs := Evaluate([]Doc{{Name: "x", Resources: []model.Resource{r}}}).Findings; len(fs) != 0 {
			t.Errorf("%s: %v", typ, fs)
		}
	}
	r := model.Resource{Addr: "R", Type: "AWS::DynamoDB::Table", Source: model.KindCFN,
		After: map[string]any{"DeletionProtectionEnabled": true}}
	fs := Evaluate([]Doc{{Name: "x", Resources: []model.Resource{r}}}).Findings
	if len(fs) != 1 || !strings.Contains(fs[0].Message, "Delete (the default)") {
		t.Errorf("dynamodb: %v", fs)
	}
	// An explicit Delete on a database is still reported, and both problems
	// of one resource land in one IAC006 finding.
	r = model.Resource{Addr: "R", Type: "AWS::RDS::DBInstance", Source: model.KindCFN,
		After: map[string]any{"StorageEncrypted": true}, Meta: map[string]string{"deletion_policy": "Delete"}}
	fs = Evaluate([]Doc{{Name: "x", Resources: []model.Resource{r}}}).Findings
	if len(fs) != 1 || fs[0].Rule != IAC006 || !strings.Contains(fs[0].Message, "deletion protection is off") || !strings.Contains(fs[0].Message, "DeletionPolicy Delete") {
		t.Errorf("rds delete: %v", fs)
	}
}

// TestConftestPolicyScalar: a single string for conftest.policy is
// rejected by the validator instead of being accepted and then silently
// dropped by Load (review r0 #5); the list form loads.
func TestConftestPolicyScalar(t *testing.T) {
	node := func(src string) yaml.Node {
		var n yaml.Node
		if err := yaml.Unmarshal([]byte(src), &n); err != nil {
			t.Fatal(err)
		}
		return *n.Content[0]
	}
	scalar := node("conftest:\n  policy: policy/\n")
	if err := ValidateConftest(*scalar.Content[1]); err == nil || !strings.Contains(err.Error(), "list") {
		t.Errorf("scalar policy accepted: %v", err)
	}
	list := node("conftest:\n  policy: [policy/]\n")
	if err := ValidateConftest(*list.Content[1]); err != nil {
		t.Fatalf("list: %v", err)
	}
	c := Load(packs.Settings{Raw: list})
	if len(c.Conftest.Policy) != 1 || c.Conftest.Policy[0] != "policy/" {
		t.Errorf("policy = %v, want [policy/]", c.Conftest.Policy)
	}
}
