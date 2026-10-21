package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

func runRedact(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"redact"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestRedact_StdinDefaultAll(t *testing.T) {
	isolateHome(t)
	out, err := runRedact(t, "cpf 529.982.247-25 card 4111 1111 1111 1111\n")
	if err != nil || strings.Contains(out, "529") || !strings.Contains(out, "[REDACTED:cpf]") || !strings.Contains(out, "[REDACTED:card]") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestRedact_ClassesAndCount(t *testing.T) {
	isolateHome(t)
	out, _ := runRedact(t, "cpf 529.982.247-25 card 4111 1111 1111 1111\n", "--classes", "financial")
	if !strings.Contains(out, "529.982.247-25") || !strings.Contains(out, "[REDACTED:card]") {
		t.Fatalf("%q", out)
	}
	out, _ = runRedact(t, "cpf 529.982.247-25\n", "--count")
	if strings.Contains(out, "529") || !strings.Contains(out, "cpf: 1") {
		t.Fatalf("%q", out)
	}
	if _, err := runRedact(t, "x", "--classes", "bogus"); err == nil {
		t.Fatal("unknown class accepted")
	}
}

func TestRedact_FileAndRun(t *testing.T) {
	isolateHome(t)
	dir := testutil.TempDir(t)
	f := filepath.Join(dir, "export.csv")
	if err := os.WriteFile(f, []byte("ana@x.io,529.982.247-25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runRedact(t, "", "--file", f)
	if err != nil || strings.Contains(out, "ana@") {
		t.Fatalf("%q %v", out, err)
	}
	sh, flag := "sh", "-c"
	if runtime.GOOS == "windows" {
		sh, flag = "cmd", "/c"
	}
	out, err = runRedact(t, "", "run", "--classes", "pii", "--", sh, flag, "echo mail ana@x.io")
	if err != nil || strings.Contains(out, "ana@") || !strings.Contains(out, "[REDACTED:email]") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestRedact_DefaultClassesFromManifest(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "data:\n  classes: [financial]\n  prompts: { allow: [] }\n")
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(ws); err != nil {
		t.Fatal(err)
	}
	out, _ := runRedact(t, "cpf 529.982.247-25 card 4111 1111 1111 1111 key ghp_abcdefghijklmnopqrstuvwxyz0123\n")
	// financial from the manifest, secrets always; pii is not declared, so the CPF stays
	if !strings.Contains(out, "529.982.247-25") || !strings.Contains(out, "[REDACTED:card]") || !strings.Contains(out, "[REDACTED:github_token]") {
		t.Fatalf("%q", out)
	}
}
