package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSecretKey(t *testing.T) {
	yes := []string{
		"password", "master_password", "administratorLoginPassword", "client_secret", "secret_string",
		"token", "sas_token", "connection_string", "primaryConnectionString", "primary_access_key",
		"private_key", "secret_key", "api_key", "AccountKey", "credentials", "password_wo",
		"instrumentation_key", "MasterUserPassword", "passphrase",
	}
	no := []string{
		"", "Key", "key", "kms_key_id", "KmsKeyId", "secret_name", "secret_arn", "password_policy",
		"token_lifetime", "key_vault_id", "tags", "Value", "public_key", "customer_managed_key",
		"secret_rotation", "manage_master_user_password_enabled", "SecretId", "key_name", "ssh_key",
		"from_port", "cidr_blocks", "policy", "id",
	}
	for _, k := range yes {
		if !SecretKey(k) {
			t.Errorf("%q should be a secret key", k)
		}
	}
	for _, k := range no {
		if SecretKey(k) {
			t.Errorf("%q should not be a secret key", k)
		}
	}
}

func TestMask(t *testing.T) {
	v := map[string]any{
		"name":     "acme",
		"password": "hunter2-tester-pass",
		"nested":   map[string]any{"a": "keep", "b": map[string]any{"deep": "s3cr3t-value"}},
		"list":     []any{"one", "two-sensitive"},
		"nullish":  nil,
	}
	mask := map[string]any{
		"password": true,
		"nested":   map[string]any{"b": true},
		"list":     []any{false, true},
		"nullish":  true,
	}
	var k Known
	got := Mask(v, mask, &k).(map[string]any)
	if got["name"] != "acme" || got["password"] != RedactedSensitive {
		t.Fatalf("leaf mask: %v", got)
	}
	if Get(got, "nested", "a") != "keep" || Get(got, "nested", "b") != RedactedSensitive {
		t.Fatalf("object mask: %v", got["nested"])
	}
	if !reflect.DeepEqual(got["list"], []any{"one", RedactedSensitive}) || got["nullish"] != nil {
		t.Fatalf("list mask: %v", got)
	}
	if v["password"] != "hunter2-tester-pass" {
		t.Fatal("Mask modified its input")
	}
	if k.Len() != 3 {
		t.Fatalf("known %d", k.Len())
	}
	// A whole-value mask.
	if Mask(map[string]any{"x": 1}, true, nil) != RedactedSensitive {
		t.Fatal("root mask")
	}
	// Shape mismatches leave the value alone.
	if Mask("str", map[string]any{"a": true}, nil) != "str" || Mask(nil, false, nil) != nil {
		t.Fatal("mismatch")
	}
}

func TestKnownReplace(t *testing.T) {
	var k Known
	k.Add("hunter2-tester-pass")
	k.Add("abc") // short: exact matches only
	k.Add(json.Number("123456789"))
	doc := map[string]any{
		"default": "hunter2-tester-pass",
		"url":     "postgres://tester@db/acme?pw=hunter2-tester-pass",
		"word":    "abc",
		"abcdef":  "contains abc but is not abc",
		"n":       json.Number("123456789"),
		"list":    []any{"hunter2-tester-pass"},
	}
	got := k.Replace(doc).(map[string]any)
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "hunter2") || strings.Contains(string(b), "123456789") {
		t.Fatalf("leak: %s", b)
	}
	if got["word"] != RedactedSensitive || got["abcdef"] != "contains abc but is not abc" {
		t.Fatalf("short values: %v", got)
	}
	var empty *Known
	if empty.Replace("x") != "x" {
		t.Fatal("nil Known")
	}
}

func TestScrub(t *testing.T) {
	v := map[string]any{
		"name":                         "acme",
		"administrator_login_password": "Pa55word-tester",
		"port":                         json.Number("5432"),
		"master_password":              json.Number("12345678"),
		"manage_master_user_password":  true,
		"connection_strings":           []any{map[string]any{"value": "Server=acme;Password=xyz"}},
		"secret":                       map[string]any{"nested": "deep-value", "flag": false},
		"note":                         "uses ghp_" + strings.Repeat("A", 36),
		"pulumi":                       map[string]any{PulumiSecretSig: "1b47061264138c4ac30d75fd1eb44270", "plaintext": "\"pulumi-secret\""},
		"tags":                         []any{map[string]any{"Key": "env", "Value": "prod"}},
		"kms_key_id":                   "arn:aws:kms:us-east-1:000000000000:key/1111",
		"empty_password":               "",
	}
	got := Scrub(v).(map[string]any)
	b, _ := json.Marshal(got)
	s := string(b)
	for _, leak := range []string{"Pa55word", "12345678", "xyz", "deep-value", "ghp_AAAA", "pulumi-secret"} {
		if strings.Contains(s, leak) {
			t.Errorf("leak %q in %s", leak, s)
		}
	}
	if got["name"] != "acme" || got["port"] != json.Number("5432") || got["manage_master_user_password"] != true {
		t.Fatalf("over-redacted: %v", got)
	}
	if Get(got, "secret", "flag") != false || got["administrator_login_password"] != RedactedKey || got["pulumi"] != RedactedSensitive {
		t.Fatalf("markers: %v", got)
	}
	if !reflect.DeepEqual(got["tags"], v["tags"]) || got["kms_key_id"] != v["kms_key_id"] || got["empty_password"] != "" {
		t.Fatalf("kept values changed: %v", got)
	}
	if v["administrator_login_password"] != "Pa55word-tester" {
		t.Fatal("Scrub modified its input")
	}
	pairs := Scrub([]any{
		map[string]any{"Name": "DB_PASSWORD", "Value": "tester-env-pass"},
		map[string]any{"Key": "api_token", "Value": "tester-tag-token"},
		map[string]any{"ParameterKey": "AdminSecret", "ParameterValue": "tester-param"},
		map[string]any{"Key": "team", "Value": "acme"},
	}).([]any)
	for i := 0; i < 3; i++ {
		b, _ := json.Marshal(pairs[i])
		if strings.Contains(string(b), "tester-") {
			t.Errorf("pair %d leaks: %s", i, b)
		}
	}
	if Get(pairs[3], "Value") != "acme" || Get(pairs[0], "Name") != "DB_PASSWORD" {
		t.Fatalf("pairs over-redacted: %v", pairs)
	}
	refs := Scrub(map[string]any{
		"MasterUserPassword":         map[string]any{"Ref": "DbPassword"},
		"administratorLoginPassword": "[parameters('adminPassword')]",
		"password":                   "{{resolve:secretsmanager:acme:SecretString:password}}",
		"client_secret":              map[string]any{"Fn::Sub": "literal-${AWS::Region}"},
		"secret":                     "[REDACTED:sensitive]",
	}).(map[string]any)
	if !reflect.DeepEqual(refs["MasterUserPassword"], map[string]any{"Ref": "DbPassword"}) ||
		refs["administratorLoginPassword"] != "[parameters('adminPassword')]" ||
		refs["password"] != "{{resolve:secretsmanager:acme:SecretString:password}}" ||
		refs["secret"] != "[REDACTED:sensitive]" {
		t.Fatalf("references redacted: %v", refs)
	}
	if !reflect.DeepEqual(refs["client_secret"], map[string]any{"Fn::Sub": RedactedKey}) {
		t.Fatalf("Fn::Sub can hold a literal: %v", refs["client_secret"])
	}
	if Scrub("[secret]") != RedactedSensitive {
		t.Fatal("pulumi [secret]")
	}
	if ScrubMap(nil) != nil {
		t.Fatal("ScrubMap nil")
	}
}
