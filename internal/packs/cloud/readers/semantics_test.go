package readers

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

func load(t *testing.T, k model.Kind, base string) map[string]model.Resource {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtures, string(k), base))
	if err != nil {
		t.Fatal(err)
	}
	rs, err := Read(k, base, raw, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]model.Resource{}
	for _, r := range rs {
		key := r.Addr
		if d := r.Meta["deposed"]; d != "" {
			key += " deposed"
		}
		if _, dup := m[key]; dup {
			t.Fatalf("duplicate %s", key)
		}
		m[key] = r
	}
	return m
}

func acts(r model.Resource) string { return strings.Join(r.Actions, ",") }

func TestTFPlanSemantics(t *testing.T) {
	m := load(t, model.KindTFPlan, "changes.json")
	db := m["aws_db_instance.main"]
	if acts(db) != "replace" || db.Meta["raw_action"] != "delete,create" || db.Meta["action_reason"] != "replace_because_cannot_update" {
		t.Fatalf("replace: %+v", db)
	}
	if db.After["password"] != model.RedactedSensitive || db.Before["password"] != model.RedactedSensitive {
		t.Fatal("sensitive password not masked")
	}
	if p, _ := model.Int(db.After["port"]); p != 5432 {
		t.Fatal("port lost")
	}
	if _, ok := db.Allowed("IAC001"); !ok {
		t.Fatal("tq-allow tag lost in the plan")
	}
	if db.Meta["provider"] != "registry.terraform.io/hashicorp/aws" || db.Meta["mode"] != "managed" {
		t.Fatalf("meta %v", db.Meta)
	}
	if db.Pos.Pointer != "/resource_changes/0" {
		t.Fatalf("pointer %q", db.Pos.Pointer)
	}
	old := m["aws_s3_bucket.old"]
	if acts(old) != "delete" || old.After != nil || old.Before["bucket"] != "acme-old" || !old.Destroys() {
		t.Fatalf("delete: %+v", old)
	}
	sg := m["module.net.aws_security_group.web"]
	if sg.Meta["module"] != "module.net" || acts(sg) != "update" {
		t.Fatalf("module: %+v", sg)
	}
	if sg.After["api_token"] != model.RedactedKey {
		t.Fatalf("secret-looking key not scrubbed: %v", sg.After["api_token"])
	}
	cidr := model.Strings(model.Get(sg.After, "ingress", "0", "cidr_blocks"))
	if !reflect.DeepEqual(cidr, []string{"0.0.0.0/0"}) {
		t.Fatalf("cidr %v", cidr)
	}
	role := m["aws_iam_role.app"]
	if acts(role) != "no-op" || role.Changes() || !strings.Contains(role.After["assume_role_policy"].(string), "sts:AssumeRole") {
		t.Fatalf("no-op role: %+v", role)
	}
	dep := m["aws_instance.legacy deposed"]
	if dep.Meta["deposed"] != "00000000" || acts(dep) != "delete" {
		t.Fatalf("deposed: %+v", dep)
	}
	whole := m["aws_kms_key.whole"]
	if whole.After != nil || whole.Meta["after_redacted"] != "true" {
		t.Fatalf("whole-object sensitive: %+v", whole)
	}
	data := m["data.aws_iam_policy_document.read"]
	if data.Meta["mode"] != "data" || acts(data) != "read" {
		t.Fatalf("data source: %+v", data)
	}
	adopted := m["aws_s3_bucket.adopted"]
	if acts(adopted) != "no-op,import" {
		t.Fatalf("import: %q", acts(adopted))
	}
	if acts(m["aws_s3_bucket.forgotten"]) != "forget" || m["aws_s3_bucket.forgotten"].Destroys() {
		t.Fatal("forget")
	}
	s := model.Summarize(mapValues(m))
	if len(s.StatefulReplaces) != 1 || len(s.StatefulDeletes) != 1 || s.StatefulDeletes[0].Addr != "aws_s3_bucket.old" {
		t.Fatalf("summary %+v", s)
	}
}

