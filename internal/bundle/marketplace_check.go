package bundle

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

// scpLikeRe matches git's scp-like ssh syntax, user@host:path. The user
// and host are plain and cannot start with "-" (ssh would read an option),
// and the path does not start with ":", "/" or "\" (which git would read
// differently) and holds no control characters.
var scpLikeRe = regexp.MustCompile(`^[A-Za-z0-9._~][A-Za-z0-9._~-]*@[A-Za-z0-9.][A-Za-z0-9.-]*:[^:/\\\x00-\x1f\x7f][^\\\x00-\x1f\x7f]*$`)

// marketplaceChecks returns the problems of one catalog marketplace in two
// tiers. warns are reported and the entry is still written; refusals are
// reported by Validate with a "refused:" prefix and make Compute (so
// `tq bundle sync`) fail for a bundle that lists the entry. A marketplace
// installs plugins, and plugins install hooks, so whatever can change the
// fetched content is code execution: a git url with a transport other than
// https, ssh or scp-like ssh (file://, ext::, a local path, an option), a
// plain-text transport (http://, git://, or a url source that is not
// https) unless the entry sets insecure: true, or a ref git could read as
// an option, a revision expression or a path.
func marketplaceChecks(mp Marketplace) (warns, refusals []string) {
	switch mp.Source {
	case "github":
		if mp.Repo == "" {
			warns = append(warns, "github source has no repo")
		}
	case "git":
		if mp.URL == "" {
			warns = append(warns, "git source has no url")
			break
		}
		w, r := gitURLProblems(mp.URL, mp.Insecure)
		warns = append(warns, w...)
		refusals = append(refusals, r...)
	case "url":
		w, r := urlSourceProblems(mp.URL, mp.Insecure)
		warns = append(warns, w...)
		refusals = append(refusals, r...)
	case "directory", "local":
	default:
		warns = append(warns, fmt.Sprintf("unknown source kind %q", mp.Source))
	}
	if mp.Ref != "" && (mp.Source == "github" || mp.Source == "git") {
		if why := badRef(mp.Ref); why != "" {
			refusals = append(refusals, fmt.Sprintf("ref %q %s", mp.Ref, why))
		}
	}
	if mp.Insecure {
		warns = append(warns, "insecure: true allows a plain-text transport; anyone on the network path can change the plugins (and hooks) it installs")
	}
	return warns, refusals
}

// hasControl reports whether s holds a control character (newline included).
func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// plainText refuses a plain-text transport, or warns about it when the
// entry opted in with insecure: true.
func plainText(what, u string, insecure bool) (warns, refusals []string) {
	msg := fmt.Sprintf("%s %q uses a plain-text transport", what, redactURL(u))
	if insecure {
		return []string{msg + " (allowed by insecure: true)"}, nil
	}
	return nil, []string{msg + "; use https:// (or ssh:// for git), or set insecure: true for a local mirror"}
}

// gitURLProblems checks a git source's clone URL.
func gitURLProblems(u string, insecure bool) (warns, refusals []string) {
	warns = credentialProblems(u)
	if hasControl(u) {
		return warns, []string{"git url contains a control character"}
	}
	if scpLikeRe.MatchString(u) {
		return warns, nil
	}
	pu, err := url.Parse(u)
	if err != nil || pu.Host == "" {
		return warns, []string{fmt.Sprintf("git url %q is not https://, ssh:// or user@host:path", redactURL(u))}
	}
	if strings.HasPrefix(pu.Hostname(), "-") || (pu.User != nil && strings.HasPrefix(pu.User.Username(), "-")) {
		return warns, []string{fmt.Sprintf("git url %q has a host or user starting with -", redactURL(u))}
	}
	switch strings.ToLower(pu.Scheme) {
	case "https", "ssh":
		return warns, nil
	case "http", "git":
		w, r := plainText("git url", u, insecure)
		return append(warns, w...), r
	default:
		return warns, []string{fmt.Sprintf("git url %q uses transport %q; only https://, ssh:// and user@host:path are written", redactURL(u), pu.Scheme)}
	}
}

// urlSourceProblems checks a url source's marketplace.json URL: https only
// (http with insecure: true).
func urlSourceProblems(u string, insecure bool) (warns, refusals []string) {
	warns = credentialProblems(u)
	if hasControl(u) {
		return warns, []string{"url contains a control character"}
	}
	pu, err := url.Parse(u)
	if err != nil || pu.Host == "" || strings.HasPrefix(pu.Hostname(), "-") {
		return warns, []string{fmt.Sprintf("url %q is not an https:// URL", redactURL(u))}
	}
	switch strings.ToLower(pu.Scheme) {
	case "https":
		return warns, nil
	case "http":
		w, r := plainText("url", u, insecure)
		return append(warns, w...), r
	default:
		return warns, []string{fmt.Sprintf("url %q uses scheme %q; only https:// is written", redactURL(u), pu.Scheme)}
	}
}

// credentialProblems warns when a URL carries credentials: a token-shaped
// value anywhere, a password, or any userinfo on an http(s) URL. A bare ssh
// user name (ssh://git@host/...) is normal and not reported.
func credentialProblems(u string) []string {
	var warns []string
	if manifest.LooksLikeSecret(u) {
		warns = append(warns, "url looks like it carries a token/secret (settings.json would hold it in clear; use a credential helper)")
	}
	if pu, err := url.Parse(u); err == nil && pu.User != nil {
		_, hasPassword := pu.User.Password()
		scheme := strings.ToLower(pu.Scheme)
		if hasPassword || scheme == "http" || scheme == "https" {
			warns = append(warns, "url carries userinfo (user/password); settings.json would hold it in clear, use a credential helper")
		}
	}
	return warns
}

// badRef reports why ref is not a plain branch or tag name, or "".
func badRef(ref string) string {
	switch {
	case strings.HasPrefix(ref, "-"):
		return "starts with -"
	case strings.Contains(ref, ".."):
		return "contains .."
	case strings.HasSuffix(ref, ".lock"):
		return "ends with .lock"
	case strings.HasSuffix(ref, "/"):
		return "ends with /"
	}
	if hasControl(ref) {
		return "contains a control character"
	}
	for _, r := range ref {
		if strings.ContainsRune(" :~^?*[\\", r) {
			return fmt.Sprintf("contains %q", r)
		}
	}
	return ""
}

// redactURL drops the userinfo of a parseable URL, so a drift detail or a
// warning never prints a password or token embedded there; a URL still
// token-shaped after that (a token in the path or query) is not printed.
func redactURL(u string) string {
	if pu, err := url.Parse(u); err == nil && pu.User != nil {
		pu.User = nil
		u = pu.String()
	}
	if manifest.LooksLikeSecret(u) {
		return "<url with a token-shaped value>"
	}
	return u
}
