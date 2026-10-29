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

func (s *Server) headerTokenOK(r *http.Request) bool {
	v := r.Header.Get(HeaderToken)
	return v != "" && eq(HashToken(v), s.opts.TokenHash)
}

func (s *Server) tokenOK(r *http.Request) bool {
	if s.headerTokenOK(r) {
		return true
	}
	if c, err := r.Cookie(CookieToken); err == nil && eq(HashToken(c.Value), s.opts.TokenHash) {
		return true
	}
	return false
}

// hostAllowed: host is in AllowedHosts (case-insensitive). The request's own Host header is
// attacker-controlled under DNS rebinding, so it only ever counts when this list names it.
func (s *Server) hostAllowed(host string) bool {
	if host == "" {
		return false
	}
	return slices.ContainsFunc(s.opts.AllowedHosts, func(h string) bool { return strings.EqualFold(h, host) })
}

// sameOrigin: the Origin host, else the Referer host, must be in AllowedHosts; with neither,
// Sec-Fetch-Site: same-origin counts only when the Host itself is in the list. r.Host alone
// never vouches for a request.
func (s *Server) sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err == nil && u.Scheme == "http" && (u.Path == "" || u.Path == "/") && s.hostAllowed(u.Host)
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		return err == nil && u.Scheme == "http" && s.hostAllowed(u.Host)
	}
	return r.Header.Get("Sec-Fetch-Site") == "same-origin" && s.hostAllowed(r.Host)
}

// guard: Host allow-list (DNS rebinding), token when configured, same-origin for every write.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		// Fail closed: with neither a host allow-list nor a token nothing stops DNS rebinding.
		if len(s.opts.AllowedHosts) == 0 && s.opts.TokenHash == "" {
			http.Error(w, "this hub was started without a host allow-list; refusing every request", http.StatusMisdirectedRequest)
			return
		}
		if len(s.opts.AllowedHosts) > 0 && !s.hostAllowed(r.Host) {
			http.Error(w, "unexpected Host header", http.StatusMisdirectedRequest)
			return
		}
		local := eq(r.Header.Get(serveinfo.HeaderLocal), s.opts.LocalKey)
		if s.opts.TokenHash != "" && !local && r.URL.Path != "/login" && !s.tokenOK(r) {
			http.Error(w, "this hub needs its token: open /login?token=<token> once in this browser, or send "+HeaderToken, http.StatusUnauthorized)
			return
		}
		// A token in the request header cannot come from a cross-site page (a custom header needs a
		// CORS preflight this server never grants), so it stands in for the origin check.
		viaHeader := s.opts.TokenHash != "" && s.headerTokenOK(r)
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !local && !viaHeader && !s.sameOrigin(r) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, s.withSession(w, r))
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
