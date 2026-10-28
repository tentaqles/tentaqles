package azdo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// TestMain doubles as the fake `az` CLI: the test binary copied onto PATH as az(.exe).
func TestMain(m *testing.M) {
	if strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe") == "az" {
		os.Exit(fakeAz(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeAz(args []string) int {
	want := []string{"account", "get-access-token", "--resource", Resource, "--output", "json"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		fmt.Fprintf(os.Stderr, "unexpected args: %q\n", args)
		return 2
	}
	dir := os.Getenv("AZURE_CONFIG_DIR")
	if dir == "" || os.Getenv("FAKE_AZ_LOGGED_OUT") == "1" {
		fmt.Fprintln(os.Stderr, "ERROR: Please run 'az login' to setup account.")
		return 1
	}
	// Every run is counted in the config dir, so tests can see how often az ran.
	if err := os.MkdirAll(dir, 0o755); err == nil {
		if fh, err := os.OpenFile(filepath.Join(dir, "calls"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(fh, "run")
			fh.Close()
		}
	}
	exp := time.Now().Add(time.Hour)
	if v := os.Getenv("FAKE_AZ_EXPIRES_IN"); v != "" {
		d, _ := time.ParseDuration(v)
		exp = time.Now().Add(d)
	}
	out := map[string]any{"accessToken": "tok-" + filepath.Base(dir), "tokenType": "Bearer",
		"expiresOn": exp.Local().Format("2006-01-02 15:04:05.000000")}
	if os.Getenv("FAKE_AZ_OLD") != "1" {
		out["expires_on"] = exp.Unix()
	}
	json.NewEncoder(os.Stdout).Encode(out)
	return 0
}

// azRuns is how many times the fake az ran under config dir dir.
func azRuns(t *testing.T, dir string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "run")
}

// withoutAzureDir is the process environment minus any AZURE_CONFIG_DIR (the "ambient login").
func withoutAzureDir() []string {
	var out []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "AZURE_CONFIG_DIR=") {
			out = append(out, kv)
		}
	}
	return out
}

// installFakeAz copies the test binary onto a fresh dir prepended to PATH, named az.
func installFakeAz(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	name := "az"
	if runtime.GOOS == "windows" {
		name = "az.exe"
	}
	src, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := lookPath
	lookPath = func(file string) (string, error) {
		if file != "az" {
			return "", exec.ErrNotFound
		}
		return filepath.Join(dir, name), nil
	}
	t.Cleanup(func() { lookPath = old })
}

func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	for _, k := range []string{"TQ_HOME", "HOME", "USERPROFILE"} {
		t.Setenv(k, home)
	}
}

func TestAzTokenUsesTheIdentityConfigDir(t *testing.T) {
	isolate(t)
	installFakeAz(t)
	env := append(os.Environ(), "AZURE_CONFIG_DIR="+filepath.Join(t.TempDir(), "acme"))
	tok, err := AzToken(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Value != "tok-acme" || tok.Header() != "Bearer tok-acme" {
		t.Fatalf("token %q header %q", tok.Value, tok.Header())
	}
}

func TestAzTokenLoggedOutIsNoCredential(t *testing.T) {
	isolate(t)
	installFakeAz(t)
	env := append(os.Environ(), "AZURE_CONFIG_DIR="+filepath.Join(t.TempDir(), "acme"), "FAKE_AZ_LOGGED_OUT=1")
	_, err := AzToken(context.Background(), env)
	if !errors.Is(err, cred.ErrNoCredential) {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(err.Error(), "az login") {
		t.Fatalf("the error should say to run az login: %v", err)
	}
}

func TestAzTokenMissingCLIIsNoCredential(t *testing.T) {
	isolate(t)
	old := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookPath = old })
	env := []string{"AZURE_CONFIG_DIR=" + filepath.Join(t.TempDir(), "acme")}
	if _, err := AzToken(context.Background(), env); !errors.Is(err, cred.ErrNoCredential) || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err %v", err)
	}
}

func TestAzHookRegistered(t *testing.T) {
	if _, ok := cred.CLIHookFor(Provider); !ok {
		t.Fatal("the azure-devops cli hook is not registered")
	}
}
