// Package cred is how a tracker adapter gets its credential (spec B §7). A Resolver tries the
// sources in a fixed order: the tracker CLI's own login, the OS keychain, an env var resolved via
// tq dotenv, and finally the agent's MCP connector (which only a Claude session can use, so the
// engine reports needs-session).
//
// This package holds the interfaces and the registry only. The resolver chain and the keychain
// live in their own workstream; until one is installed with SetDefault, Resolve reports
// needs-session.
//
// A token is held in memory for one request only. It is never written to a file, a log, activity,
// a comment or a prompt, and errors name the source that failed, never the token.
package cred

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Source is where a credential comes from.
type Source string

const (
	SourceCLI      Source = "cli"
	SourceKeychain Source = "keychain"
	SourceEnv      Source = "env"
	SourceMCP      Source = "mcp"
)

// Order is the fixed fallback order.
var Order = []Source{SourceCLI, SourceKeychain, SourceEnv, SourceMCP}

// ParseSource reads a manifest tracker.auth value.
func ParseSource(s string) (Source, error) {
	for _, o := range Order {
		if string(o) == s {
			return o, nil
		}
	}
	return "", fmt.Errorf("credential source %q: use cli, keychain, env or mcp", s)
}

// KeychainService is the OS keychain service every tq tracker token is stored under.
const KeychainService = "tentaqles"

// KeychainAccount is the keychain account of a workspace's provider token: "<ws>/<provider>".
func KeychainAccount(workspace, provider string) string { return workspace + "/" + provider }

var (
	// ErrNeedsSession means only the MCP connector is left: a Claude session (/tentaqles:sync)
	// has to do the work.
	ErrNeedsSession = errors.New("needs-session: no background credential; use /tentaqles:sync from a Claude session")
	// ErrNoCredential means a source has nothing for this provider (not logged in, no entry, unset).
	ErrNoCredential = errors.New("no credential")
)

// SourceError says which source failed for which provider. It never carries a token.
type SourceError struct {
	Source   Source
	Provider string
	Err      error
}

func (e *SourceError) Error() string {
	if e.Provider == "" {
		return fmt.Sprintf("credential from %s: %v", e.Source, e.Err)
	}
	return fmt.Sprintf("%s credential from %s: %v", e.Provider, e.Source, e.Err)
}

func (e *SourceError) Unwrap() error { return e.Err }

// Token is one credential, for one request. It never prints its value.
type Token struct {
	Scheme string // "Bearer" (default), "Basic", "token"
	Value  string
}

// Header is the Authorization header value.
func (t Token) Header() string {
	s := t.Scheme
	if s == "" {
		s = "Bearer"
	}
	return s + " " + t.Value
}

func (t Token) String() string   { return "[token]" }
func (t Token) GoString() string { return "cred.Token{[redacted]}" }

// Format keeps %v, %+v, %#v and %s from printing the value.
func (t Token) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('#') {
		fmt.Fprint(f, t.GoString())
		return
	}
	fmt.Fprint(f, t.String())
}

// Transport hands an adapter a fresh token for each request. Callers use it at once and keep
// nothing.
type Transport interface {
	Source() Source
	Token(ctx context.Context) (Token, error)
}

type funcTransport struct {
	src   Source
	fetch func(ctx context.Context) (Token, error)
}

func (t funcTransport) Source() Source { return t.src }

func (t funcTransport) Token(ctx context.Context) (Token, error) { return t.fetch(ctx) }

// New is a Transport that calls fetch for every request.
func New(src Source, fetch func(ctx context.Context) (Token, error)) Transport {
	return funcTransport{src: src, fetch: fetch}
}

// Static is a Transport with a fixed token (tests, and sources read once per request).
func Static(src Source, tok Token) Transport {
	return New(src, func(context.Context) (Token, error) { return tok, nil })
}

type authRoundTripper struct {
	t    Transport
	base http.RoundTripper
}

