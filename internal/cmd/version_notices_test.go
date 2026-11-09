package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersion_Notices(t *testing.T) {
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version", "--notices"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Trail-code", "agentmemory", "Graphify", "Graft",
		"MIT License", "Apache License",
		"==== third_party/licenses/graphify/NOTICE ====",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("--notices output does not contain %q", want)
		}
	}

	plain := NewRoot()
	var plainOut bytes.Buffer
	plain.SetOut(&plainOut)
	plain.SetArgs([]string{"version"})
	if err := plain.Execute(); err != nil {
		t.Fatal(err)
	}
	line := strings.TrimRight(plainOut.String(), "\n")
	if strings.Contains(line, "\n") || !strings.HasPrefix(line, "tentaqles ") || strings.Contains(line, "Apache") {
		t.Fatalf("version without --notices printed %q", plainOut.String())
	}
}
