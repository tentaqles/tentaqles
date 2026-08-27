package cmd

import (
	"bytes"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/providers"
)

func TestProvidersAdd_WritesUserFile(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"providers", "add", "widget-co",
		"--name", "Widget Co",
		"--category", "other",
		"--command", "widget",
		"--version-args", "--version",
		"--env", "WIDGET_HOME={dir}",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("providers add: %v\noutput: %s", err, out.String())
	}

	cat, err := providers.Load()
	if err != nil {
		t.Fatalf("providers.Load: %v", err)
	}
	p, ok := cat.Get("widget-co")
	if !ok {
		t.Fatalf("provider widget-co not found after add; output: %s", out.String())
	}
	if p.Name != "Widget Co" || p.Category != "other" {
		t.Fatalf("unexpected provider: %+v", p)
	}
	if p.CLI == nil || p.CLI.Command != "widget" {
		t.Fatalf("expected CLI.Command=widget, got %+v", p.CLI)
	}
	if p.Identity.Env["WIDGET_HOME"] != "{dir}" {
		t.Fatalf("expected identity env WIDGET_HOME={dir}, got %+v", p.Identity.Env)
	}
}

func TestProvidersAdd_RefusesOverwriteWithoutForce(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())

	run := func(args ...string) error {
		root := NewRoot()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		return root.Execute()
	}

	if err := run("providers", "add", "widget-co", "--name", "Widget Co", "--category", "other"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := run("providers", "add", "widget-co", "--name", "Widget Co v2", "--category", "other"); err == nil {
		t.Fatal("expected an error overwriting an existing user file without --force")
	}
	if err := run("providers", "add", "widget-co", "--name", "Widget Co v2", "--category", "other", "--force"); err != nil {
		t.Fatalf("add with --force: %v", err)
	}

	if err := run("providers", "add", "gh", "--name", "GitHub 2", "--category", "vcs"); err == nil {
		t.Fatal("expected an error overwriting an embedded provider without --force")
	}
	if err := run("providers", "add", "gh", "--name", "GitHub 2", "--category", "vcs", "--force"); err != nil {
		t.Fatalf("add gh with --force: %v", err)
	}
}

func TestProvidersCheck_ExitCode(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	prevExit := exitFunc
	var exitCode int
	exitCalled := false
	exitFunc = func(code int) { exitCode = code; exitCalled = true }
	t.Cleanup(func() { exitFunc = prevExit })

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"providers", "add", "not-on-path",
		"--name", "Not On Path",
		"--category", "other",
		"--command", "definitely-not-on-path-xyz",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("providers add: %v\noutput: %s", err, out.String())
	}

	out.Reset()
	root2 := NewRoot()
	root2.SetOut(&out)
	root2.SetErr(&out)
	root2.SetArgs([]string{"providers", "check", "not-on-path"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("providers check: %v\noutput: %s", err, out.String())
	}
	if !exitCalled {
		t.Fatalf("expected exitFunc to be called; output: %s", out.String())
	}
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !bytes.Contains(out.Bytes(), []byte("[missing] not-on-path")) {
		t.Fatalf("expected [missing] line, got: %s", out.String())
	}
}

func TestProvidersCheck_NoCLIDoesNotAffectExit(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	prevExit := exitFunc
	exitCalled := false
	exitFunc = func(int) { exitCalled = true }
	t.Cleanup(func() { exitFunc = prevExit })

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"providers", "add", "no-cli-provider",
		"--name", "No CLI",
		"--category", "other",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("providers add: %v\noutput: %s", err, out.String())
	}

	out.Reset()
	root2 := NewRoot()
	root2.SetOut(&out)
	root2.SetErr(&out)
	root2.SetArgs([]string{"providers", "check", "no-cli-provider"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("providers check: %v\noutput: %s", err, out.String())
	}
	if exitCalled {
		t.Fatal("providers with no CLI must not affect exit code")
	}
	if !bytes.Contains(out.Bytes(), []byte("[n/a] no-cli-provider")) {
		t.Fatalf("expected [n/a] line, got: %s", out.String())
	}
}

func TestProvidersList_JSON(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"providers", "list", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("providers list: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("expected JSON output")
	}
}

func TestProvidersShow(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"providers", "show", "gh"})
	if err := root.Execute(); err != nil {
		t.Fatalf("providers show: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("id: gh")) {
		t.Fatalf("expected yaml output with id: gh, got: %s", out.String())
	}
}

func TestProvidersAdd_RejectsBadEnvKey(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"providers", "add", "widget-co",
		"--name", "Widget Co",
		"--category", "other",
		"--env", "X;curl x|sh={dir}",
	})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected an error for a hostile --env key; output: %s", out.String())
	}

	cat, err := providers.Load()
	if err != nil {
		t.Fatalf("providers.Load: %v", err)
	}
	if _, ok := cat.Get("widget-co"); ok {
		t.Fatal("provider file should not have been written")
	}
}
