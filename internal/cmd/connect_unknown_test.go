package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/connect"
)

// L-19: connect with an id the catalog does not have names the catalog, exits 1, logs no denial
// line (a typo is not a crossing), and writes nothing: neither .claude.json nor the connection log.
func TestConnect_UnknownProviderNamesTheCatalog(t *testing.T) {
	root, _ := setupScoped(t, "acme", ghBody)
	chdir(t, root)
	before := 0
	if b, err := os.ReadFile(audit.DenialsPath()); err == nil {
		before = strings.Count(string(b), "\n")
	}
	out, err := runTq(t, "connect", "nosuch")
	if err == nil || !strings.Contains(err.Error(), "provider catalog") {
		t.Fatalf("connect nosuch: err=%v out=%q, want a refusal naming the catalog", err, out)
	}
	if got := fileBytes(t, claudeJSONOf("acme")); got != "<missing>" {
		t.Errorf("the Claude config was written: %s", got)
	}
	if _, serr := os.Stat(connect.LogPath()); serr == nil {
		t.Error("the connection log was written")
	}
	after := 0
	if b, rerr := os.ReadFile(audit.DenialsPath()); rerr == nil {
		after = strings.Count(string(b), "\n")
	}
	if after != before {
		t.Errorf("an unknown id added %d denial line(s)", after-before)
	}
}
