package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// renderOnce runs renderIdentityFiles for client name and returns what it printed to stderr.
func renderOnce(t *testing.T, name string) string {
	t.Helper()
	ws, err := findWorkspace(name)
	if err != nil {
		t.Fatal(err)
	}
	return renderWS(t, ws)
}

// renderWS runs renderIdentityFiles for ws and returns what it printed to stderr.
func renderWS(t *testing.T, ws *resolve.Workspace) string {
	t.Helper()
	c := &cobra.Command{}
	var stderr bytes.Buffer
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&stderr)
	renderIdentityFiles(c, ws)
	return stderr.String()
}

// A render of a client whose identity folder does not exist prints no lobe store warning: there is no
// store to open yet.
func TestRenderLobeStore_MissingIdentityFolderIsNoWarning(t *testing.T) {
	base := newBase(t)
	writeClient(t, base, "acme", lobeManifestBody)
	ws, err := findWorkspace("acme")
	if err != nil {
		t.Fatal(err)
	}
	// The workspace is read first: its manifest lives in the identity folder that is removed here.
	if err := os.RemoveAll(paths.IdentityDir("acme", "")); err != nil {
		t.Fatal(err)
	}
	if got := renderWS(t, ws); strings.Contains(got, "lobe store") {
		t.Fatalf("a missing identity folder printed a lobe store warning:\n%s", got)
	}
}

// A refusal of the store is still a warning: here the store.json names another client.
func TestRenderLobeStore_RefusalStillWarns(t *testing.T) {
	base := newBase(t)
	writeClient(t, base, "acme", lobeManifestBody)
	writeTB(t, filepath.Join(paths.LobeDir("acme"), "store.json"), `{"v":1,"client":"globex"}`+"\n")
	if got := renderOnce(t, "acme"); !strings.Contains(got, "lobe store not opened") {
		t.Fatalf("a refused store printed no warning:\n%s", got)
	}
}
