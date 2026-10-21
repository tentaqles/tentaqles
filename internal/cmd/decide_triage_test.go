package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

func TestClassifiedFlag(t *testing.T) {
	d := manifest.Data{Classes: []string{"pii"}, Paths: []manifest.DataPath{{Path: "db/schema/**", Classes: []string{"pii"}}}}
	if !classifiedFlag([]string{"README.md", "db/schema/customers.sql"}, d) {
		t.Fatal("classified file not flagged")
	}
	if classifiedFlag([]string{"README.md"}, d) || classifiedFlag([]string{"db/schema/x.sql"}, manifest.Data{}) {
		t.Fatal("false positive")
	}
}

func TestTriage_ClassifiedDataFlagWithoutJev(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "data:\n  classes: [pii]\n  paths:\n    - { path: \"db/schema/**\", classes: [pii] }\n")
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = ws
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.MkdirAll(filepath.Join(ws, "db", "schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "db", "schema", "customers.sql"), []byte("create table customers(email text);\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "db/schema/customers.sql")
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(ws); err != nil {
		t.Fatal(err)
	}
	var code int
	prevExit := exitFunc
	exitFunc = func(c int) { code = c; panic(hookExitSentinel{}) }
	t.Cleanup(func() { exitFunc = prevExit })
	var out bytes.Buffer
	root := NewRoot()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"decide", "triage", "--staged"})
	func() {
		defer func() { _ = recover() }()
		_ = root.Execute()
	}()
	if code != 3 || !strings.Contains(out.String(), "classified_data") {
		t.Fatalf("code=%d out=%q", code, out.String())
	}
}
