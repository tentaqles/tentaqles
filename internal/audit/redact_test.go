package audit

import (
	"strings"
	"testing"
)

func TestRedactCommandTruncates(t *testing.T) {
	got := RedactCommand(strings.Repeat("ls a ", 200))
	if n := len([]rune(got)); n > MaxCommandRunes+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("len %d, %q", n, got)
	}
}

// A denial written with a secret in any text field never lands with it.
func TestAppendDenialMasksEveryField(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	tok := "ghp_abcdefghijklmnopqrstuvwxyz0123456789" // tq:allow-secret (fake fixture)
	if err := AppendDenial(Denial{Category: "identity", Layer: LayerHook, Command: "x " + tok, Subject: tok, Reason: "because " + tok}); err != nil {
		t.Fatal(err)
	}
	ds, err := ReadDenials()
	if err != nil || len(ds) != 1 {
		t.Fatalf("%v %v", ds, err)
	}
	d := ds[0]
	for _, f := range []string{d.Command, d.Subject, d.Reason} {
		if strings.Contains(f, tok) {
			t.Errorf("field kept the token: %q", f)
		}
	}
}

// Lines of the Phase 1 schema (kind, workspace) still read.
func TestReadDenialsOldSchema(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", home)
	if err := appendLine(DenialsPath(), map[string]any{"kind": "human-only", "command": "tq allow", "reason": "r", "workspace": "acme"}); err != nil {
		t.Fatal(err)
	}
	if err := appendLine(DenialsPath(), map[string]any{"kind": "secrets", "layer": "hook", "command": "printenv", "reason": "r", "workspace": "acme"}); err != nil {
		t.Fatal(err)
	}
	ds, err := ReadDenials()
	if err != nil || len(ds) != 2 {
		t.Fatalf("%v %v", ds, err)
	}
	if ds[0].Category != "self-protection" || ds[0].Layer != LayerHumanOnly || ds[0].Identity != "acme" {
		t.Errorf("old human-only line: %+v", ds[0])
	}
	if ds[1].Category != "secrets" || ds[1].Identity != "acme" {
		t.Errorf("old hook line: %+v", ds[1])
	}
}
