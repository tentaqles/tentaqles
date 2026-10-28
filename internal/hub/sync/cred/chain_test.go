package cred

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/secret"
)

func chainValue(tag string) string { return tag + strings.Repeat("Vw3", 8) }

// chainHome isolates TQ_HOME, HOME and USERPROFILE, and the keychain is go-keyring's mock.
func chainHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	secret.MockInit()
	return home
}

func registerTestCLI(t *testing.T, provider string, h CLIHook) {
	t.Helper()
	RegisterCLI(provider, h)
	t.Cleanup(func() { unregisterCLI(provider) })
}

func tokenOf(t *testing.T, tr Transport) Token {
	t.Helper()
	tok, err := tr.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestChainTriesCLIThenKeychainThenEnvThenMCP(t *testing.T) {
	chainHome(t)
	root := t.TempDir()
	ch := Chain{Root: func(ws string) (string, error) { return root, nil }}
	req := Request{Workspace: "acme", Provider: "github", EnvVar: "ACME_GH_TOKEN_T1"}

	// Nothing anywhere: needs-session, and the error names every source tried.
	_, err := ch.Resolve(context.Background(), req)
	var se *SourceError
	if !errors.Is(err, ErrNeedsSession) || !errors.As(err, &se) || se.Source != SourceMCP {
		t.Fatalf("%v", err)
	}
	for _, s := range []string{"cli", "keychain", "env", "github"} {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("error %q does not name %s", err, s)
		}
	}

	// env: the manifest's variable in the workspace .env.
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("ACME_GH_TOKEN_T1="+chainValue("e")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tr, err := ch.Resolve(context.Background(), req)
	if err != nil || tr.Source() != SourceEnv || tokenOf(t, tr).Value != chainValue("e") {
		t.Fatalf("env: %v", err)
	}

	// keychain beats env.
	if err := secret.Set("acme", "github", chainValue("k")); err != nil {
		t.Fatal(err)
	}
	tr, err = ch.Resolve(context.Background(), req)
	if err != nil || tr.Source() != SourceKeychain || tokenOf(t, tr).Value != chainValue("k") {
		t.Fatalf("keychain: %v", err)
	}

	// cli beats both, and the hook gets the identity environment.
	var gotEnv []string
	registerTestCLI(t, "github", func(ctx context.Context, env []string) (Token, error) {
		gotEnv = env
		return Token{Scheme: "token", Value: chainValue("c")}, nil
	})
	req.Env = []string{"GH_CONFIG_DIR=/x/gh"}
	tr, err = ch.Resolve(context.Background(), req)
	if err != nil || tr.Source() != SourceCLI {
		t.Fatalf("cli: %v", err)
	}
	if tok := tokenOf(t, tr); tok.Header() != "token "+chainValue("c") {
		t.Fatal("cli token scheme")
	}
	if fmt.Sprint(gotEnv) != "[GH_CONFIG_DIR=/x/gh]" {
		t.Fatalf("hook env %v", gotEnv)
	}
}

func TestChainPreferredSkipsEarlierSources(t *testing.T) {
	chainHome(t)
	registerTestCLI(t, "jira", func(ctx context.Context, env []string) (Token, error) {
		return Token{Value: chainValue("c")}, nil
	})
	if err := secret.Set("acme", "jira", chainValue("k")); err != nil {
		t.Fatal(err)
	}
	ch := Chain{Root: func(string) (string, error) { return t.TempDir(), nil }}
	tr, err := ch.Resolve(context.Background(), Request{Workspace: "acme", Provider: "jira", Preferred: SourceKeychain})
	if err != nil || tr.Source() != SourceKeychain {
		t.Fatalf("auth keychain: %v %v", tr, err)
	}
	// auth env with nothing set falls to mcp, never back to cli or keychain.
	_, err = ch.Resolve(context.Background(), Request{Workspace: "acme", Provider: "jira", Preferred: SourceEnv, EnvVar: "ACME_JIRA_TOKEN_T2"})
	if !errors.Is(err, ErrNeedsSession) {
		t.Fatalf("auth env: %v", err)
	}
	_, err = ch.Resolve(context.Background(), Request{Workspace: "acme", Provider: "jira", Preferred: SourceMCP})
	if !errors.Is(err, ErrNeedsSession) {
		t.Fatalf("auth mcp: %v", err)
	}
	if _, err := ch.Resolve(context.Background(), Request{Workspace: "acme", Provider: "jira", Preferred: "password"}); err == nil || errors.Is(err, ErrNeedsSession) {
		t.Fatalf("unknown source: %v", err)
	}
}

func TestChainEnvOrder(t *testing.T) {
	chainHome(t)
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".env"), []byte("ACME_LIN_TOKEN_T3="+chainValue("file")+"\n"), 0o600)
	ch := Chain{Root: func(string) (string, error) { return root, nil }}
	req := Request{Workspace: "acme", Provider: "linear", Preferred: SourceEnv, EnvVar: "ACME_LIN_TOKEN_T3"}
	tr, err := ch.Resolve(context.Background(), req)
	if err != nil || tokenOf(t, tr).Value != chainValue("file") {
		t.Fatalf("file: %v", err)
	}
	// A variable already in the environment wins over the file, as with tq dotenv run.
	t.Setenv("ACME_LIN_TOKEN_T3", chainValue("proc"))
	if tok := tokenOf(t, tr); tok.Value != chainValue("proc") {
		t.Fatal("the transport re-reads per request; the process env wins")
	}
	// The identity environment wins over both.
	req.Env = []string{"ACME_LIN_TOKEN_T3=" + chainValue("ident")}
	tr, _ = ch.Resolve(context.Background(), req)
	if tokenOf(t, tr).Value != chainValue("ident") {
		t.Fatal("identity env")
	}
	// No tracker.env: the env source has nothing.
	_, err = ch.Resolve(context.Background(), Request{Workspace: "acme", Provider: "linear", Preferred: SourceEnv})
	if !errors.Is(err, ErrNeedsSession) || !strings.Contains(err.Error(), "tracker.env") {
		t.Fatalf("no env name: %v", err)
	}
}

