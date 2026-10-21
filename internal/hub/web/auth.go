package web

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
)

const (
	HeaderToken = "X-TQ-Hub-Token" // tq:allow-secret (a header name)
	HeaderAgent = "X-TQ-Hub-Agent"
	CookieToken = "tq_hub_token" // tq:allow-secret (a cookie name)
)

// csp: scripts only from /static (no inline script or handler runs), nothing framed, forms post home.
const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func eq(a, b string) bool { return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func (s *Server) tokenOK(r *http.Request) bool {
	if v := r.Header.Get(HeaderToken); v != "" && eq(HashToken(v), s.opts.TokenHash) {
		return true
	}
	if c, err := r.Cookie(CookieToken); err == nil && eq(HashToken(c.Value), s.opts.TokenHash) {
		return true
	}
	return false
}

// sameOrigin: Origin against the request's Host, else Referer, else Sec-Fetch-Site. A request
// carrying none of them is refused.
func sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		return o == "http://"+r.Host
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		return err == nil && u.Scheme == "http" && u.Host == r.Host
	}
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

// guard: Host check (DNS rebinding), token when configured, same-origin for every write.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		if len(s.opts.AllowedHosts) > 0 && !slices.Contains(s.opts.AllowedHosts, strings.ToLower(r.Host)) {
			http.Error(w, "unexpected Host header", http.StatusMisdirectedRequest)
			return
		}
		local := eq(r.Header.Get(serveinfo.HeaderLocal), s.opts.LocalKey)
		if s.opts.TokenHash != "" && !local && r.URL.Path != "/login" && !s.tokenOK(r) {
			http.Error(w, "this hub needs its token: open /login?token=<token> once in this browser, or send "+HeaderToken, http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !local && !sameOrigin(r) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if s.opts.TokenHash == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !eq(HashToken(tok), s.opts.TokenHash) {
		http.Error(w, "wrong token", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieToken, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
