package bundle

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

func validateOne(mp Marketplace) []string {
	return (&Catalog{Marketplaces: map[string]Marketplace{"m": mp}}).Validate()
}

func hasPrefixed(ws []string, prefix, sub string) bool {
	for _, w := range ws {
		if strings.HasPrefix(w, prefix) && strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

// F4: a URL with userinfo or a token-shaped value is reported, and the
// warning itself never prints the credential.
func TestValidate_URLCredentials(t *testing.T) {
	token := "ghp_" + strings.Repeat("a", 36)
	cases := []Marketplace{
		{Source: "git", URL: "https://user:hunter2@example.com/r.git"},
		{Source: "git", URL: "https://" + token + "@example.com/r.git"},
		{Source: "url", URL: "https://user:hunter2@example.com/marketplace.json"},
		{Source: "url", URL: "https://example.com/marketplace.json?token=" + token},
		{Source: "git", URL: "ssh://git:pw@example.com/r.git"},
	}
	for _, mp := range cases {
		ws := validateOne(mp)
		if !hasPrefixed(ws, `marketplace "m"`, "userinfo") && !hasPrefixed(ws, `marketplace "m"`, "token/secret") {
			t.Errorf("%s: want a credential warning, got %q", mp.URL, ws)
		}
		for _, w := range ws {
			if strings.Contains(w, "hunter2") || strings.Contains(w, token) || strings.Contains(w, ":pw@") {
				t.Errorf("%s: warning prints the credential: %q", mp.URL, w)
			}
		}
	}
	// A plain ssh user name is not a credential.
	for _, u := range []string{"ssh://git@github.com/acme/r.git", "git@github.com:acme/r.git", "https://github.com/acme/r.git"} {
		if ws := validateOne(Marketplace{Source: "git", URL: u}); len(ws) != 0 {
			t.Errorf("%s: want no warnings, got %q", u, ws)
		}
	}
}

func TestDescribeSource_StripsUserinfo(t *testing.T) {
	got := describeSource(Marketplace{Source: "git", URL: "https://user:hunter2@example.com/r.git", Ref: "main"}.ToKnownMarketplace())
	if got != "git https://example.com/r.git@main" {
		t.Fatalf("describeSource = %q", got)
	}
	token := "ghp_" + strings.Repeat("b", 36)
	got = describeSource(Marketplace{Source: "url", URL: "https://example.com/m.json?t=" + token}.ToKnownMarketplace())
	if strings.Contains(got, token) {
		t.Fatalf("describeSource prints a token: %q", got)
	}
	if !manifest.LooksLikeSecret(token) {
		t.Fatal("test token should look like a secret")
	}
}

// F5: only https, ssh and scp-like git URLs are written; other transports
// are refused by Validate and by Compute; plain http/git:// only warn.
func TestValidate_GitURLTransport(t *testing.T) {
	refused := []string{
		"file:///tmp/repo",
		"ext::sh -c touch% /tmp/pwned",
		"fd::17",
		"/srv/repo.git",
		`C:\repos\r`,
		"--upload-pack=touch /tmp/x",
		"ftp://example.com/r.git",
	}
	for _, u := range refused {
		ws := validateOne(Marketplace{Source: "git", URL: u})
		if !hasPrefixed(ws, "refused: ", "git url") {
			t.Errorf("%q: want a refused: warning, got %q", u, ws)
		}
		cat := &Catalog{Marketplaces: map[string]Marketplace{"m": {Source: "git", URL: u}}}
		d, errs := Compute(&manifest.Bundle{Marketplaces: []string{"m"}}, cat)
		if len(errs) == 0 || len(d.Marketplaces) != 0 {
			t.Errorf("%q: Compute should refuse it, got %+v %v", u, d.Marketplaces, errs)
		}
	}
	for _, u := range []string{"http://example.com/r.git", "git://example.com/r.git"} {
		ws := validateOne(Marketplace{Source: "git", URL: u})
		if !hasPrefixed(ws, `marketplace "m"`, "unencrypted") || hasPrefixed(ws, "refused: ", "") {
			t.Errorf("%q: want an unencrypted-transport warning only, got %q", u, ws)
		}
	}
}

func TestValidate_Ref(t *testing.T) {
	bad := []string{"-x", "--upload-pack=x", "a..b", "a:b", "a b", "v1~1", "v1^", "a?", "a*", "a[", `a\b`, "a\x01", "v1.lock", "feature/"}
	for _, ref := range bad {
		for _, src := range []Marketplace{
			{Source: "github", Repo: "a/b", Ref: ref},
			{Source: "git", URL: "https://example.com/r.git", Ref: ref},
		} {
			ws := validateOne(src)
			if !hasPrefixed(ws, "refused: ", "ref ") {
				t.Errorf("%s ref %q: want refused, got %q", src.Source, ref, ws)
			}
			cat := &Catalog{Marketplaces: map[string]Marketplace{"m": src}}
			if _, errs := Compute(&manifest.Bundle{Marketplaces: []string{"m"}}, cat); len(errs) == 0 {
				t.Errorf("%s ref %q: Compute should refuse it", src.Source, ref)
			}
		}
	}
	for _, ref := range []string{"v0.8.0", "main", "release/1.2", "feat_x-y"} {
		if ws := validateOne(Marketplace{Source: "github", Repo: "a/b", Ref: ref}); len(ws) != 0 {
			t.Errorf("ref %q: want no warnings, got %q", ref, ws)
		}
	}
}
