package pgtap

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// HostPort is one host of a connection target. A Host starting with "/" is
// a Unix-socket directory.
type HostPort struct {
	Host string
	Port int
}

// Socket reports a Unix-socket host.
func (h HostPort) Socket() bool {
	return strings.HasPrefix(h.Host, "/") || strings.HasPrefix(h.Host, "@")
}

// String is host:port (never printed with credentials).
func (h HostPort) String() string { return h.Host + ":" + strconv.Itoa(h.Port) }

// Target is a parsed Postgres connection string.
type Target struct {
	Hosts    []HostPort
	HostAddr []string
	DB, User string
	Password string
	SSLMode  string
}

// errTarget never carries the value: a connection string holds a password.
var errTarget = errors.New("the test_db connection string cannot be parsed (postgres://… or key=value form)")

// ParseTarget parses a postgres:// or postgresql:// URL, or a libpq
// key=value string. A service= reference is refused: tq could not tell
// where it points.
func ParseTarget(s string) (Target, error) {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, "postgres://"), strings.HasPrefix(s, "postgresql://"):
		return parseURL(s)
	case strings.Contains(s, "=") && !strings.Contains(s, "://"):
		return parseKV(s)
	}
	return Target{}, errTarget
}

func parseURL(s string) (Target, error) {
	var t Target
	rest := s[strings.Index(s, "://")+3:]
	query := ""
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest, query = rest[:i], rest[i+1:]
	}
	authority, db := rest, ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		authority, db = rest[:i], rest[i+1:]
	}
	if i := strings.LastIndexByte(authority, '@'); i >= 0 {
		user, pass, hasPass := strings.Cut(authority[:i], ":")
		var err error
		if t.User, err = url.PathUnescape(user); err != nil {
			return Target{}, errTarget
		}
		if hasPass {
			if t.Password, err = url.PathUnescape(pass); err != nil {
				return Target{}, errTarget
			}
		}
		authority = authority[i+1:]
	}
	var err error
	if t.DB, err = url.PathUnescape(db); err != nil {
		return Target{}, errTarget
	}
	if authority != "" {
		for _, hp := range strings.Split(authority, ",") {
			h, err := parseHostPort(hp)
			if err != nil {
				return Target{}, err
			}
			t.Hosts = append(t.Hosts, h)
		}
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return Target{}, errTarget
	}
	params := map[string]string{}
	for k, v := range q {
		if len(v) > 0 {
			params[k] = v[len(v)-1]
		}
	}
	if err := t.apply(params); err != nil {
		return Target{}, err
	}
	return t.finish()
}

func parseHostPort(hp string) (HostPort, error) {
	hp, err := url.PathUnescape(hp)
	if err != nil {
		return HostPort{}, errTarget
	}
	if strings.HasPrefix(hp, "/") {
		return HostPort{Host: hp}, nil
	}
	host, port := hp, ""
	if strings.HasPrefix(hp, "[") {
		e := strings.IndexByte(hp, ']')
		if e < 0 {
			return HostPort{}, errTarget
		}
		host = hp[1:e]
		if r := hp[e+1:]; r != "" {
			if !strings.HasPrefix(r, ":") {
				return HostPort{}, errTarget
			}
			port = r[1:]
		}
	} else if i := strings.LastIndexByte(hp, ':'); i >= 0 {
		host, port = hp[:i], hp[i+1:]
	}
	h := HostPort{Host: host}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n <= 0 || n > 65535 {
			return HostPort{}, errTarget
		}
		h.Port = n
	}
	return h, nil
}

// parseKV parses libpq's key=value form (values may be single-quoted).
func parseKV(s string) (Target, error) {
	params := map[string]string{}
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}
		eq := strings.IndexByte(s[i:], '=')
		if eq < 0 {
			return Target{}, errTarget
		}
		k := strings.TrimSpace(s[i : i+eq])
		i += eq + 1
		for i < len(s) && s[i] == ' ' {
			i++
		}
		var v strings.Builder
		if i < len(s) && s[i] == '\'' {
			i++
			closed := false
			for i < len(s) {
				c := s[i]
				if c == '\\' && i+1 < len(s) {
					v.WriteByte(s[i+1])
					i += 2
					continue
				}
				if c == '\'' {
					i++
					closed = true
					break
				}
				v.WriteByte(c)
				i++
			}
			if !closed {
				return Target{}, errTarget
			}
		} else {
			for i < len(s) && s[i] != ' ' && s[i] != '\t' {
				v.WriteByte(s[i])
				i++
			}
		}
		if k == "" {
			return Target{}, errTarget
		}
		params[k] = v.String()
	}
	var t Target
	if err := t.apply(params); err != nil {
		return Target{}, err
	}
	return t.finish()
}