func TestTFPlanRealOutput(t *testing.T) {
	m := load(t, model.KindTFPlan, "builtin-create.json")
	r := m["terraform_data.acme"]
	if acts(r) != "create" || model.Get(r.After, "input", "password") != model.RedactedSensitive || model.Get(r.After, "input", "name") != "acme" {
		t.Fatalf("%+v", r)
	}
	if r.Pos.Line != 1 || r.Pos.Pointer != "/resource_changes/0" || r.Pos.String() != "builtin-create.json:1#/resource_changes/0" {
		t.Fatalf("pos %+v", r.Pos)
	}
	if r.Tags() != nil {
		// terraform_data has no tags attribute; input.tags is not the resource's tags.
		t.Fatalf("tags %v", r.Tags())
	}
}

func mapValues(m map[string]model.Resource) []model.Resource {
	var out []model.Resource
	for _, r := range m {
		out = append(out, r)
	}
	return out
}

func TestARMSemantics(t *testing.T) {
	m := load(t, model.KindARM, "template.json")
	sa := m["[format('{0}sa', parameters('prefix'))]"]
	if sa.Type != "Microsoft.Storage/storageAccounts" || sa.Actions != nil {
		t.Fatalf("storage: %+v", sa)
	}
	if b, _ := model.Bool(model.Get(sa.After, "properties", "allowBlobPublicAccess")); !b {
		t.Fatal("allowBlobPublicAccess lost")
	}
	if _, ok := sa.After["resources"]; ok {
		t.Fatal("child resources left inside the parent")
	}
	if _, ok := sa.Allowed("IAC003"); !ok {
		t.Fatal("tq-allow tag")
	}
	child := m["[format('{0}sa', parameters('prefix'))]/default/assets"]
	if child.Type != "Microsoft.Storage/storageAccounts/blobServices/containers" || child.Meta["parent"] != sa.Addr {
		t.Fatalf("child: %+v", child)
	}
	if child.Pos.Pointer != "/resources/0/resources/0" {
		t.Fatalf("child pointer %q", child.Pos.Pointer)
	}
	sql := m["acme-sql"]
	if model.Get(sql.After, "properties", "administratorLoginPassword") != "[parameters('adminPassword')]" {
		t.Fatal("a parameter reference is not a secret")
	}
	rules := m["[format('{0}/{1}', 'acme-sql', format('rule{0}', copyIndex()))]"]
	if rules.Meta["copy"] != "rules" {
		t.Fatalf("copy: %+v", rules.Meta)
	}
	rbac := m["rbac"]
	if rbac.Meta["deployment_mode"] != "Complete" || model.Get(rbac.After, "properties", "template", "resources") != nil {
		t.Fatalf("deployment: %+v", rbac)
	}
	ra := m["00000000-0000-0000-0000-000000000001"]
	if ra.Type != "Microsoft.Authorization/roleAssignments" || ra.Meta["parent"] != "rbac" || ra.Pos.Pointer != "/resources/3/properties/template/resources/0" {
		t.Fatalf("nested: %+v", ra)
	}
	kv := m["acme-kv"]
	if kv.Meta["parent"] != "inner" || kv.Type != "Microsoft.KeyVault/vaults" {
		t.Fatalf("doubly nested: %+v", kv)
	}
	if m["inner"].Meta["parent"] != "rbac" || m["inner"].Meta["deployment_mode"] != "Incremental" {
		t.Fatalf("inner: %+v", m["inner"])
	}
	if _, ok := m["linked"]; !ok {
		t.Fatal("linked deployment missing")
	}
	sym := load(t, model.KindARM, "symbolic.json")
	if _, ok := sym["acme-rg"]; ok {
		t.Fatal("existing resource read as declared")
	}
	if sym["acme-kv"].Meta["symbol"] != "vault" || sym["acme-kv"].Pos.Pointer != "/resources/vault" {
		t.Fatalf("symbolic: %+v", sym["acme-kv"])
	}
	if len(sym) != 2 {
		t.Fatalf("symbolic count %d", len(sym))
	}
}

