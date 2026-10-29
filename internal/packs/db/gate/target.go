package gate

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs/db/drill"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
)

// Target is what a command says about its database: the hosts it names,
// the signals for environment matching, and whether something tq could not
// read may change it.
type Target struct {
	Hosts []string
	Tier1 envgate.Tier // flags and URLs on the command line
	Tier3 envgate.Tier // the Supabase linked project
	Tier4 envgate.Tier // a sqlcmd -S server or a Server= fragment
	// Unknown: a target value is computed ($VAR, %VAR%, $(…)) or a
	// connection string cannot be read: the target cannot be known.
	Unknown bool
	// Local: the command names its target and every target is local
	// (localhost, loopback, a socket, --local, supabase's local default).
	Local bool
	// Linked: a Supabase command against the linked project.
	Linked bool
}

var (
	urlRe     = regexp.MustCompile(`(?i)(?:jdbc:)?(postgres(?:ql)?|sqlserver|mssql|mysql|mariadb|cockroachdb|redshift)://[^\s'"]*`)
	supaHost  = regexp.MustCompile(`(?i)^db\.([a-z0-9]{20})\.supabase\.(co|com|net)$`)
	poolUser  = regexp.MustCompile(`(?i)^[a-z_]+\.([a-z0-9]{20})$`)
	serverRe  = regexp.MustCompile(`(?i)\b(?:server|data source|addr|address)\s*=\s*([^;'"\s]+)`)
	refFileRe = regexp.MustCompile(`^[a-z0-9]{20}$`)
)

// hostSignals are the match signals of one host (and the user name, for the
// Supabase pooler's postgres.<ref> users).
func hostSignals(host, user string) envgate.Tier {
	h := strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	t := envgate.Tier{{Key: "hosts", Value: h}, {Key: "servers", Value: h}}
	if m := supaHost.FindStringSubmatch(h); m != nil {
		t = append(t, envgate.Signal{Key: "project_refs", Value: strings.ToLower(m[1])})
	}
	if m := poolUser.FindStringSubmatch(user); m != nil {
		t = append(t, envgate.Signal{Key: "project_refs", Value: strings.ToLower(m[1])})
	}
	return t
}

// parseURL reads the hosts and user of a connection URL; ok false when it
// cannot be read.
func parseURL(s string) (hosts []string, user string, ok bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "jdbc:"), "JDBC:")
	if computed(s) {
		return nil, "", false
	}
	if c, err := drill.ParseConn(s); err == nil {
		for _, h := range c.Hosts {
			hosts = append(hosts, h)
		}
		if c.HostAddr != "" {
			hosts = append(hosts, c.HostAddr)
		}
		return hosts, c.User, len(hosts) > 0
	}
	i := strings.Index(s, "://")
	if i < 0 {
		return nil, "", false
	}
	auth := s[i+3:]
	if j := strings.IndexAny(auth, "/?;"); j >= 0 {
		auth = auth[:j]
	}
	if j := strings.LastIndex(auth, "@"); j >= 0 {
		user, _ = url.PathUnescape(auth[:j])
		if k := strings.Index(user, ":"); k >= 0 {
			user = user[:k]
		}
		auth = auth[j+1:]
	}
	for _, hp := range strings.Split(auth, ",") {
		h := hp
		if strings.HasPrefix(h, "[") {
			if k := strings.Index(h, "]"); k > 0 {
				h = h[1:k]
			}
		} else if k := strings.LastIndex(h, ":"); k >= 0 {
			h = h[:k]
		}
		hosts = append(hosts, h)
	}
	if len(hosts) == 1 && hosts[0] == "" {
		hosts = []string{""} // a socket default
	}
	return hosts, user, true
}

// ReadRef reads a Supabase project-ref file's content ("" when unusable).
func ReadRef(b []byte) string {
	r := strings.TrimSpace(string(b))
	if !refFileRe.MatchString(r) {
		return ""
	}
	return r
}

func (t *Target) addURL(s string, extra []string) {
	hosts, user, ok := parseURL(s)
	if !ok {
		t.Unknown = true
		return
	}
	for _, h := range hosts {
		t.Hosts = append(t.Hosts, h)
		t.Tier1 = append(t.Tier1, hostSignals(h, user)...)
	}
}

// AllLocal reports that the target named hosts and all of them are local.
func (t Target) AllLocal(extra []string) bool {
	if len(t.Hosts) == 0 {
		return false
	}
	for _, h := range t.Hosts {
		if !drill.LocalHost(h, extra) {
			return false
		}
	}
	return true
}

// Tiers are the signal tiers in spec §14 order: flags, the dotenv wrapper,
// the linked project, the server on the line.
func (t Target) Tiers(l Leaf) []envgate.Tier {
	var dot envgate.Tier
	if l.Dotenvs {
		for _, f := range l.Dotenv {
			dot = append(dot, envgate.Signal{Key: "dotenv", Value: f})
		}
	}
	return []envgate.Tier{t.Tier1, dot, t.Tier3, t.Tier4}
}

// urls finds the connection URLs among the leaf's words and its inline
// variables (DATABASE_URL=… prisma migrate deploy).
func urls(l Leaf) []string {
	var out []string
	for _, a := range l.Args {
		out = append(out, urlRe.FindAllString(a, -1)...)
	}
	for _, v := range l.Env {
		out = append(out, urlRe.FindAllString(v, -1)...)
	}
	return out
}

// valueURL is a flag value that should be a connection string; a computed
// or unreadable one makes the target unknown.
func (t *Target) flagValue(v string, extra []string) {
	switch {
	case v == "":
	case computed(v):
		t.Unknown = true
	case urlRe.MatchString(v):
		// added through urls()
	case strings.Contains(strings.ToLower(v), "server=") || strings.Contains(strings.ToLower(v), "host="):
		t.connInfo(v)
	default:
		t.Unknown = true
	}
}

// connInfo reads host= / Server= fragments of a keyword connection string.
func (t *Target) connInfo(v string) {
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == ' ' }) {
		k, val, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "host", "hostaddr", "server", "data source", "address", "addr":
			val = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(val), "tcp:"), "TCP:")
			if i := strings.LastIndex(val, ","); i >= 0 {
				val = val[:i]
			}
			if computed(val) {
				t.Unknown = true
				continue
			}
			t.Hosts = append(t.Hosts, val)
			t.Tier1 = append(t.Tier1, hostSignals(val, "")...)
		case "service":
			t.Unknown = true
		}
	}
}

// serverFlag reads a sqlcmd-style -S value (tier 4).
func (t *Target) serverFlag(v string) {
	v = strings.TrimPrefix(strings.TrimPrefix(v, "tcp:"), "TCP:")
	if i := strings.LastIndex(v, ","); i >= 0 {
		v = v[:i]
	}
	if computed(v) {
		t.Unknown = true
		return
	}
	t.Hosts = append(t.Hosts, v)
	t.Tier4 = append(t.Tier4, hostSignals(v, "")...)
}

// serverFragments reads Server=… fragments anywhere on the line (tier 4).
func (t *Target) serverFragments(raw string) {
	for _, m := range serverRe.FindAllStringSubmatch(raw, -1) {
		t.serverFlag(m[1])
	}
}
