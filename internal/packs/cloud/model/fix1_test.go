package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// Review r0 Major 1: secrets inside JSON- or YAML-encoded string attributes.
func TestScrubEncodedStrings(t *testing.T) {
	ecs := `[{"name":"app","image":"acme/app:1","environment":[{"name":"DB_PASSWORD","value":"tester-ep1"},{"name":"MODE","value":"prod"}]}]`
	helm := "db:\n  host: db.acme.internal\n  password: tester-hp1\nreplicas: 2\n"
	nested := `{"config":"{\"api_token\":\"tester-np1\"}"}`
	v := map[string]any{
		"container_definitions": ecs,
		"values":                []any{helm},
		"nested":                nested,
	}
	got := Scrub(v).(map[string]any)
	b, _ := json.Marshal(got)
	for _, leak := range []string{"tester-ep1", "tester-hp1", "tester-np1"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("leak %q in %s", leak, b)
		}
	}
	// The re-encoded strings stay readable as their format, with the
	// non-secret values kept.
	var cd []any
	if err := json.Unmarshal([]byte(got["container_definitions"].(string)), &cd); err != nil {
		t.Fatalf("container_definitions not JSON after scrub: %v", err)
	}
	if Get(cd, "0", "environment", "1", "value") != "prod" || Get(cd, "0", "environment", "0", "value") != RedactedKey {
		t.Fatalf("container_definitions: %v", cd)
	}
	h := Get(got, "values", "0").(string)
	if !strings.Contains(h, "db.acme.internal") || !strings.Contains(h, RedactedKey) {
		t.Fatalf("helm values: %q", h)
	}
	// A string with nothing to redact is kept byte for byte (policy JSON,
	// ARM expressions, plain multi-line text).
	policy := `{"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Action": "s3:GetObject", "Resource": "*"}]}`
	for _, keep := range []string{policy, "[parameters('name')]", "line one\nline two", "[1, 2]", "key: value\nother: x <&>"} {
		if got := Scrub(keep); got != keep {
			t.Errorf("Scrub(%q) = %q, want it unchanged", keep, got)
		}
	}
}

// Review r0 Major 3: an unknown action on a stateful resource is a possible destroy.
func TestSummarizeStatefulUnknown(t *testing.T) {
	rs := []Resource{
		{Addr: "Db", Type: "AWS::RDS::DBInstance", Actions: []string{ActUnknown}},
		{Addr: "Topic", Type: "AWS::SNS::Topic", Actions: []string{ActUnknown}},
	}
	s := Summarize(rs)
	if len(s.StatefulUnknown) != 1 || s.StatefulUnknown[0].Addr != "Db" {
		t.Fatalf("stateful unknown %+v", s.StatefulUnknown)
	}
	if !s.HasStatefulDestroy() {
		t.Fatal("an unknown action on a stateful resource must count as a possible destroy")
	}
	if !strings.Contains(s.Text(), "stateful unknown: Db") {
		t.Fatalf("text %q", s.Text())
	}
	if !rs[0].MayDestroy() || rs[0].Destroys() || (Resource{Actions: []string{ActUpdate}}).MayDestroy() {
		t.Fatal("MayDestroy")
	}
	// A replace that also carries unknown is listed once, as a replace.
	s2 := Summarize([]Resource{{Addr: "B", Type: "aws_s3_bucket", Actions: []string{ActReplace, ActUnknown}}})
	if len(s2.StatefulReplaces) != 1 || len(s2.StatefulUnknown) != 0 {
		t.Fatalf("replace+unknown %+v", s2)
	}
}

// Review r0 Minor 5: gaps in the stateful table.
func TestStatefulAdditions(t *testing.T) {
	for _, s := range []string{
		"AWS::Serverless::SimpleTable", "azurerm_key_vault_secret", "azurerm_key_vault_certificate",
		"Microsoft.KeyVault/vaults/secrets", "azurerm_storage_table", "azurerm_storage_queue",
		"azurerm_cosmosdb_mongo_collection", "azurerm_cosmosdb_cassandra_keyspace", "azurerm_cosmosdb_gremlin_database",
		"azurerm_cosmosdb_table",
		"Microsoft.DocumentDB/databaseAccounts/sqlDatabases", "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers",
		"Microsoft.DocumentDB/databaseAccounts/mongodbDatabases/collections",
		"Microsoft.Storage/storageAccounts/tableServices/tables", "Microsoft.Storage/storageAccounts/queueServices/queues",
		"azure:cosmosdb/sqlDatabase:SqlDatabase", "azure:cosmosdb/sqlContainer:SqlContainer",
		"azure:keyvault/secret:Secret", "azure:storage/table:Table", "azure:storage/queue:Queue",
		"azure-native:documentdb:SqlResourceSqlDatabase", "azure-native:documentdb:SqlResourceSqlContainer",
		"azure-native:keyvault:Secret", "azure-native:storage:Table", "azure-native:storage:Queue",
	} {
		if !Stateful(s) {
			t.Errorf("%s should be stateful", s)
		}
	}
	for _, s := range []string{"azure-native:storage:TableServiceProperties", "azure-native:keyvault:SecretPolicy"} {
		if Stateful(s) {
			t.Errorf("%s should not be stateful", s)
		}
	}
}

// Review r0 Minor 6: which allow tag wins is fixed.
func TestSuppressionsTagOrder(t *testing.T) {
	r := Resource{After: map[string]any{"tags": map[string]any{
		"tq_allow": "IAM001: from underscore",
		"TQ-Allow": "IAM002: from mixed case",
		"tq-allow": "IAC001: from canonical",
	}}}
	for i := 0; i < 50; i++ {
		got := r.Suppressions()
		if len(got) != 1 || got[0].Rule != "IAC001" {
			t.Fatalf("run %d: %+v", i, got)
		}
	}
	r2 := Resource{After: map[string]any{"tags": map[string]any{"tq_allow": "IAM001: u", "TQ-ALLOW": "IAM002: m"}}}
	for i := 0; i < 50; i++ {
		if got := r2.Suppressions(); len(got) != 1 || got[0].Rule != "IAM002" {
			t.Fatalf("run %d: %+v", i, got)
		}
	}
}