func TestWhatIfSemantics(t *testing.T) {
	m := load(t, model.KindWhatIf, "cli.json")
	id := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/acme-rg/providers/"
	sa := m[id+"Microsoft.Storage/storageAccounts/acmesa"]
	if acts(sa) != "update" || sa.Type != "Microsoft.Storage/storageAccounts" || sa.Meta["raw_action"] != "Modify" {
		t.Fatalf("modify: %+v", sa)
	}
	if model.Get(sa.After, "properties", "primaryAccessKey") != model.RedactedKey {
		t.Fatal("access key not scrubbed")
	}
	if b, _ := model.Bool(model.Get(sa.Before, "properties", "allowBlobPublicAccess")); b {
		t.Fatal("before")
	}
	db := m[id+"Microsoft.Sql/servers/acme-sql/databases/acme-db"]
	if acts(db) != "delete" || db.Type != "Microsoft.Sql/servers/databases" || db.After != nil {
		t.Fatalf("delete: %+v", db)
	}
	if acts(m[id+"Microsoft.Authorization/roleAssignments/00000000-0000-0000-0000-000000000001"]) != "create" {
		t.Fatal("create")
	}
	ign := m[id+"Microsoft.Network/virtualNetworks/acme-vnet"]
	if acts(ign) != "no-op" || ign.Type != "Microsoft.Network/virtualNetworks" {
		t.Fatalf("ignore: %+v", ign)
	}
	if acts(m[id+"Microsoft.KeyVault/vaults/acme-kv"]) != "no-op" {
		t.Fatal("nochange")
	}
	un := m[id+"Microsoft.Web/sites/acme-app"]
	if acts(un) != "unknown" || un.Meta["unsupported_reason"] == "" {
		t.Fatalf("unsupported: %+v", un)
	}
	if acts(m[id+"Microsoft.Network/dnsZones/acme.example"]) != "update" {
		t.Fatal("deploy")
	}
	disk := m[id+"Microsoft.Compute/disks/acme-disk"]
	if disk.Meta["potential"] != "true" || acts(disk) != "delete" || disk.Pos.Pointer != "/potentialChanges/0" {
		t.Fatalf("potential: %+v", disk)
	}
	rest := load(t, model.KindWhatIf, "rest.json")
	for _, r := range rest {
		if r.Pos.Pointer != "/properties/changes/0" || r.Type != "Microsoft.ManagedIdentity/userAssignedIdentities" {
			t.Fatalf("rest: %+v", r)
		}
	}
}

func TestARMTypeFromID(t *testing.T) {
	cases := map[string]string{
		"/subscriptions/0/resourceGroups/rg/providers/Microsoft.Sql/servers/s/databases/d": "Microsoft.Sql/servers/databases",
		"/subscriptions/0/providers/Microsoft.Authorization/roleAssignments/x":             "Microsoft.Authorization/roleAssignments",
		"/subscriptions/0/resourceGroups/rg":                                               "Microsoft.Resources/resourceGroups",
		"/subscriptions/0":                                                                 "Microsoft.Resources/subscriptions",
		"/subscriptions/0/resourceGroups/rg/providers/Microsoft.Network/vnets/v/providers/Microsoft.Authorization/locks/l": "Microsoft.Authorization/locks",
		"/providers/Microsoft.Management/managementGroups/mg":                                                              "Microsoft.Management/managementGroups",
		"garbage": "",
	}
	for id, want := range cases {
		if got := armTypeFromID(id); got != want {
			t.Errorf("%s: got %q want %q", id, got, want)
		}
	}
}

