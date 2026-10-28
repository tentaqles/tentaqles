package azdo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// Resource is the Azure DevOps application id an Entra access token is requested for.
const Resource = "499b84ac-1321-427f-aa17-267ca6975798"

// azTimeout bounds one az call: a CLI that hangs (a login prompt, a slow proxy) must never stall
// the sync loop.
const azTimeout = 30 * time.Second

// azMargin is how long before its expiry a cached az token is no longer handed out.
const azMargin = 5 * time.Minute

var (
	lookPath = exec.LookPath
	azNow    = time.Now
)

// azCache holds az tokens in memory, keyed by the identity's AZURE_CONFIG_DIR, until shortly
// before they expire, so az runs once per token instead of once per HTTP call. It is never
// written anywhere.
var azCache = struct {
	gosync.Mutex
	m map[string]azEntry
}{m: map[string]azEntry{}}

type azEntry struct {
	tok cred.Token
	exp time.Time
}

// configDir is the AZURE_CONFIG_DIR env sets (the last entry wins, as for exec), "" when none.
func configDir(env []string) string {
	dir := ""
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "AZURE_CONFIG_DIR") {
			dir = strings.TrimSpace(v)
		}
	}
	return dir
}

// forgetAzToken drops the cached token of env's identity (after the tracker refused it).
func forgetAzToken(env []string) {
	if dir := configDir(env); dir != "" {
		azCache.Lock()
		delete(azCache.m, dir)
		azCache.Unlock()
	}
}

// AzToken is the cli credential source for azure-devops: `az account get-access-token` run with
// env, the workspace identity environment, as its whole environment. env must set this
// identity's AZURE_CONFIG_DIR: without it az would fall back to the default ~/.azure login,
// which may belong to another client, so a nil env or one without AZURE_CONFIG_DIR is
// cred.ErrNoCredential and az never runs. A missing or logged-out az is cred.ErrNoCredential too.
// The token is kept in memory until shortly before its expiry and never logged; az's stderr is
// quoted only as a short reason, and az never prints the token there.
func AzToken(ctx context.Context, env []string) (cred.Token, error) {
	dir := configDir(env)
	if dir == "" {
		return cred.Token{}, fmt.Errorf("%w: no AZURE_CONFIG_DIR for this identity (the default az login is never used)",
			cred.ErrNoCredential)
	}
	azCache.Lock()
	e, ok := azCache.m[dir]
	azCache.Unlock()
	if ok && azNow().Add(azMargin).Before(e.exp) {
		return e.tok, nil
	}
	tok, exp, err := runAz(ctx, env)
	if err != nil {
		return cred.Token{}, err
	}
	if !exp.IsZero() {
		azCache.Lock()
		azCache.m[dir] = azEntry{tok: tok, exp: exp}
		azCache.Unlock()
	}
	return tok, nil
}

// runAz runs az once and returns the token and its expiry (zero when az printed none).
func runAz(ctx context.Context, env []string) (cred.Token, time.Time, error) {
	path, err := lookPath("az")
	if err != nil {
		return cred.Token{}, time.Time{}, fmt.Errorf("%w: az CLI not found (install the Azure CLI, then az login)", cred.ErrNoCredential)
	}
	ctx, cancel := context.WithTimeout(ctx, azTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "account", "get-access-token", "--resource", Resource, "--output", "json")
	cmd.Env = env
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return cred.Token{}, time.Time{}, fmt.Errorf("az account get-access-token timed out after %s", azTimeout)
		}
		if ctx.Err() != nil {
			return cred.Token{}, time.Time{}, ctx.Err()
		}
		return cred.Token{}, time.Time{}, fmt.Errorf("%w: az account get-access-token failed (run az login for this identity): %s",
			cred.ErrNoCredential, reason(stderr.String()))
	}
	var out struct {
		AccessToken string          `json:"accessToken"`
		ExpiresOn   string          `json:"expiresOn"`  // local time, every az version
		ExpiresUnix json.RawMessage `json:"expires_on"` // epoch seconds, az 2.54+
	}
	// The reply holds the token: a decode error never quotes it.
	if json.Unmarshal(stdout.Bytes(), &out) != nil {
		return cred.Token{}, time.Time{}, fmt.Errorf("%w: az account get-access-token printed no token (run az login)", cred.ErrNoCredential)
	}
	tok := strings.TrimSpace(out.AccessToken)
	if tok == "" || strings.ContainsAny(tok, " \t\r\n") {
		return cred.Token{}, time.Time{}, fmt.Errorf("%w: az account get-access-token printed no token (run az login)", cred.ErrNoCredential)
	}
	return cred.Token{Scheme: "Bearer", Value: tok}, azExpiry(out.ExpiresUnix, out.ExpiresOn), nil
}

// azExpiry reads expires_on (a number or a numeric string), else expiresOn in local time.
func azExpiry(unix json.RawMessage, local string) time.Time {
	if s := strings.Trim(strings.TrimSpace(string(unix)), `"`); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			return time.Unix(n, 0)
		}
	}
	for _, layout := range []string{"2006-01-02 15:04:05.999999", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(local), time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

// reason is the first non-empty line of a CLI's stderr, capped.
func reason(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if len(l) > 200 {
				l = l[:200] + "..."
			}
			return l
		}
	}
	return "no detail"
}
