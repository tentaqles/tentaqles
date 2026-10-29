package readers

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// Review r0 Major 2: a paginated change set with a NextToken is incomplete.
func TestChangeSetIncomplete(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(fixtures, "invalid", "changeset-nexttoken.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rs, err := ChangeSet("cs.json", raw, Options{}); !errors.Is(err, ErrIncomplete) || rs != nil {
		t.Fatalf("NextToken: rs=%v err=%v", rs, err)
	}
	if _, err := Redact(model.KindChangeSet, raw); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("Redact of an incomplete change set: %v", err)
	}
	_, _, err = ReadAuto("cs.json", raw, Options{})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("ReadAuto: %v", err)
	}
	if strings.Contains(err.Error(), "tester-p2") {
		t.Fatal("the token is repeated in the error")
	}
	ok := []byte(`{"ChangeSetName":"c","StackName":"s","NextToken":"","Changes":[]}`)
	if _, err := ChangeSet("cs.json", ok, Options{}); err != nil {
		t.Fatalf("empty NextToken: %v", err)
	}
}

// Review r0 Minor 1: a what-if that did not succeed is never "no changes".
func TestWhatIfNotSucceeded(t *testing.T) {
	for _, raw := range []string{
		`{"status":"Failed","changes":null}`,
		`{"status":"Failed","changes":[]}`,
		`{"status":"Running","changes":[]}`,
		`{"changes":null,"potentialChanges":[]}`,
		`{"status":"Failed","properties":{"changes":[]}}`,
	} {
		if _, err := WhatIf("w.json", []byte(raw), Options{}); !errors.Is(err, ErrWhatIfFailed) {
			t.Errorf("%s: err = %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{"status":"Succeeded","changes":[]}`,
		`{"changes":[],"potentialChanges":[]}`,
		`{"status":"succeeded","properties":{"changes":[]}}`,
	} {
		if _, err := WhatIf("w.json", []byte(raw), Options{}); err != nil {
			t.Errorf("%s: err = %v", raw, err)
		}
	}
}

// Review r0 Minor 2: a known sensitive value never rewrites the plan's
// structure (actions, addresses, types, version fields).
func TestRedactKnownKeepsStructure(t *testing.T) {
	plan := `{"format_version":"1.2","terraform_version":"1.9.0",
"variables":{"p":{"value":"delete"},"q":{"value":"aws_db_instance"},"r":{"value":"acmedb"},"v":{"value":"1.2"}},
"configuration":{"root_module":{"variables":{"p":{"sensitive":true},"q":{"sensitive":true},"r":{"sensitive":true},"v":{"sensitive":true}}}},
"resource_changes":[{"address":"aws_db_instance.acmedb","type":"aws_db_instance","name":"acmedb","mode":"managed","provider_name":"registry.terraform.io/hashicorp/aws",
"change":{"actions":["delete"],"before":{"identifier":"acmedb","note":"delete","engine":"aws_db_instance"},"after":null}}]}`
	red, err := Redact(model.KindTFPlan, []byte(plan))
	if err != nil {
		t.Fatal(err)
	}
	rs, err := TFPlan("r.json", red, Options{})
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if len(rs) != 1 || rs[0].Addr != "aws_db_instance.acmedb" || rs[0].Type != "aws_db_instance" || acts(rs[0]) != "delete" {
		t.Fatalf("structure changed: %+v", rs)
	}
	if rs[0].Meta["mode"] != "managed" || rs[0].Meta["provider"] != "registry.terraform.io/hashicorp/aws" {
		t.Fatalf("meta changed: %v", rs[0].Meta)
	}
	// The values are still redacted.
	for _, k := range []string{"identifier", "note", "engine"} {
		if rs[0].Before[k] != model.RedactedSensitive {
			t.Errorf("before.%s = %v", k, rs[0].Before[k])
		}
	}
	if Detect("r.json", red) != model.KindTFPlan {
		t.Fatal("redacted copy no longer detects as a plan")
	}
}

// Review r0 Minor 3: ARM nesting deeper than the bound is an error.
func TestARMTooDeep(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#","contentVersion":"1.0.0.0","resources":[`)
	n := maxNesting + 3
	for i := 0; i < n; i++ {
		b.WriteString(`{"type":"Microsoft.Resources/deployments","name":"d","properties":{"mode":"Incremental","template":{"resources":[`)
	}
	b.WriteString(`{"type":"Microsoft.Sql/servers","name":"acme"}`)
	for i := 0; i < n; i++ {
		b.WriteString(`]}}}`)
	}
	b.WriteString(`]}`)
	if _, err := ARM("deep.json", []byte(b.String()), Options{}); err == nil || !strings.Contains(err.Error(), "nest") {
		t.Fatalf("too deep: err = %v", err)
	}
}

// Review r0 Minor 4: a YAML merge with a list of maps merges each, earlier
// maps first; explicit keys win.
func TestYAMLMergeList(t *testing.T) {
	y := "AWSTemplateFormatVersion: '2010-09-09'\nX: &a\n  PubliclyAccessible: true\n  Engine: mysql\nY: &b\n  Engine: postgres\n  StorageEncrypted: false\nResources:\n  Db:\n    Type: AWS::RDS::DBInstance\n    Properties:\n      <<: [*a, *b]\n      StorageEncrypted: true\n"
	rs, err := CFN("t.yaml", []byte(y), Options{})
	if err != nil || len(rs) != 1 {
		t.Fatalf("%v %v", rs, err)
	}
	a := rs[0].After
	if a["PubliclyAccessible"] != true || a["Engine"] != "mysql" || a["StorageEncrypted"] != true {
		t.Fatalf("merged %v", a)
	}
	bad := "AWSTemplateFormatVersion: '2010-09-09'\nResources:\n  Db:\n    Type: AWS::RDS::DBInstance\n    Properties:\n      <<: [x, y]\n"
	if _, err := CFN("t.yaml", []byte(bad), Options{}); err == nil {
		t.Fatal("merge of scalars: no error")
	}
}