func TestCFNSemantics(t *testing.T) {
	m := load(t, model.KindCFN, "template.yaml")
	b := m["Bucket"]
	if b.Type != "AWS::S3::Bucket" || b.Meta["deletion_policy"] != "Delete" || b.Meta["update_replace_policy"] != "Retain" || b.Actions != nil {
		t.Fatalf("bucket: %+v", b)
	}
	if !reflect.DeepEqual(b.After["BucketName"], map[string]any{"Fn::Sub": "${AWS::StackName}-assets"}) {
		t.Fatalf("!Sub: %#v", b.After["BucketName"])
	}
	if v, ok := model.Bool(model.Get(b.After, "PublicAccessBlockConfiguration", "BlockPublicPolicy")); !ok || v {
		t.Fatal("string bool")
	}
	if s, ok := b.Allowed("IAC003"); !ok || s.Reason != "public assets bucket" {
		t.Fatal("tq-allow list tag")
	}
	db := m["Db"]
	if db.Meta["condition"] != "IsProd" || !reflect.DeepEqual(db.After["MasterUserPassword"], map[string]any{"Ref": "DbPassword"}) {
		t.Fatalf("db: %#v", db.After["MasterUserPassword"])
	}
	if p, _ := model.Int(db.After["Port"]); p != 5432 {
		t.Fatal("yaml int")
	}
	if !reflect.DeepEqual(model.Get(db.After, "VPCSecurityGroups", "0"), map[string]any{"Fn::GetAtt": []any{"Sg", "GroupId"}}) {
		t.Fatalf("!GetAtt short: %#v", model.Get(db.After, "VPCSecurityGroups", "0"))
	}
	sg := m["Sg"]
	if !reflect.DeepEqual(sg.After["GroupDescription"], map[string]any{"Fn::Join": []any{"", []any{"web ", map[string]any{"Ref": "AWS::Region"}}}}) {
		t.Fatalf("!Join: %#v", sg.After["GroupDescription"])
	}
	role := m["Role"]
	res := model.Get(role.After, "Policies", "0", "PolicyDocument", "Statement", "0", "Resource")
	want := map[string]any{"Fn::If": []any{"IsProd", "*", map[string]any{"Fn::GetAtt": []any{"Bucket", "Arn"}}}}
	if !reflect.DeepEqual(res, want) {
		t.Fatalf("!If: %#v", res)
	}
	if model.Get(role.After, "AssumeRolePolicyDocument", "Statement", "Principal") != "*" {
		t.Fatal("statement object")
	}
	if b.Pos.Line != 10 || db.Pos.Line != 24 {
		t.Fatalf("yaml lines %d %d", b.Pos.Line, db.Pos.Line)
	}
	j := load(t, model.KindCFN, "template.json")
	if j["Table"].Meta["deletion_policy"] != "Retain" || j["Table"].Pos.Pointer != "/Resources/Table" {
		t.Fatalf("json: %+v", j["Table"])
	}
	cdk := load(t, model.KindCFN, "AcmeStack.template.json")
	if cdk["AssetsBucket1A2B3C4D"].Meta["cdk_path"] != "AcmeStack/AssetsBucket/Resource" {
		t.Fatalf("cdk: %+v", cdk["AssetsBucket1A2B3C4D"])
	}
}

