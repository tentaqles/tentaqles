package cmd

import (
	"bytes"
	"os"
	"path/filepath"
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
	if !strings.Contains(out.String(), "tentaqles 1.2.3") {
		t.Fatalf("got %q", out.String())
	}
}

// tentaqles version names the commit it was built from, or says it is a dev build.
func TestVersionPrintsTheCommit(t *testing.T) {
	prevV, prevC := Version, Commit
	t.Cleanup(func() { Version, Commit = prevV, prevC })
	Version, Commit = "1.2.3", "642dfb458723ee4373447cc80bda9c77af70daa2"
	out, err := runTq(t, "version")
	if err != nil || out != "tentaqles 1.2.3 (commit 642dfb458723ee4373447cc80bda9c77af70daa2)\n" {
		t.Fatalf("%v %q", err, out)
	}
	Commit = ""
	if out, _ := runTq(t, "version"); out != "tentaqles 1.2.3 (commit unknown (dev build))\n" {
		t.Fatalf("%q", out)
	}
}

// The release build injects the commit: .goreleaser.yaml sets cmd.Commit
// from {{.FullCommit}} next to cmd.Version.
func TestReleaseBuildInjectsTheCommit(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "-X github.com/tentaqles/tentaqles/internal/cmd.Commit={{.FullCommit}}") {
		t.Fatalf(".goreleaser.yaml must inject cmd.Commit:\n%s", raw)
	}
}
