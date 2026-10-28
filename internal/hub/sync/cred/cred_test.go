package cred

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSourcesAndOrder(t *testing.T) {
	if fmt.Sprint(Order) != "[cli keychain env mcp]" {
		t.Fatalf("order %v", Order)
	}
	for _, s := range []string{"cli", "keychain", "env", "mcp"} {
		if got, err := ParseSource(s); err != nil || string(got) != s {
			t.Fatalf("%s: %v %v", s, got, err)
		}
	}
	if _, err := ParseSource("password"); err == nil {
		t.Fatal("unknown source accepted")
	}
	if KeychainService != "tentaqles" || KeychainAccount("acme", "jira") != "acme/jira" {
		t.Fatal(KeychainAccount("acme", "jira"))
	}
}

func TestTokenNeverPrints(t *testing.T) {
	tok := Token{Scheme: "Bearer", Value: "s3cr3t-value"}
	for _, s := range []string{fmt.Sprint(tok), fmt.Sprintf("%v %+v %#v %s", tok, tok, tok, tok)} {
		if strings.Contains(s, "s3cr3t") {
			t.Fatalf("token printed: %s", s)
		}
	}
	if tok.Header() != "Bearer s3cr3t-value" {
		t.Fatal("header")
	}
	if (Token{Value: "x"}).Header() != "Bearer x" {
		t.Fatal("default scheme is Bearer")
	}
}

func TestClientSetsAuthorizationPerRequest(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
	}))
	defer srv.Close()
	n := 0
	tr := New(SourceKeychain, func(ctx context.Context) (Token, error) {
		n++
		return Token{Scheme: "Basic", Value: fmt.Sprintf("t%d", n)}, nil
	})
	if tr.Source() != SourceKeychain {
		t.Fatal(tr.Source())
	}
	c := Client(tr, nil)
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("GET", srv.URL, nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if req.Header.Get("Authorization") != "" {
			t.Fatal("the caller's request was modified")
		}
	}
	if fmt.Sprint(got) != "[Basic t1 Basic t2]" {
		t.Fatalf("headers %v", got)
	}
	bad := New(SourceEnv, func(ctx context.Context) (Token, error) { return Token{}, errors.New("unset") })
	req, _ := http.NewRequest("GET", srv.URL, nil)
	_, err := Client(bad, nil).Do(req)
	var se *SourceError
	if !errors.As(err, &se) || se.Source != SourceEnv {
		t.Fatalf("want a SourceError naming env, got %v", err)
	}
}

func TestTokenNeverMarshalsOrLogs(t *testing.T) {
	tok := Token{Scheme: "Bearer", Value: "s3cr3t-value"}
	b, err := json.Marshal(struct {
		Tok  Token
		Ptr  *Token
		List []Token
	}{tok, &tok, []Token{tok}})
	if err != nil || strings.Contains(string(b), "s3cr3t") {
		t.Fatalf("json: %s %v", b, err)
	}
	if !strings.Contains(string(b), `"[token]"`) {
		t.Fatalf("json shape: %s", b)
	}
	var buf bytes.Buffer
	for _, h := range []slog.Handler{slog.NewTextHandler(&buf, nil), slog.NewJSONHandler(&buf, nil)} {
		slog.New(h).Info("x", "tok", tok, "ptr", &tok, slog.Any("any", tok))
	}
	if strings.Contains(buf.String(), "s3cr3t") || !strings.Contains(buf.String(), "[redacted]") {
		t.Fatalf("slog: %s", buf.String())
	}
}

func TestClientNeverSendsTheTokenAcrossOrigins(t *testing.T) {
	var seenB []string
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenB = append(seenB, r.Header.Get("Authorization"))
	}))
	defer b.Close()
	var seenA []string
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenA = append(seenA, r.URL.Path+"="+r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, b.URL+"/stolen", http.StatusFound)
		case "/local":
			http.Redirect(w, r, "/landed", http.StatusTemporaryRedirect)
		}
	}))
	defer a.Close()
	fetches := 0
	c := Client(New(SourceCLI, func(context.Context) (Token, error) {
		fetches++
		return Token{Value: "s3cr3t"}, nil
	}), nil)
	for _, p := range []string{"/away", "/local"} {
		req, _ := http.NewRequest("GET", a.URL+p, nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(p, err)
		}
		resp.Body.Close()
	}
	if len(seenB) != 1 || seenB[0] != "" {
		t.Fatalf("the other origin saw %q", seenB)
	}
	want := "[/away=Bearer s3cr3t /local=Bearer s3cr3t /landed=Bearer s3cr3t]"
	if fmt.Sprint(seenA) != want {
		t.Fatalf("origin A saw %v, want %s", seenA, want)
	}
	if fetches != 3 {
		t.Fatalf("token fetched %d times; a cross-origin hop must not fetch one", fetches)
	}
	// A caller that sets its own Authorization does not get it carried across either.
	req, _ := http.NewRequest("GET", a.URL+"/away", nil)
	req.Header.Set("Authorization", "Bearer caller-set")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(seenB) != 2 || seenB[1] != "" {
		t.Fatalf("the other origin saw %q", seenB)
	}
}

func TestStatic(t *testing.T) {
	tr := Static(SourceEnv, Token{Value: "v"})
	tok, err := tr.Token(context.Background())
	if err != nil || tok.Value != "v" || tr.Source() != SourceEnv {
		t.Fatal(tok, err)
	}
}

func TestDefaultResolverIsNoopNeedsSession(t *testing.T) {
	_, err := Resolve(context.Background(), Request{Workspace: "acme", Provider: "jira"})
	var se *SourceError
	if !errors.Is(err, ErrNeedsSession) || !errors.As(err, &se) || se.Source != SourceMCP {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(err.Error(), "mcp") || !strings.Contains(err.Error(), "jira") {
		t.Fatalf("error text %q", err)
	}
	restore := SetDefault(ResolverFunc(func(ctx context.Context, r Request) (Transport, error) {
		return Static(SourceCLI, Token{Value: "x"}), nil
	}))
	tr, err := Resolve(context.Background(), Request{Workspace: "acme", Provider: "github"})
	if err != nil || tr.Source() != SourceCLI {
		t.Fatal(err)
	}
	restore()
	if _, err := Resolve(context.Background(), Request{Provider: "github"}); !errors.Is(err, ErrNeedsSession) {
		t.Fatalf("restore: %v", err)
	}
}

func TestCLIHookRegistry(t *testing.T) {
	t.Cleanup(func() { unregisterCLI("acme-tracker") })
	if _, ok := CLIHookFor("acme-tracker"); ok {
		t.Fatal("found before register")
	}
	RegisterCLI("acme-tracker", func(ctx context.Context, env []string) (Token, error) {
		return Token{Value: strings.Join(env, ",")}, nil
	})
	h, ok := CLIHookFor("acme-tracker")
	if !ok {
		t.Fatal("not found")
	}
	tok, err := h(context.Background(), []string{"GH_CONFIG_DIR=x"})
	if err != nil || tok.Value != "GH_CONFIG_DIR=x" {
		t.Fatal(tok, err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a second registration did not panic")
		}
	}()
	RegisterCLI("acme-tracker", func(ctx context.Context, env []string) (Token, error) { return Token{}, nil })
}

func TestSourceErrorNeverHoldsTheToken(t *testing.T) {
	err := &SourceError{Source: SourceKeychain, Provider: "jira", Err: ErrNoCredential}
	if err.Error() != "jira credential from keychain: no credential" || !errors.Is(err, ErrNoCredential) {
		t.Fatal(err.Error())
	}
}
