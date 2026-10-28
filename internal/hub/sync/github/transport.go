package github

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// callTimeout bounds one request (gh run or HTTP call) when the caller's context has no deadline.
const callTimeout = 60 * time.Second

// maxBody caps a response read into memory.
const maxBody = 16 << 20

var now = time.Now

// setNow swaps the clock (tests) and returns the restore func.
func setNow(f func() time.Time) (restore func()) {
	prev := now
	now = f
	return func() { now = prev }
}

// response is one HTTP answer, from either transport.
type response struct {
	status int
	header http.Header
	body   []byte
}

// backend sends one GitHub API request. path is a REST path ("repos/o/r/issues") or "graphql".
type backend interface {
	do(ctx context.Context, method, path string, body []byte) (response, error)
}

// errCLIUnavailable: gh cannot act for this identity (not installed, logged out, a rejected
// token, or no GH_CONFIG_DIR in the identity env), so the next credential source is tried. It is
// wrapped with the reason, which never holds a token.
var errCLIUnavailable = errors.New("gh cannot be used for this workspace")

// errNoConfigDir: the identity env pins no gh config dir, so gh would act as whatever login the
// hub process has (another workspace's or the shell's).
var errNoConfigDir = fmt.Errorf("%w: the workspace identity sets no GH_CONFIG_DIR", errCLIUnavailable)

// ghCommand is the gh executable looked up on PATH (tests point it at a name that is not there).
var ghCommand = "gh"

// ghBackend runs `gh api` with the workspace identity environment. gh keeps the token in its own
// store; tq never sees it.
type ghBackend struct{ env []string }