func TestChainDefaultRootFindsTheRegisteredWorkspace(t *testing.T) {
	chainHome(t)
	if _, err := defaultRoot("acme"); err == nil {
		t.Fatal("an unregistered workspace has a root")
	}
	base := t.TempDir()
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "acme")
	os.MkdirAll(root, 0o755)
	if err := os.WriteFile(filepath.Join(root, manifest.FileName), []byte("schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {} }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, ".env"), []byte("ACME_GH_TOKEN_T5="+chainValue("e")+"\n"), 0o600)
	tr, err := NewChain().Resolve(context.Background(), Request{Workspace: "acme", Provider: "github", Preferred: SourceEnv, EnvVar: "ACME_GH_TOKEN_T5"})
	if err != nil || tokenOf(t, tr).Value != chainValue("e") {
		t.Fatalf("the registered workspace .env: %v", err)
	}
}

func TestChainKeychainFailureIsReportedNotTheValue(t *testing.T) {
	chainHome(t)
	if err := secret.Set("acme", "asana", chainValue("k")); err != nil {
		t.Fatal(err)
	}
	ch := Chain{Root: func(string) (string, error) { return t.TempDir(), nil }}
	tr, err := ch.Resolve(context.Background(), Request{Workspace: "acme", Provider: "asana"})
	if err != nil {
		t.Fatal(err)
	}
	// The entry is removed after resolve: the next request fails, naming the keychain.
	secret.Delete("acme", "asana")
	_, err = tr.Token(context.Background())
	var se *SourceError
	if !errors.As(err, &se) || se.Source != SourceKeychain || !errors.Is(err, ErrNoCredential) {
		t.Fatalf("%v", err)
	}
	secret.MockInitWithError(errors.New("keychain locked"))
	t.Cleanup(secret.MockInit)
	_, err = ch.Resolve(context.Background(), Request{Workspace: "acme", Provider: "asana"})
	if !errors.Is(err, ErrNeedsSession) || !strings.Contains(err.Error(), "keychain locked") {
		t.Fatalf("%v", err)
	}
}

// TestChainNeverPrintsAValue: no resolve outcome, error or log line carries a token.
func TestChainNeverPrintsAValue(t *testing.T) {
	chainHome(t)
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".env"), []byte("ACME_ADO_TOKEN_T4="+chainValue("e")+"\n"), 0o600)
	secret.Set("acme", "azure-devops", chainValue("k"))
	registerTestCLI(t, "azure-devops", func(ctx context.Context, env []string) (Token, error) {
		return Token{}, fmt.Errorf("az: %w", ErrNoCredential)
	})
	ch := Chain{Root: func(string) (string, error) { return root, nil }}
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	for _, p := range []Source{"", SourceKeychain, SourceEnv, SourceMCP} {
		tr, err := ch.Resolve(context.Background(), Request{Workspace: "acme", Provider: "azure-devops", Preferred: p, EnvVar: "ACME_ADO_TOKEN_T4"})
		log.Info("resolve", "transport", tr, "err", err)
		fmt.Fprintf(&buf, "%v %+v %#v\n", tr, tr, err)
		if tr != nil {
			tok, _ := tr.Token(context.Background())
			log.Info("tok", "tok", tok)
			fmt.Fprintf(&buf, "%v %+v %#v %s\n", tok, tok, tok, tok)
		}
	}
	for _, v := range []string{chainValue("e"), chainValue("k")} {
		if strings.Contains(buf.String(), v) {
			t.Fatalf("a value was printed:\n%s", buf.String())
		}
	}
}

func TestChainCanBeTheDefault(t *testing.T) {
	chainHome(t)
	if KeychainService != secret.Service || KeychainAccount("acme", "jira") != secret.Account("acme", "jira") {
		t.Fatal("cred and secret name the keychain entry differently")
	}
	restore := SetDefault(Chain{Root: func(string) (string, error) { return t.TempDir(), nil }})
	defer restore()
	secret.Set("acme", "github", chainValue("k"))
	tr, err := Resolve(context.Background(), Request{Workspace: "acme", Provider: "github"})
	if err != nil || tr.Source() != SourceKeychain {
		t.Fatal(err)
	}
}
