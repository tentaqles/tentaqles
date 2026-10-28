package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// maxResponse caps a GraphQL response body.
const maxResponse = 8 << 20

// requestTimeout bounds one GraphQL request on top of the caller's context.
const requestTimeout = 30 * time.Second

// authHeader is the Authorization value for a Linear token. A personal API key (lin_api_...) goes
// raw, with no scheme, as Linear expects; so does a token with no scheme set. An OAuth token
// carries its scheme (Bearer).
func authHeader(t cred.Token) string {
	if t.Scheme == "" || strings.HasPrefix(t.Value, "lin_api_") {
		return t.Value
	}
	return t.Header()
}

type gqlError struct {
	Message    string `json:"message"`
	Extensions struct {
		Code                   string `json:"code"`
		UserPresentableMessage string `json:"userPresentableMessage"`
	} `json:"extensions"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors"`
}

// transport resolves the credential: the configured one, else the default resolver's chain
// (keychain, then env; Linear has no CLI).
func (a *Adapter) transport(ctx context.Context) (cred.Transport, error) {
	if a.cfg.Transport != nil {
		return a.cfg.Transport, nil
	}
	return cred.Resolve(ctx, a.cfg.CredRequest())
}

// blocked is the rate-limit error when an earlier response said the budget is spent.
func (a *Adapter) blocked() error {
	a.mu.Lock()
	until := a.limitedUntil
	a.mu.Unlock()
	if now := a.now(); now.Before(until) {
		return &hubsync.RateLimitError{Provider: Name, RetryAfter: until.Sub(now)}
	}
	return nil
}

// resetAt reads X-RateLimit-<kind>-Reset (epoch milliseconds).
func resetAt(h http.Header, kind string) time.Time {
	v := strings.TrimSpace(h.Get("X-RateLimit-" + kind + "-Reset"))
	if v == "" {
		return time.Time{}
	}
	ms, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// exhaustedUntil is when a spent X-RateLimit budget (requests or complexity) resets; zero when
// neither is spent.
func exhaustedUntil(h http.Header) time.Time {
	var until time.Time
	for _, kind := range []string{"Requests", "Complexity", "Endpoint-Requests", "Endpoint-Complexity"} {
		if strings.TrimSpace(h.Get("X-RateLimit-"+kind+"-Remaining")) != "0" {
			continue
		}
		if r := resetAt(h, kind); r.After(until) {
			until = r
		}
	}
	return until
}

// retryAfter is how long the tracker asked us to wait: Retry-After seconds, else the latest
// X-RateLimit reset.
func (a *Adapter) retryAfter(h http.Header) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	var until time.Time
	for _, kind := range []string{"Requests", "Complexity", "Endpoint-Requests", "Endpoint-Complexity"} {
		if r := resetAt(h, kind); r.After(until) {
			until = r
		}
	}
	if d := until.Sub(a.now()); d > 0 {
		return d
	}
	return 0
}

func (a *Adapter) noteLimit(until time.Time) {
	a.mu.Lock()
	if until.After(a.limitedUntil) {
		a.limitedUntil = until
	}
	a.mu.Unlock()
}

func isRateLimited(errs []gqlError) bool {
	for _, e := range errs {
		if e.Extensions.Code == "RATELIMITED" {
			return true
		}
	}
	return false
}

func isNotFound(errs []gqlError) bool {
	for _, e := range errs {
		m := strings.ToLower(e.Message + " " + e.Extensions.UserPresentableMessage)
		if strings.Contains(m, "not found") || strings.Contains(m, "could not find") {
			return true
		}
	}
	return false
}

func errorText(errs []gqlError) string {
	var parts []string
	for _, e := range errs {
		m := e.Message
		if e.Extensions.UserPresentableMessage != "" && e.Extensions.UserPresentableMessage != m {
			m += " (" + e.Extensions.UserPresentableMessage + ")"
		}
		parts = append(parts, m)
		if len(parts) == 3 {
			break
		}
	}
	return strings.Join(parts, "; ")
}

// do runs one named GraphQL operation and decodes data into out. The token is fetched for this
// request only and never appears in an error.
func (a *Adapter) do(ctx context.Context, op, query string, vars map[string]any, out any) error {
	if err := a.blocked(); err != nil {
		return err
	}
	tr, err := a.transport(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"query": query, "operationName": op, "variables": vars})
	if err != nil {
		return fmt.Errorf("linear %s: %w", op, err)
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("linear %s: %w", op, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	tok, err := tr.Token(ctx)
	if err != nil {
		var se *cred.SourceError
		if errors.As(err, &se) {
			return err
		}
		return &cred.SourceError{Source: tr.Source(), Provider: Name, Err: err}
	}
	req.Header.Set("Authorization", authHeader(tok))
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("linear %s: %w", op, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return fmt.Errorf("linear %s: reading the response: %w", op, err)
	}
	if until := exhaustedUntil(resp.Header); !until.IsZero() {
		a.noteLimit(until)
	}
	var gr gqlResponse
	jsonErr := json.Unmarshal(raw, &gr)

	if resp.StatusCode == http.StatusTooManyRequests || (jsonErr == nil && isRateLimited(gr.Errors)) {
		d := a.retryAfter(resp.Header)
		if d > 0 {
			a.noteLimit(a.now().Add(d))
		}
		return &hubsync.RateLimitError{Provider: Name, RetryAfter: d}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		msg := http.StatusText(resp.StatusCode)
		if jsonErr == nil && len(gr.Errors) > 0 {
			msg = errorText(gr.Errors)
		}
		return &cred.SourceError{Source: tr.Source(), Provider: Name, Err: fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)}
	}
	if jsonErr != nil {
		if resp.StatusCode >= 300 {
			return fmt.Errorf("linear %s: HTTP %d", op, resp.StatusCode)
		}
		return fmt.Errorf("linear %s: bad response: %w", op, jsonErr)
	}
	if len(gr.Errors) > 0 {
		for _, e := range gr.Errors {
			if e.Extensions.Code == "AUTHENTICATION_ERROR" || e.Extensions.Code == "FORBIDDEN" {
				return &cred.SourceError{Source: tr.Source(), Provider: Name, Err: errors.New(errorText(gr.Errors))}
			}
		}
		if isNotFound(gr.Errors) {
			return fmt.Errorf("linear %s: %s: %w", op, errorText(gr.Errors), hubsync.ErrNotFound)
		}
		return fmt.Errorf("linear %s: %s", op, errorText(gr.Errors))
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("linear %s: HTTP %d", op, resp.StatusCode)
	}
	if out == nil || len(gr.Data) == 0 || string(gr.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(gr.Data, out); err != nil {
		return fmt.Errorf("linear %s: decoding data: %w", op, err)
	}
	return nil
}
