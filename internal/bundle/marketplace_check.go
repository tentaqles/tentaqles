package bundle

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

// scpLikeRe matches git's scp-like ssh syntax, user@host:path, where the
// path does not start with "/" twice or ":" (which git would read
// differently) and the user and host are plain.
var scpLikeRe = regexp.MustCompile(`^[A-Za-z0-9._~-]+@[A-Za-z0-9.-]+:[^:/\\][^\\]*$`)

// marketplaceChecks returns the problems of one catalog marketplace in two
// tiers. warns are reported and the entry is still written; refusals are
// reported by Validate with a "refused:" prefix and make Compute (so
// `tq bundle sync`) fail for a bundle that lists the entry, since Claude
// Code would hand them to git: a git url with a transport other than
// https, ssh or scp-like ssh (file://, ext::, a local path, an option),
// or a ref git could read as an option, a revision expression or a path.
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
		w, r := gitURLProblems(mp.URL)
		warns = append(warns, w...)
		refusals = append(refusals, r...)
	case "url":
		warns = append(warns, credentialProblems(mp.URL)...)
	case "directory", "local":
	default:
		warns = append(warns, fmt.Sprintf("unknown source kind %q", mp.Source))
	}
	if mp.Ref != "" && (mp.Source == "github" || mp.Source == "git") {
		if why := badRef(mp.Ref); why != "" {
			refusals = append(refusals, fmt.Sprintf("ref %q %s", mp.Ref, why))
		}
	}
	return warns, refusals
}

// gitURLProblems checks a git source's clone URL.
func gitURLProblems(u string) (warns, refusals []string) {
	warns = credentialProblems(u)
	if scpLikeRe.MatchString(u) {
		return warns, nil
	}
	pu, err := url.Parse(u)
	if err != nil || pu.Host == "" {
		return warns, append(refusals, fmt.Sprintf("git url %q is not https://, ssh:// or user@host:path", redactURL(u)))
	}
	switch strings.ToLower(pu.Scheme) {
	case "https", "ssh":
		return warns, nil
	case "http", "git":
		return append(warns, fmt.Sprintf("git url %q uses an unencrypted transport; use https:// or ssh://", redactURL(u))), nil
	default:
		return warns, append(refusals, fmt.Sprintf("git url %q uses transport %q; only https://, ssh:// and user@host:path are written", redactURL(u), pu.Scheme))
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
	for _, r := range ref {
		if r < 0x20 || r == 0x7f {
			return "contains a control character"
		}
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
