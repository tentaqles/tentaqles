package asana

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

const (
	// DefaultBaseURL is Asana's public REST API.
	DefaultBaseURL = "https://app.asana.com/api/1.0"
	// requestTimeout bounds every call so a stuck tracker never stalls a sync tick.
	requestTimeout = 30 * time.Second
	maxBody        = 8 << 20
	pageLimit      = 100
	maxPages       = 50
	maxErrText     = 200
)

// lazyTransport resolves the credential through the cred resolver chain (keychain, then the env
// PAT via tq dotenv; Asana has no CLI login) on the first request that needs it, and asks the
// resolved Transport for a fresh token on every request.
type lazyTransport struct {
	req cred.Request
	mu  gosync.Mutex
	t   cred.Transport
}

func (l *lazyTransport) Source() cred.Source {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.t != nil {
		return l.t.Source()
	}
	if l.req.Preferred != "" {
		return l.req.Preferred
	}
	return cred.SourceKeychain
}

func (l *lazyTransport) Token(ctx context.Context) (cred.Token, error) {
	l.mu.Lock()
	t := l.t
	l.mu.Unlock()
	if t == nil {
		var err error
		if t, err = cred.Resolve(ctx, l.req); err != nil {
			return cred.Token{}, err
		}
		l.mu.Lock()
		l.t = t
		l.mu.Unlock()
	}
	return t.Token(ctx)
}

// envelope is every Asana response: {"data": ..., "next_page": {...}} or {"errors": [...]}.
type envelope struct {
	Data     json.RawMessage `json:"data"`
	NextPage *struct {
		Offset string `json:"offset"`
	} `json:"next_page"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// do sends one request. body (if not nil) is wrapped as {"data": body}; out (if not nil) gets
// the response's data. It returns the next-page offset for list calls.
func (a *Adapter) do(ctx context.Context, method, path string, q url.Values, body, out any) (string, error) {
	u := a.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(map[string]any{"data": body})
		if err != nil {
			return "", fmt.Errorf("asana: %s %s: %w", method, path, err)
		}
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return "", fmt.Errorf("asana: %s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.http.Do(req)
	if err != nil {
		var se *cred.SourceError
		if errors.As(err, &se) {
			return "", se // which source failed, never the token
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL carries no secret, but keep errors short
		}
		return "", fmt.Errorf("asana: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", fmt.Errorf("asana: %s %s: read: %w", method, path, err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", &hubsync.RateLimitError{Provider: Provider, RetryAfter: retryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	var env envelope
	jerr := json.Unmarshal(raw, &env)
	if resp.StatusCode >= 300 {
		msg := http.StatusText(resp.StatusCode)
		if jerr == nil && len(env.Errors) > 0 && env.Errors[0].Message != "" {
			msg = env.Errors[0].Message
		}
		if len(msg) > maxErrText {
			msg = msg[:maxErrText] + "..."
		}
		err := fmt.Errorf("asana: %s %s: %d: %s", method, path, resp.StatusCode, msg)
		if resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("%w: %w", hubsync.ErrNotFound, err)
		}
		return "", err
	}
	if jerr != nil {
		return "", fmt.Errorf("asana: %s %s: bad response: %w", method, path, jerr)
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return "", fmt.Errorf("asana: %s %s: bad response: %w", method, path, err)
		}
	}
	if env.NextPage != nil {
		return env.NextPage.Offset, nil
	}
	return "", nil
}

// list follows next_page through every page (capped at maxPages) and appends each page's items.
func list[T any](ctx context.Context, a *Adapter, path string, q url.Values) ([]T, error) {
	q2 := url.Values{}
	for k, v := range q {
		q2[k] = v
	}
	q2.Set("limit", strconv.Itoa(pageLimit))
	var all []T
	for range maxPages {
		var page []T
		next, err := a.do(ctx, http.MethodGet, path, q2, nil, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if next == "" {
			return all, nil
		}
		q2.Set("offset", next)
	}
	return all, nil
}

// retryAfter reads a Retry-After header: delay seconds or an HTTP date. 0 when absent or bad.
func retryAfter(h string, now time.Time) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if s, err := strconv.Atoi(h); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

// gid checks an Asana object id: digits only, so an id can never reshape a request path.
func gid(kind, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("asana: %s id is empty", kind)
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("asana: %s id %q is not an Asana gid", kind, id)
		}
	}
	return id, nil
}