func (a authRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	tok, err := a.t.Token(r.Context())
	if err != nil {
		if r.Body != nil {
			r.Body.Close()
		}
		var se *SourceError
		if errors.As(err, &se) {
			return nil, err
		}
		return nil, &SourceError{Source: a.t.Source(), Err: err}
	}
	r2 := r.Clone(r.Context())
	r2.Header.Set("Authorization", tok.Header())
	return a.base.RoundTrip(r2)
}

// Client is an *http.Client that sets each request's Authorization header from t. base nil means
// http.DefaultTransport.
func Client(t Transport, base http.RoundTripper) *http.Client {
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{Transport: authRoundTripper{t: t, base: base}}
}

// Request names the credential wanted.
type Request struct {
	Workspace string
	Provider  string   // a tracker id: github, azure-devops, jira, linear, asana
	Preferred Source   // the manifest's tracker.auth ("" = cli); the order after it stays fixed
	EnvVar    string   // the manifest's tracker.env: the variable tq dotenv resolves
	Env       []string // the workspace identity environment (GH_CONFIG_DIR, AZURE_CONFIG_DIR, ...) for CLI hooks
}

// Resolver finds a Transport for a request, trying the sources in order.
type Resolver interface {
	Resolve(ctx context.Context, req Request) (Transport, error)
}

type ResolverFunc func(ctx context.Context, req Request) (Transport, error)

func (f ResolverFunc) Resolve(ctx context.Context, req Request) (Transport, error) {
	return f(ctx, req)
}

// NoopResolver finds nothing: every request needs a Claude session.
type NoopResolver struct{}

func (NoopResolver) Resolve(_ context.Context, req Request) (Transport, error) {
	return nil, &SourceError{Source: SourceMCP, Provider: req.Provider, Err: ErrNeedsSession}
}

var (
	defMu sync.RWMutex
	def   Resolver = NoopResolver{}
)

// Default is the installed resolver (NoopResolver until SetDefault).
func Default() Resolver {
	defMu.RLock()
	defer defMu.RUnlock()
	return def
}

// SetDefault installs r (nil = NoopResolver) and returns a func restoring the previous one.
func SetDefault(r Resolver) (restore func()) {
	if r == nil {
		r = NoopResolver{}
	}
	defMu.Lock()
	prev := def
	def = r
	defMu.Unlock()
	return func() {
		defMu.Lock()
		def = prev
		defMu.Unlock()
	}
}

// Resolve asks the default resolver.
func Resolve(ctx context.Context, req Request) (Transport, error) { return Default().Resolve(ctx, req) }

// CLIHook gets a token from a tracker CLI's own login (gh auth token, az account
// get-access-token, acli), run with env (the workspace identity environment). It returns
// ErrNoCredential when the CLI is missing or logged out.
type CLIHook func(ctx context.Context, env []string) (Token, error)

var (
	hookMu sync.RWMutex
	hooks  = map[string]CLIHook{}
)

// RegisterCLI registers the cli source for a provider, normally from an adapter package's init.
// It panics on an empty name, a nil hook or a second registration.
func RegisterCLI(provider string, h CLIHook) {
	provider = strings.TrimSpace(provider)
	if provider == "" || h == nil {
		panic("cred: RegisterCLI needs a provider and a hook")
	}
	hookMu.Lock()
	defer hookMu.Unlock()
	if _, dup := hooks[provider]; dup {
		panic("cred: RegisterCLI called twice for " + provider)
	}
	hooks[provider] = h
}

// CLIHookFor is the provider's cli hook, if one is registered.
func CLIHookFor(provider string) (CLIHook, bool) {
	hookMu.RLock()
	defer hookMu.RUnlock()
	h, ok := hooks[provider]
	return h, ok
}

func unregisterCLI(provider string) {
	hookMu.Lock()
	defer hookMu.Unlock()
	delete(hooks, provider)
}
