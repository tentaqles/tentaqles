package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPosString(t *testing.T) {
	cases := []struct {
		p    Pos
		want string
	}{
		{Pos{File: "main.json", Line: 12}, "main.json:12"},
		{Pos{File: "plan.json", Line: 1, Pointer: "/resource_changes/3"}, "plan.json:1#/resource_changes/3"},
		{Pos{File: "t.json", Line: 7, Pointer: "/resources/0"}, "t.json:7"},
		{Pos{File: "x.json", Pointer: "/a"}, "x.json#/a"},
	}
	for _, c := range cases {
		if got := c.p.String(); got != c.want {
			t.Errorf("%+v: got %q want %q", c.p, got, c.want)
		}
	}
}

func TestActions(t *testing.T) {
	r := Resource{Actions: []string{ActCreate, ActDelete}}
	if !r.Destroys() || !r.Planned() || !r.Changes() || r.Primary() != ActDelete {
		t.Fatalf("delete: %+v primary %s", r, r.Primary())
	}
	r = Resource{Actions: []string{ActReplace}}
	if r.Primary() != ActReplace || !r.Destroys() {
		t.Fatal("replace")
	}
	r = Resource{Actions: []string{ActNoOp}}
	if r.Changes() || r.Destroys() {
		t.Fatal("no-op changes")
	}
	r = Resource{Actions: []string{ActRead}}
	if r.Changes() {
		t.Fatal("read changes")
	}
	tmpl := Resource{}
	if tmpl.Planned() || !tmpl.Changes() || tmpl.Primary() != "" {
		t.Fatal("template resource")
	}
	del := Resource{Actions: []string{ActDelete}, Before: map[string]any{"a": "b"}}
	if del.Attrs()["a"] != "b" {
		t.Fatal("Attrs falls back to Before")
	}
}

func TestSummarize(t *testing.T) {
	rs := []Resource{
		{Addr: "aws_db_instance.main", Type: "aws_db_instance", Actions: []string{ActDelete}},
		{Addr: "aws_s3_bucket.logs", Type: "aws_s3_bucket", Actions: []string{ActReplace}},
		{Addr: "aws_security_group.web", Type: "aws_security_group", Actions: []string{ActDelete}},
		{Addr: "aws_instance.web", Type: "aws_instance", Actions: []string{ActCreate}},
		{Addr: "aws_iam_role.r", Type: "aws_iam_role", Actions: []string{ActNoOp}},
	}
	s := Summarize(rs)
	want := map[string]int{"delete": 2, "replace": 1, "create": 1, "no-op": 1}
	if s.Total != 5 || !reflect.DeepEqual(s.Counts, want) {
		t.Fatalf("counts %+v", s)
	}
	if len(s.StatefulDeletes) != 1 || s.StatefulDeletes[0].Addr != "aws_db_instance.main" {
		t.Fatalf("stateful deletes %+v", s.StatefulDeletes)
	}
	if len(s.StatefulReplaces) != 1 || s.StatefulReplaces[0].Type != "aws_s3_bucket" {
		t.Fatalf("stateful replaces %+v", s.StatefulReplaces)
	}
	if !s.HasStatefulDestroy() {
		t.Fatal("HasStatefulDestroy")
	}
	txt := s.Text()
	if !strings.Contains(txt, "5 resources: 1 create, 2 delete, 1 no-op, 1 replace") || !strings.Contains(txt, "stateful delete: aws_db_instance.main") {
		t.Fatalf("text %q", txt)
	}
	tmpl := Summarize([]Resource{{Type: "AWS::S3::Bucket"}})
	if tmpl.Counts["declared"] != 1 || tmpl.HasStatefulDestroy() {
		t.Fatalf("template summary %+v", tmpl)
	}
}

func TestSummarizeRedactsAddresses(t *testing.T) {
	s := Summarize([]Resource{{Addr: `aws_s3_bucket.b["ghp_` + strings.Repeat("a", 30) + `"]`, Type: "aws_s3_bucket", Actions: []string{ActDelete}}})
	if strings.Contains(s.StatefulDeletes[0].Addr, "ghp_aaaa") {
		t.Fatalf("address not redacted: %s", s.StatefulDeletes[0].Addr)
	}
}