func TestChangeSetSemantics(t *testing.T) {
	m := load(t, model.KindChangeSet, "changeset.json")
	db := m["Db"]
	if acts(db) != "replace" || db.Meta["replacement"] != "True" || db.Meta["policy_action"] != "ReplaceAndDelete" || db.Meta["raw_action"] != "Modify" {
		t.Fatalf("db: %+v", db)
	}
	if db.After["MasterUserPassword"] != model.RedactedKey || db.Before["DBInstanceIdentifier"] != "acme-db" {
		t.Fatalf("contexts: %+v %+v", db.After, db.Before)
	}
	if b, _ := model.Bool(db.After["PubliclyAccessible"]); !b {
		t.Fatal("after context")
	}
	if acts(m["Fn"]) != "replace" || acts(m["Topic"]) != "update" || acts(m["Queue"]) != "create" {
		t.Fatalf("modify variants: %s %s %s", acts(m["Fn"]), acts(m["Topic"]), acts(m["Queue"]))
	}
	old := m["OldBucket"]
	if acts(old) != "delete" || old.Meta["policy_action"] != "Retain" || old.Meta["physical_id"] != "acme-old" {
		t.Fatalf("remove: %+v", old)
	}
	if acts(m["Imported"]) != "import" || acts(m["Nested"]) != "unknown" || acts(m["Synced"]) != "no-op" {
		t.Fatal("import/dynamic/sync")
	}
	if m["Db"].Pos.Pointer != "/Changes/0" {
		t.Fatal("pointer")
	}
}

func TestPulumiSemantics(t *testing.T) {
	m := load(t, model.KindPulumi, "preview.json")
	pre := "urn:pulumi:prod::acme::"
	if _, ok := m[pre+"pulumi:pulumi:Stack::acme-prod"]; !ok {
		t.Fatal("stack missing")
	}
	pol := m[pre+"aws:iam/policy:Policy::admin"]
	if pol.Type != "aws:iam/policy:Policy" || acts(pol) != "create" || pol.Meta["provider"] == "" {
		t.Fatalf("policy: %+v", pol)
	}
	if _, ok := pol.Allowed("IAM001"); !ok {
		t.Fatal("tags")
	}
	db := m[pre+"acme:index:Db$aws:rds/instance:Instance::main"]
	if acts(db) != "replace" || db.Meta["raw_action"] != "create-replacement,replace,delete-replaced" || db.Meta["protect"] != "true" {
		t.Fatalf("replace merge: %+v", db)
	}
	if db.After["password"] != model.RedactedSensitive {
		t.Fatalf("secret: %v", db.After["password"])
	}
	if b, _ := model.Bool(db.Before["publiclyAccessible"]); b {
		t.Fatal("before")
	}
	if db.Pos.Pointer != "/steps/2" {
		t.Fatalf("first step pointer %q", db.Pos.Pointer)
	}
	del := m[pre+"aws:s3/bucketV2:BucketV2::old"]
	if acts(del) != "delete" || del.Type != "aws:s3/bucketV2:BucketV2" || del.After != nil {
		t.Fatalf("delete: %+v", del)
	}
	sa := m[pre+"azure-native:storage:StorageAccount::sa"]
	if acts(sa) != "update" || model.IsRedacted(sa.After["connectionString"]) == false {
		t.Fatalf("update: %+v", sa)
	}
	if acts(m[pre+"aws:ec2/vpc:Vpc::shared"]) != "read" || acts(m[pre+"aws:s3/bucketV2:BucketV2::adopted"]) != "import" {
		t.Fatal("read/import")
	}
	if len(m) != 7 {
		t.Fatalf("%d resources", len(m))
	}
}

func TestPulumiTypeFromURN(t *testing.T) {
	cases := map[string]string{
		"urn:pulumi:prod::acme::aws:s3/bucketV2:BucketV2::old":                 "aws:s3/bucketV2:BucketV2",
		"urn:pulumi:prod::acme::acme:index:Db$aws:rds/instance:Instance::main": "aws:rds/instance:Instance",
		"urn:pulumi:prod::acme::pulumi:pulumi:Stack::acme-prod":                "pulumi:pulumi:Stack",
		"urn:pulumi:prod::acme::aws:s3/bucket:Bucket::name::with::colons":      "aws:s3/bucket:Bucket",
		"not-a-urn": "",
	}
	for urn, want := range cases {
		if got := pulumiTypeFromURN(urn); got != want {
			t.Errorf("%s: got %q want %q", urn, got, want)
		}
	}
}

