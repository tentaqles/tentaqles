package manifest

import (
	"reflect"
	"strings"
	"testing"
)

const dataManifest = `
schema: tentaqles-client-v2
client: acme
data:
  classes: [pii, financial]
  regions: [BR]
  prompts: { allow: [public, internal] }
  paths:
    - { path: "db/schema/customers.sql", classes: [pii] }
    - { path: "data\\exports\\**", classes: [pii, financial] }
    - { table: public.payments, classes: [financial] }
  retention_default: 24m
  incident: { authority: ANPD, contact: dpo@example.test }
`

func TestData_ParsesAndEnables(t *testing.T) {
	m, err := Load(write(t, dataManifest))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Data.Enabled() || m.Data.RetentionDefault != "24m" || m.Data.Incident.Authority != "ANPD" {
		t.Fatalf("%+v", m.Data)
	}
	if got := m.Data.Tables()["public.payments"]; !reflect.DeepEqual(got, []string{"financial"}) {
		t.Fatalf("tables %v", m.Data.Tables())
	}
	if got := m.Data.Tables()["payments"]; !reflect.DeepEqual(got, []string{"financial"}) {
		t.Fatalf("bare table name must resolve too: %v", m.Data.Tables())
	}
}

func TestData_ClassifiedMatchesNormalisedPaths(t *testing.T) {
	m, err := Load(write(t, dataManifest))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		"db/schema/customers.sql":  {"pii"},
		`db\schema\customers.sql`:  {"pii"},
		"data/exports/2026/q3.csv": {"financial", "pii"},
		"data/exports/x.csv":       {"financial", "pii"},
		"db/schema/orders.sql":     nil,
		"src/data/exports/x.csv":   nil, // anchored at the repo root
	}
	for p, want := range cases {
		if got := m.Data.Classified(p); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v want %v", p, got, want)
		}
	}
}

func TestData_EmptyBlockIsDisabled(t *testing.T) {
	m, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\n"))
	if err != nil || m.Data.Enabled() || m.Data.Classified("anything") != nil {
		t.Fatalf("%v %+v", err, m.Data)
	}
}

func TestData_Validation(t *testing.T) {
	bad := map[string]string{
		"unknown class":        "data: { classes: [pii, private] }",
		"allow not in set":     "data: { classes: [pii], prompts: { allow: [pii] } }",
		"path and table":       "data: { classes: [pii], paths: [ { path: a, table: b, classes: [pii] } ] }",
		"path without classes": "data: { classes: [pii], paths: [ { path: a } ] }",
		"bad retention":        "data: { classes: [pii], retention_default: forever }",
		"bad authority":        "data: { classes: [pii], incident: { authority: FBI } }",
		"region not ISO":       "data: { classes: [pii], regions: [Brazil] }",
	}
	for name, body := range bad {
		_, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\n"+body+"\n"))
		if err == nil || !strings.Contains(err.Error(), "data.") {
			t.Errorf("%s: want a data.* validation error, got %v", name, err)
		}
	}
	ok := "schema: tentaqles-client-v2\nclient: x\ndata: { classes: [pii], retention_default: 30d, regions: [BR, EU], incident: { authority: GDPR } }\n"
	if _, err := Load(write(t, ok)); err != nil {
		t.Fatal(err)
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pat, path string
		want      bool
	}{
		{"src/core/**", "src/core/a/b.go", true},
		{"src/core/**", "src/core", true},
		{"src/*/x.go", "src/a/x.go", true},
		{"src/*/x.go", "src/a/b/x.go", false},
		{"**/migrations/*.sql", "db/migrations/1.sql", true},
		{"*.sql", "db/1.sql", false},
	}
	for _, c := range cases {
		if got := GlobMatch(c.pat, c.path); got != c.want {
			t.Errorf("GlobMatch(%q,%q)=%v", c.pat, c.path, got)
		}
	}
}