// pinsConfigDir says whether an identity env sets a non-empty GH_CONFIG_DIR.
func pinsConfigDir(env []string) bool {
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if strings.EqualFold(k, "GH_CONFIG_DIR") && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

func (g *ghBackend) do(ctx context.Context, method, path string, body []byte) (response, error) {
	if !pinsConfigDir(g.env) {
		return response{}, errNoConfigDir
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	args := []string{"api", "--include", "-X", method,
		"-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2022-11-28"}
	if body != nil {
		args = append(args, "--input", "-")
	}
	args = append(args, strings.TrimPrefix(path, "/"))
	cmd := exec.CommandContext(ctx, ghCommand, args...)
	cmd.Env = ghEnv(os.Environ(), g.env)
	if body != nil {
		cmd.Stdin = bytes.NewReader(body)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{w: &stdout, n: maxBody}
	cmd.Stderr = &limitWriter{w: &stderr, n: 4096}
	runErr := cmd.Run()
	if resp, ok := parseIncluded(stdout.Bytes()); ok {
		if resp.status == http.StatusUnauthorized { // gh's stored token is expired or revoked
			return response{}, fmt.Errorf("%w: gh api %s: HTTP 401: %s", errCLIUnavailable, path, clip(apiMessage(resp.body)))
		}
		return resp, nil // gh exits 1 on an HTTP error but still prints the response
	}
	if runErr == nil {
		return response{}, fmt.Errorf("github: gh api %s: unreadable output", path)
	}
	if ctx.Err() != nil {
		return response{}, fmt.Errorf("github: gh api %s: %w", path, ctx.Err())
	}
	msg := strings.TrimSpace(stderr.String())
	var ee *exec.ExitError
	if errors.Is(runErr, exec.ErrNotFound) || (errors.As(runErr, &ee) && ee.ExitCode() == 4) ||
		strings.Contains(msg, "gh auth login") {
		return response{}, fmt.Errorf("%w: gh is not installed or not logged in", errCLIUnavailable)
	}
	return response{}, fmt.Errorf("github: gh api %s: %v: %s", path, runErr, clip(msg))
}

// parseIncluded reads `gh api --include` output: a status line, headers, a blank line, the body.
func parseIncluded(out []byte) (response, bool) {
	if !bytes.HasPrefix(out, []byte("HTTP/")) {
		return response{}, false
	}
	r := bufio.NewReader(bytes.NewReader(out))
	line, err := r.ReadString('\n')
	if err != nil {
		return response{}, false
	}
	f := strings.Fields(line)
	if len(f) < 2 {
		return response{}, false
	}
	status, err := strconv.Atoi(f[1])
	if err != nil {
		return response{}, false
	}
	h, err := textproto.NewReader(r).ReadMIMEHeader()
	if err != nil && !errors.Is(err, io.EOF) {
		return response{}, false
	}
	body, _ := io.ReadAll(r)
	return response{status: status, header: http.Header(h), body: body}, true
}

// ambientGH are the process env vars that pick a gh login or host. They are never inherited: in
// a hub serving several workspaces they belong to another workspace or to the shell, and gh
// prefers GH_TOKEN over the config dir. Only the identity env may set them.
var ambientGH = []string{"GH_CONFIG_DIR", "GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}

// neverGH are dropped from both envs: with them gh traces requests and responses to stderr,
// which ends up in error text.
var neverGH = map[string]bool{"GH_DEBUG": true, "DEBUG": true}

// ghEnv is the child env for gh: the process env, without any ambient gh login or host, with the
// workspace identity env laid over it. Debug tracing is never on.
func ghEnv(base, identity []string) []string {
	over := map[string]string{}
	var order []string
	for _, kv := range identity {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" || neverGH[strings.ToUpper(k)] {
			continue
		}
		if _, seen := over[strings.ToUpper(k)]; !seen {
			order = append(order, k)
		}
		over[strings.ToUpper(k)] = v
	}
	drop := map[string]bool{}
	for k := range over {
		drop[k] = true
	}
	for _, k := range ambientGH {
		drop[k] = true
	}
	for k := range neverGH {
		drop[k] = true
	}
	out := make([]string, 0, len(base)+len(order)+2)
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if drop[strings.ToUpper(k)] {
			continue
		}
		out = append(out, kv)
	}
	for _, k := range order {
		out = append(out, k+"="+over[strings.ToUpper(k)])
	}
	return append(out, "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "NO_COLOR=1")
}

// httpBackend calls the API with a token from a cred.Transport (keychain or env source).
type httpBackend struct {
	client  *http.Client
	baseURL string // REST root, no trailing slash
	gqlURL  string
}

func newHTTPBackend(t cred.Transport, baseURL string) *httpBackend {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	gql := base + "/graphql"
	if strings.HasSuffix(base, "/api/v3") { // GitHub Enterprise Server
		gql = strings.TrimSuffix(base, "/v3") + "/graphql"
	}
	return &httpBackend{client: cred.Client(t, nil), baseURL: base, gqlURL: gql}
}

func (h *httpBackend) do(ctx context.Context, method, path string, body []byte) (response, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	u := h.baseURL + "/" + strings.TrimPrefix(path, "/")
	if path == "graphql" {
		u = h.gqlURL
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return response{}, fmt.Errorf("github: %s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "tentaqles-tq")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		var se *cred.SourceError
		if errors.As(err, &se) {
			return response{}, se
		}
		return response{}, fmt.Errorf("github: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return response{}, fmt.Errorf("github: %s %s: reading the response: %w", method, path, err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: b}, nil
}

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, callTimeout)
}

// rateLimit classifies a response as a primary or secondary rate limit.
func rateLimit(r response) *hubsync.RateLimitError {
	remaining := r.header.Get("X-RateLimit-Remaining")
	secondary := r.status == http.StatusForbidden && bytes.Contains(bytes.ToLower(r.body), []byte("rate limit"))
	if r.status != http.StatusTooManyRequests && !secondary && !(r.status == http.StatusForbidden && remaining == "0") {
		return nil
	}
	return &hubsync.RateLimitError{Provider: Provider, RetryAfter: retryAfter(r.header, true)}
}

// retryAfter reads Retry-After, then an exhausted X-RateLimit-Reset. GitHub asks for at least a
// minute when it says neither; fallback says whether to use that minute.
func retryAfter(h http.Header, fallback bool) time.Duration {
	if s := h.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
		if t, err := http.ParseTime(s); err == nil {
			if d := t.Sub(now()); d > 0 {
				return d
			}
		}
	}
	if h.Get("X-RateLimit-Remaining") == "0" {
		if n, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			d := time.Unix(n, 0).Sub(now())
			if d < time.Second {
				d = time.Second
			}
			return min(d, time.Hour)
		}
	}
	if fallback {
		return time.Minute
	}
	return 0
}

// apiMessage is GitHub's top-level error message in a response body.
func apiMessage(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Message
}

// apiError is a non-2xx answer: the status and GitHub's message, never a header or a token.
func apiError(method, path string, r response) error {
	msg := ""
	var e struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Field   string `json:"field"`
		} `json:"errors"`
	}
	if json.Unmarshal(r.body, &e) == nil {
		msg = e.Message
		for _, x := range e.Errors {
			d := x.Message
			if d == "" {
				d = strings.TrimSpace(x.Field + " " + x.Code)
			}
			if d != "" {
				msg += "; " + d
			}
		}
	}
	err := fmt.Errorf("github: %s %s: HTTP %d: %s", method, path, r.status, clip(msg))
	if r.status == http.StatusNotFound || r.status == http.StatusGone {
		return fmt.Errorf("%w: %w", hubsync.ErrNotFound, err)
	}
	return err
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}

type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	n := len(p)
	if l.n <= 0 {
		return n, nil
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	l.n -= len(p)
	_, err := l.w.Write(p)
	return n, err
}
