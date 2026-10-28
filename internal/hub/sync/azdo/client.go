package azdo

import (
	"bytes"
	"context"
	"encoding/base64"
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

const (
	apiVersion         = "7.1"
	commentsAPIVersion = "7.1-preview.4"
	jsonPatch          = "application/json-patch+json"
	maxBody            = 16 << 20
)

// patTransport sends a personal access token (from the keychain or env) the way Azure DevOps
// wants it: Basic with an empty user. Entra tokens (JWTs, and everything az prints) stay Bearer.
type patTransport struct{ cred.Transport }

func (p patTransport) Token(ctx context.Context) (cred.Token, error) {
	tok, err := p.Transport.Token(ctx)
	if err != nil || p.Source() == cred.SourceCLI {
		return tok, err
	}
	if (tok.Scheme == "" || strings.EqualFold(tok.Scheme, "Bearer")) && !looksJWT(tok.Value) {
		return cred.Token{Scheme: "Basic", Value: base64.StdEncoding.EncodeToString([]byte(":" + tok.Value))}, nil
	}
	return tok, nil
}

func looksJWT(v string) bool { return strings.HasPrefix(v, "eyJ") && strings.Count(v, ".") == 2 }

// httpClient resolves the credential on first use (cfg.Transport, else the cred chain). A
// resolve failure is not cached: the next call tries again.
func (a *Adapter) httpClient(ctx context.Context) (*http.Client, cred.Source, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil {
		return a.client, a.src, nil
	}
	t := a.cfg.Transport
	if t == nil {
		var err error
		if t, err = cred.Resolve(ctx, a.cfg.CredRequest()); err != nil {
			return nil, "", err
		}
		if t == nil {
			return nil, "", &cred.SourceError{Source: cred.SourceMCP, Provider: Provider, Err: cred.ErrNeedsSession}
		}
	}
	a.client, a.src = cred.Client(patTransport{t}, a.rt), t.Source()
	return a.client, a.src, nil
}

// blockedFor is how long the tracker asked us to wait before the next call (0 = go).
func (a *Adapter) blockedFor() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if d := a.blocked.Sub(a.now()); d > 0 {
		return d
	}
	return 0
}

func (a *Adapter) blockUntil(t time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t.After(a.blocked) {
		a.blocked = t
	}
}

// noteLimits reads Azure DevOps throttling headers off any response: Retry-After (sent on 429,
// and on a 200 once requests are being delayed), an exhausted X-RateLimit-Remaining with its
// X-RateLimit-Reset (epoch seconds), and X-RateLimit-Delay (seconds the tracker held this
// request). The first two hold back the next call until the time given; the delay holds it back
// as long again, at most shortWait, so it is always waited out, never turned into an error.
func (a *Adapter) noteLimits(h http.Header) {
	now := a.now()
	if d := retryAfter(h, now); d > 0 {
		a.blockUntil(now.Add(d))
	}
	if v := strings.TrimSpace(h.Get("X-RateLimit-Delay")); v != "" {
		if s, err := strconv.ParseFloat(v, 64); err == nil && s > 0 {
			d := shortWait
			if s < shortWait.Seconds() {
				d = time.Duration(s * float64(time.Second))
			}
			a.blockUntil(now.Add(d))
		}
	}
	if rem := strings.TrimSpace(h.Get("X-RateLimit-Remaining")); rem != "" {
		if n, err := strconv.ParseFloat(rem, 64); err == nil && n <= 0 {
			if reset, err := strconv.ParseInt(strings.TrimSpace(h.Get("X-RateLimit-Reset")), 10, 64); err == nil {
				a.blockUntil(time.Unix(reset, 0))
			}
		}
	}
}

// sleepCtx waits d, or until ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func retryAfter(h http.Header, now time.Time) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if s, err := strconv.ParseFloat(v, 64); err == nil {
		if s <= 0 {
			return 0
		}
		return time.Duration(s * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

// do sends one request (in marshalled as JSON with content type ctype) and decodes the reply into
// out. Errors name the provider and the credential source, never the token.
func (a *Adapter) do(ctx context.Context, method, u, ctype string, in, out any) error {
	c, src, err := a.httpClient(ctx)
	if err != nil {
		return err
	}
	if wait := a.blockedFor(); wait > 0 {
		// A short delay (Azure DevOps sends Retry-After on 200s while it slows requests) is waited
		// out here, within ctx; a long one, or one past ctx's deadline, fails fast.
		if dl, ok := ctx.Deadline(); wait > shortWait || ok && time.Until(dl) < wait {
			return &hubsync.RateLimitError{Provider: Provider, RetryAfter: wait}
		}
		if err := a.sleep(ctx, wait); err != nil {
			return fmt.Errorf("%s: %w", Provider, err)
		}
	}
	if a.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.timeout)
		defer cancel()
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("%s: encode request: %w", Provider, err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return fmt.Errorf("%s: %w", Provider, err)
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", Provider, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	a.noteLimits(resp.Header)
	code := resp.StatusCode
	switch {
	case code == http.StatusTooManyRequests || (code == http.StatusServiceUnavailable && resp.Header.Get("Retry-After") != ""):
		return &hubsync.RateLimitError{Provider: Provider, RetryAfter: a.blockedFor()}
	case code == http.StatusUnauthorized || code == http.StatusNonAuthoritativeInfo:
		if src == cred.SourceCLI {
			forgetAzToken(a.cfg.Env) // a revoked or rotated az token is not handed out again
		}
		return notAuthorized(src, code)
	case code == http.StatusForbidden:
		return fmt.Errorf("%s: forbidden (credential from %s): %s", Provider, src, message(data))
	case code == http.StatusNotFound:
		return fmt.Errorf("%s: %s: %w", Provider, message(data), hubsync.ErrNotFound)
	case code < 200 || code >= 300:
		return fmt.Errorf("%s: %s %s: HTTP %d: %s", Provider, method, req.URL.Path, code, message(data))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "html") || bytes.HasPrefix(bytes.TrimSpace(data), []byte("<")) {
			return notAuthorized(src, code) // a sign-in page instead of JSON
		}
		return fmt.Errorf("%s: %s %s: decode reply: %w", Provider, method, req.URL.Path, err)
	}
	return nil
}

func notAuthorized(src cred.Source, code int) error {
	return fmt.Errorf("%s: not authorized (credential from %s, HTTP %d): run az login for this identity, or tq secret set with a PAT",
		Provider, src, code)
}

// message is the "message" of an Azure DevOps error reply, or a short status text.
func message(data []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &e) == nil && e.Message != "" {
		m := e.Message
		if len(m) > 300 {
			m = m[:300] + "..."
		}
		return m
	}
	return "no detail"
}

var errBadID = errors.New("work item id must be a positive number")
