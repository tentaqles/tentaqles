package cred

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/dotenv"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/secret"
)

// Chain is the resolver of spec B §7. It tries the sources from req.Preferred (cli when unset) on,
// in the fixed order cli, keychain, env, mcp:
//
//   - cli: the provider's registered CLIHook (RegisterCLI), run with the identity environment;
//   - keychain: the OS keychain, service "tentaqles", account "<ws>/<provider>" (tq secret set);
//   - env: the manifest's tracker.env, resolved as tq dotenv run does: the identity environment,
//     then the process environment, then the workspace root's .env;
//   - mcp: nothing to fetch in the background, so Resolve fails with ErrNeedsSession.
//
// Resolve checks that a source has a credential now; the Transport it returns fetches a fresh one
// from that same source for every request. Keychain and env tokens arrive as Token{Value: raw}
// (Bearer); an adapter whose tracker wants another scheme (Basic for an ADO PAT or a Jira API
// token) wraps the Transport with New. No value is ever logged, printed or kept.
//
// The zero Chain uses the real hook registry, keychain and workspace registry; tests set the
// fields.
type Chain struct {
	// CLI finds a provider's cli hook (nil = CLIHookFor).
	CLI func(provider string) (CLIHook, bool)
	// Keychain reads a stored token (nil = secret.Get).
	Keychain func(workspace, provider string) (string, error)
	// Root is a workspace's root folder, where its .env sits (nil = the registered workspace).
	Root func(workspace string) (string, error)
}

// NewChain is the production chain.
func NewChain() Chain { return Chain{} }

func (c Chain) cliHook(provider string) (CLIHook, bool) {
	if c.CLI != nil {
		return c.CLI(provider)
	}
	return CLIHookFor(provider)
}

func (c Chain) keychain(ws, provider string) (string, error) {
	get := c.Keychain
	if get == nil {
		get = secret.Get
	}
	v, err := get(ws, provider)
	if errors.Is(err, secret.ErrNotFound) {
		return "", ErrNoCredential
	}
	if err == nil && v == "" {
		return "", ErrNoCredential
	}
	return v, err
}

func (c Chain) root(ws string) (string, error) {
	if c.Root != nil {
		return c.Root(ws)
	}
	return defaultRoot(ws)
}

// defaultRoot is the registered workspace's root folder.
func defaultRoot(ws string) (string, error) {
	cfg, err := registry.Load()
	if err != nil {
		return "", err
	}
	all, _ := resolve.ListWorkspaces(cfg)
	for _, w := range all {
		if w.Name == ws {
			return w.Root, nil
		}
	}
	return "", fmt.Errorf("no registered workspace %q", ws)
}

// lookupEnv resolves the variable name as tq dotenv run would see it, never printing it.
func (c Chain) lookupEnv(req Request) (string, error) {
	name := strings.TrimSpace(req.EnvVar)
	if name == "" {
		return "", fmt.Errorf("%w: the manifest names no tracker.env", ErrNoCredential)
	}
	if !dotenv.ValidKey(name) {
		return "", fmt.Errorf("tracker.env %q is not a variable name", name)
	}
	v := ""
	for _, kv := range req.Env {
		if k, val, ok := strings.Cut(kv, "="); ok && k == name {
			v = val // last one wins, as in an environment
		}
	}
	if v != "" {
		return v, nil
	}
	if val, ok := os.LookupEnv(name); ok && val != "" {
		return val, nil
	}
	root, err := c.root(req.Workspace)
	if err != nil {
		return "", fmt.Errorf("%w: %s is unset and the workspace .env was not found (%v)", ErrNoCredential, name, err)
	}
	if v := dotenv.Lookup(filepath.Join(root, ".env"), name); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%w: %s is unset (environment and the workspace .env)", ErrNoCredential, name)
}

// fetcher is one source's fetch; Resolve calls it once to check, and the Transport per request.
func (c Chain) fetcher(src Source, req Request) (func(ctx context.Context) (Token, error), error) {
	switch src {
	case SourceCLI:
		h, ok := c.cliHook(req.Provider)
		if !ok {
			return nil, fmt.Errorf("%w: no CLI login support for %s", ErrNoCredential, req.Provider)
		}
		env := append([]string(nil), req.Env...)
		return func(ctx context.Context) (Token, error) { return h(ctx, env) }, nil
	case SourceKeychain:
		ws, p := req.Workspace, req.Provider
		return func(context.Context) (Token, error) {
			v, err := c.keychain(ws, p)
			if err != nil {
				return Token{}, err
			}
			return Token{Value: v}, nil
		}, nil
	case SourceEnv:
		r := req
		r.Env = append([]string(nil), req.Env...)
		return func(context.Context) (Token, error) {
			v, err := c.lookupEnv(r)
			if err != nil {
				return Token{}, err
			}
			return Token{Value: v}, nil
		}, nil
	}
	return nil, fmt.Errorf("unknown credential source %q", src)
}

// Resolve finds the first source, from the preferred one on, that has a credential now.
func (c Chain) Resolve(ctx context.Context, req Request) (Transport, error) {
	pref := req.Preferred
	if pref == "" {
		pref = SourceCLI
	}
	start := -1
	for i, s := range Order {
		if s == pref {
			start = i
		}
	}
	if start < 0 {
		return nil, &SourceError{Source: pref, Provider: req.Provider, Err: fmt.Errorf("unknown credential source %q: use cli, keychain, env or mcp", pref)}
	}
	var tried []error
	for _, src := range Order[start:] {
		if src == SourceMCP {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fetch, err := c.fetcher(src, req)
		if err == nil {
			_, err = fetch(ctx) // a check only: the value is dropped at once
		}
		if err != nil {
			tried = append(tried, &SourceError{Source: src, Err: err})
			continue
		}
		src, provider := src, req.Provider
		return New(src, func(ctx context.Context) (Token, error) {
			tok, err := fetch(ctx)
			if err != nil {
				return Token{}, &SourceError{Source: src, Provider: provider, Err: err}
			}
			return tok, nil
		}), nil
	}
	return nil, &SourceError{Source: SourceMCP, Provider: req.Provider, Err: &triedError{tried: tried}}
}

// triedError is needs-session plus why each earlier source had nothing. It names sources, never
// values.
type triedError struct{ tried []error }

func (e *triedError) Error() string {
	if len(e.tried) == 0 {
		return ErrNeedsSession.Error()
	}
	parts := make([]string, len(e.tried))
	for i, t := range e.tried {
		parts[i] = t.Error()
	}
	return ErrNeedsSession.Error() + " (tried " + strings.Join(parts, "; ") + ")"
}

func (e *triedError) Unwrap() []error { return append([]error{ErrNeedsSession}, e.tried...) }