func TestDetectByContent(t *testing.T) {
	cases := map[string]model.Kind{
		`{"format_version":"1.2","terraform_version":"1.9.0","planned_values":{}}`: model.KindTFPlan,
		`{"changes":[],"status":"Succeeded"}`:                                      model.KindWhatIf,
		`{"Changes":[],"ChangeSetName":"x"}`:                                       model.KindChangeSet,
		`{"steps":[],"changeSummary":{}}`:                                          model.KindPulumi,
		`{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#","resources":[]}`:             model.KindARM,
		`{"$schema":"https://schema.management.azure.com/schemas/2018-05-01/subscriptionDeploymentTemplate.json#","resources":[]}`: model.KindARM,
		`{"Resources":{"A":{"Type":"AWS::SNS::Topic"}}}`:                                                                           model.KindCFN,
		"Resources:\n  A:\n    Type: AWS::SNS::Topic\n":                                                                            model.KindCFN,
		"AWSTemplateFormatVersion: '2010-09-09'\nResources: {}\n":                                                                  model.KindCFN,
		`{"Resources":{"A":{"Type":"Custom"}}}`:                                                                                    "",
		"key: value\n":                                                                                                             "",
		"":                                                                                                                         "",
	}
	for doc, want := range cases {
		if got := Detect("x", []byte(doc)); got != want {
			t.Errorf("%q: got %q want %q", doc, got, want)
		}
	}
}

func TestBOMAndCRLF(t *testing.T) {
	raw := []byte("\xef\xbb\xbf{\r\n  \"Resources\": {\r\n    \"A\": {\r\n      \"Type\": \"AWS::SNS::Topic\"\r\n    }\r\n  }\r\n}\r\n")
	rs, err := Read(model.KindCFN, "t.json", raw, Options{})
	if err != nil || len(rs) != 1 || rs[0].Pos.Line != 3 {
		t.Fatalf("%+v %v", rs, err)
	}
	if Detect("t.json", raw) != model.KindCFN {
		t.Fatal("detect with BOM")
	}
}

func TestWhatIfFailed(t *testing.T) {
	raw := []byte(`{"changes":null,"error":{"code":"InvalidTemplate","message":"secret-ish detail tester-x"},"status":"Failed"}`)
	_, err := Read(model.KindWhatIf, "w.json", raw, Options{})
	if !errors.Is(err, ErrWhatIfFailed) || strings.Contains(err.Error(), "tester-x") {
		t.Fatalf("err = %v", err)
	}
	empty, err := Read(model.KindWhatIf, "w.json", []byte(`{"changes":[],"status":"Succeeded"}`), Options{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty what-if: %v %v", empty, err)
	}
}

func TestYAMLAliasBomb(t *testing.T) {
	var b strings.Builder
	b.WriteString("Resources:\n  A:\n    Type: AWS::SNS::Topic\n    Properties:\n")
	b.WriteString("      l0: &l0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i < 9; i++ {
		b.WriteString("      l" + strconv.Itoa(i) + ": &l" + strconv.Itoa(i) + " [")
		for j := 0; j < 10; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("*l" + strconv.Itoa(i-1))
		}
		b.WriteString("]\n")
	}
	if _, err := Read(model.KindCFN, "bomb.yaml", []byte(b.String()), Options{}); err == nil || !strings.Contains(err.Error(), "alias") {
		t.Fatalf("alias bomb: err = %v", err)
	}
}

func TestYAMLAlias(t *testing.T) {
	raw := []byte("Resources:\n  A:\n    Type: AWS::SNS::Topic\n    Properties: &p\n      TopicName: acme\n  B:\n    Type: AWS::SNS::Topic\n    Properties: *p\n")
	rs, err := Read(model.KindCFN, "t.yaml", raw, Options{})
	if err != nil || len(rs) != 2 || rs[1].After["TopicName"] != "acme" {
		t.Fatalf("%+v %v", rs, err)
	}
}
