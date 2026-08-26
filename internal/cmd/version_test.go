package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionPrintsVersion(t *testing.T) {
	Version = "1.2.3"
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "tq 1.2.3") {
		t.Fatalf("got %q", out.String())
	}
}