func (t *Target) apply(p map[string]string) error {
	for k, v := range p {
		switch k {
		case "host":
			t.Hosts = nil
			for _, h := range strings.Split(v, ",") {
				t.Hosts = append(t.Hosts, HostPort{Host: strings.TrimSpace(h)})
			}
		case "hostaddr":
			t.HostAddr = strings.Split(v, ",")
		case "dbname":
			t.DB = v
		case "user":
			t.User = v
		case "password":
			t.Password = v
		case "sslmode":
			t.SSLMode = v
		case "service", "servicefile":
			return errors.New("the test_db connection string names a service: tq cannot tell where it points")
		}
	}
	if v, ok := p["port"]; ok {
		ports := strings.Split(v, ",")
		for i := range t.Hosts {
			pv := ports[len(ports)-1]
			if i < len(ports) {
				pv = ports[i]
			}
			n, err := strconv.Atoi(strings.TrimSpace(pv))
			if err != nil || n <= 0 || n > 65535 {
				return errTarget
			}
			t.Hosts[i].Port = n
		}
	}
	return nil
}

func (t Target) finish() (Target, error) {
	if len(t.Hosts) == 0 {
		t.Hosts = []HostPort{{Host: "localhost"}}
	}
	for i := range t.Hosts {
		if t.Hosts[i].Host == "" {
			t.Hosts[i].Host = "localhost"
		}
		if t.Hosts[i].Port == 0 {
			t.Hosts[i].Port = 5432
		}
	}
	return t, nil
}

// ErrNotLocal is the reason a target is refused before anything connects.
var ErrNotLocal = errors.New("test_db is not local")

// Local accepts only local targets: localhost, a loopback address, a Unix
// socket, or a docker-compose service name listed in test_db.hosts. Every
// host (and every hostaddr) of the string must be local.
func (t Target) Local(services []string) error {
	for _, h := range t.Hosts {
		if !localHost(h.Host, services) {
			return ErrNotLocal
		}
	}
	for _, a := range t.HostAddr {
		if ip := net.ParseIP(strings.TrimSpace(a)); ip == nil || !ip.IsLoopback() {
			return ErrNotLocal
		}
	}
	return nil
}

func localHost(h string, services []string) bool {
	if strings.HasPrefix(h, "/") || strings.HasPrefix(h, "@") {
		return true
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	for _, s := range services {
		if strings.EqualFold(s, h) {
			return true
		}
	}
	return false
}

// Env is the PG* environment for the child (first host only).
func (t Target) Env() []string {
	h := t.Hosts[0]
	env := []string{"PGHOST=" + h.Host, "PGPORT=" + strconv.Itoa(h.Port)}
	if t.DB != "" {
		env = append(env, "PGDATABASE="+t.DB)
	}
	if t.User != "" {
		env = append(env, "PGUSER="+t.User)
	}
	if t.Password != "" {
		env = append(env, "PGPASSWORD="+t.Password)
	}
	if t.SSLMode != "" {
		env = append(env, "PGSSLMODE="+t.SSLMode)
	}
	return env
}

// Args are pg_prove's connection flags; they win over any PG* variable the
// parent environment holds. The password is never on the command line.
func (t Target) Args() []string {
	h := t.Hosts[0]
	a := []string{"--host", h.Host, "--port", strconv.Itoa(h.Port)}
	if t.DB != "" {
		a = append(a, "--dbname", t.DB)
	}
	if t.User != "" {
		a = append(a, "--username", t.User)
	}
	return a
}

// Secrets are the values to mask in any tool output.
func (t Target) Secrets() []string {
	if t.Password == "" {
		return nil
	}
	return []string{t.Password, url.PathEscape(t.Password), url.QueryEscape(t.Password)}
}
