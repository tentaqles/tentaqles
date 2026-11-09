package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// reach refuses a copy of a client's marker and names tentaqles bind, and changes nothing.
func TestReach_CopiedMarkerIsNotAClient(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	if out, err := runReach(t, filepath.Join(clients, "acme")); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	copyDir := copiedClient(t, clients, "acme")
	before := registryBytes(t)
	out, err := runReach(t, copyDir)
	if err == nil || !strings.Contains(err.Error(), "tentaqles bind") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !bytes.Equal(before, registryBytes(t)) {
		t.Fatal("a refused reach changed the registry")
	}
}
