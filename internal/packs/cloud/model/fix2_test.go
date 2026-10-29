package model

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Review r1 Minor 1: the encoded-string shapes round 1 still missed.
func TestScrubEncodedMoreShapes(t *testing.T) {
	cases := map[string]string{
		"yaml list root":   "- \"name\": \"DB_PASSWORD\"\n  \"value\": \"tester-yl1\"\n- \"name\": \"MODE\"\n  \"value\": \"prod\"\n",
		"one-key yaml":     "\"password\": \"tester-ok1\"\n",
		"one-key bare":     "password: tester-ok2\n",
		"one-key no nl":    "api_token: tester-ok3",
		"yaml flow map":    "{password: tester-fm1, host: db.acme.internal}",
		"yaml flow list":   "[{name: DB_PASSWORD, value: tester-fl1}]",
		"two json docs":    `{"password":"tester-tj1"} {"password":"tester-tj2"}`,
		"json then text":   `{"password":"tester-tj3"} trailing`,
		"json lines":       "{\"a\":1}\n{\"secret\":\"tester-tj4\"}\n",
		"multi-doc yaml":   "a: 1\n---\npassword: tester-md1\n",
		"yaml list scalar": "- tester-ignored\n- password: tester-ls1\n",
	}
	leaks := []string{"tester-yl1", "tester-ok1", "tester-ok2", "tester-ok3", "tester-fm1", "tester-fl1", "tester-tj1", "tester-tj2", "tester-tj3", "tester-tj4", "tester-md1", "tester-ls1"}
	for name, s := range cases {
		got := Scrub(map[string]any{"v": s})
		b, _ := json.Marshal(got)
		for _, l := range leaks {
			if strings.Contains(string(b), l) {
				t.Errorf("%s: leak %q in %s", name, l, b)
			}
		}
	}
	// The list root re-encodes as YAML with the non-secret value kept.
	got := Scrub(cases["yaml list root"]).(string)
	var v any
	if err := yaml.Unmarshal([]byte(got), &v); err != nil {
		t.Fatalf("list root not YAML after scrub: %v (%q)", err, got)
	}
	if !strings.Contains(got, "prod") {
		t.Fatalf("list root lost a value: %q", got)
	}
	// The flow mapping keeps its other value.
	if got := Scrub(cases["yaml flow map"]).(string); !strings.Contains(got, "db.acme.internal") {
		t.Fatalf("flow map lost a value: %q", got)
	}
	// Plain strings and strings with nothing to redact stay byte for byte.
	for _, keep := range []string{
		"hello", "key: value", "a: b", "- item", "[parameters('name')]", "{not yaml: [",
		"Fn::Sub: ${AWS::Region}", "http://acme.example:8080/path", "{a: 1, b: 2}",
		"- one\n- two\n", `{"a":1} {"b":2}`, "note: see docs\n",
	} {
		if got := Scrub(keep); got != keep {
			t.Errorf("Scrub(%q) = %q, want it unchanged", keep, got)
		}
	}
}