func TestStateful(t *testing.T) {
	yes := []string{
		"aws_db_instance", "azurerm_storage_account", "google_storage_bucket", "AWS::DynamoDB::Table",
		"Microsoft.Sql/servers/databases", "microsoft.storage/STORAGEACCOUNTS", "Microsoft.KeyVault/vaults",
		"aws:rds/instance:Instance", "aws:s3/bucketV2:BucketV2", "azure-native:storage:StorageAccount",
		"gcp:sql/databaseInstance:DatabaseInstance", "azure-native:sql:Server", "AWS::KMS::Key",
	}
	no := []string{
		"", "aws_security_group", "aws_db_parameter_group", "Microsoft.Sql/servers/firewallRules",
		"AWS::IAM::Role", "aws:iam/policy:Policy", "azure-native:sql:ServerAzureADAdministrator",
		"azure-native:dbforpostgresql:FirewallRule", "gcp:spanner/instanceIAMMember:InstanceIAMMember",
		"azurerm_mssql_firewall_rule",
	}
	for _, s := range yes {
		if !Stateful(s) {
			t.Errorf("%s should be stateful", s)
		}
	}
	for _, s := range no {
		if Stateful(s) {
			t.Errorf("%s should not be stateful", s)
		}
	}
}

func TestTagsAndSuppressions(t *testing.T) {
	tf := Resource{After: map[string]any{"tags": map[string]any{"tq-allow": "IAM002: read-only role; iac001: decommissioned", "env": "prod"}}}
	if tf.Tags()["env"] != "prod" {
		t.Fatalf("tags %v", tf.Tags())
	}
	got := tf.Suppressions()
	want := []Suppression{{Rule: "IAC001", Reason: "decommissioned"}, {Rule: "IAM002", Reason: "read-only role"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suppressions %+v", got)
	}
	if s, ok := tf.Allowed("IAM002"); !ok || !s.Valid() {
		t.Fatal("Allowed IAM002")
	}
	cfn := Resource{After: map[string]any{"Tags": []any{
		map[string]any{"Key": "TQ-Allow", "Value": "IAC003"},
		map[string]any{"Key": "team", "Value": "acme"},
	}}}
	sup := cfn.Suppressions()
	if len(sup) != 1 || sup[0].Rule != "IAC003" || sup[0].Valid() {
		t.Fatalf("no-reason suppression %+v", sup)
	}
	bad := ParseSuppressions("whatever; IAM001: ok")
	if len(bad) != 2 || bad[0].Rule != "" || bad[0].Raw != "whatever" || !bad[1].Valid() {
		t.Fatalf("bad entry %+v", bad)
	}
	gcp := Resource{After: map[string]any{"labels": map[string]any{"tq_allow": "GCP001: shared"}}}
	if _, ok := gcp.Allowed("GCP001"); !ok {
		t.Fatal("tq_allow label")
	}
	if (Resource{}).Tags() != nil || (Resource{}).Suppressions() != nil {
		t.Fatal("empty resource")
	}
}

func TestValueHelpers(t *testing.T) {
	v := map[string]any{"a": []any{map[string]any{"port": json.Number("22")}, "x"}, "s": "443", "b": "TRUE", "f": 3.0}
	if p, ok := Int(Get(v, "a", "0", "port")); !ok || p != 22 {
		t.Fatal("json.Number int")
	}
	if p, ok := Int(v["s"]); !ok || p != 443 {
		t.Fatal("string int")
	}
	if p, ok := Int(v["f"]); !ok || p != 3 {
		t.Fatal("float int")
	}
	if b, ok := Bool(v["b"]); !ok || !b {
		t.Fatal("string bool")
	}
	if _, ok := Bool("yes"); ok {
		t.Fatal("yes is not a bool")
	}
	if Get(v, "a", "5") != nil || Get(v, "nope", "x") != nil || Get(v, "a", "x") != nil {
		t.Fatal("missing path")
	}
	if s, _ := Str(json.Number("5432")); s != "5432" {
		t.Fatal("Str number")
	}
	if !reflect.DeepEqual(Strings("s3:*"), []string{"s3:*"}) || !reflect.DeepEqual(Strings([]any{"a", 1, map[string]any{}}), []string{"a", "1"}) {
		t.Fatal("Strings")
	}
	if List(nil) != nil || Map("x") != nil {
		t.Fatal("List/Map")
	}
	if !IsRedacted(RedactedKey) || IsRedacted("x") {
		t.Fatal("IsRedacted")
	}
	rs := SetEnv([]Resource{{}, {}}, "prod")
	if rs[1].Env != "prod" {
		t.Fatal("SetEnv")
	}
	if len(Kinds()) != 6 {
		t.Fatal("Kinds")
	}
}
