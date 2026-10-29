package drill

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// Conn is a parsed test_db connection. It never leaves tq: tools get its
// parts through their own environment variables, and nothing of it is
// printed or recorded.
type Conn struct {
	Engine   string
	Hosts    []string // every host the string names (a multi-host URL)
	Port     string
	User     string
	Password string
	Database string
	// HostAddr is a libpq hostaddr parameter, which overrides the host.
	HostAddr string
}

// Host is the first host.
func (c Conn) Host() string {
	if len(c.Hosts) == 0 {
		return ""
	}
	return c.Hosts[0]
}

// ErrConn is a connection string tq cannot read.
var ErrConn = errors.New("the test_db connection string cannot be read (use postgres://… or sqlserver://…)")

// ParseConn reads a postgres:// or postgresql:// URL, or a sqlserver://
// URL or an ADO-style "Server=…;Database=…" string.
func ParseConn(s string) (Conn, error) {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "postgres://"), strings.HasPrefix(low, "postgresql://"):
		return parsePG(s)
	case strings.HasPrefix(low, "sqlserver://"), strings.HasPrefix(low, "mssql://"):
		return parseMSURL(s)
	case strings.Contains(low, "server=") || strings.Contains(low, "data source="):
		return parseADO(s)
	}
	return Conn{}, ErrConn
}

func parsePG(s string) (Conn, error) {
	// net/url refuses a multi-host authority (h1:5432,h2:5432): cut it out.
	rest := s[strings.Index(s, "://")+3:]
	auth := rest
	if i := strings.IndexAny(rest, "/?"); i >= 0 {
		auth = rest[:i]
	}
	userinfo, hostpart := "", auth
	if i := strings.LastIndex(auth, "@"); i >= 0 {
		userinfo, hostpart = auth[:i], auth[i+1:]
	}
	u, err := url.Parse("postgres://" + userinfo + "@placeholder" + rest[len(auth):])
	if err != nil {
		return Conn{}, ErrConn
	}
	c := Conn{Engine: Postgres}
	if u.User != nil {
		c.User = u.User.Username()
		c.Password, _ = u.User.Password()
	}
	c.Database = strings.TrimPrefix(u.Path, "/")
	for _, hp := range strings.Split(hostpart, ",") {
		h, p := splitHostPort(hp)
		h, _ = url.PathUnescape(h)
		c.Hosts = append(c.Hosts, h)
		if c.Port == "" {
			c.Port = p
		}
	}
	q := u.Query()
	if v := q.Get("host"); v != "" {
		c.Hosts = append([]string{v}, c.Hosts...)
		if len(c.Hosts) > 1 && c.Hosts[1] == "" {
			c.Hosts = append(c.Hosts[:1], c.Hosts[2:]...)
		}
	}
	if v := q.Get("port"); v != "" {
		c.Port = v
	}
	if v := q.Get("dbname"); v != "" {
		c.Database = v
	}
	if v := q.Get("user"); v != "" {
		c.User = v
	}
	if v := q.Get("password"); v != "" {
		c.Password = v
	}
	c.HostAddr = q.Get("hostaddr")
	for k := range q {
		switch strings.ToLower(k) {
		case "service", "servicefile", "passfile":
			// they point libpq somewhere else than the host tq checked
			return Conn{}, errors.New("the test_db connection string names a libpq service or passfile; give the host directly")
		}
	}
	return c, nil
}

func splitHostPort(hp string) (string, string) {
	if strings.HasPrefix(hp, "[") {
		if i := strings.Index(hp, "]"); i > 0 {
			h := hp[1:i]
			p := strings.TrimPrefix(hp[i+1:], ":")
			return h, p
		}
	}
	if i := strings.LastIndex(hp, ":"); i >= 0 && strings.Count(hp, ":") == 1 {
		return hp[:i], hp[i+1:]
	}
	return hp, ""
}

func parseMSURL(s string) (Conn, error) {
	u, err := url.Parse(s)
	if err != nil {
		return Conn{}, ErrConn
	}
	c := Conn{Engine: SQLServer, Port: u.Port()}
	host := u.Hostname()
	if inst := strings.TrimPrefix(u.Path, "/"); inst != "" {
		host += `\` + inst // named instance
	}
	c.Hosts = []string{host}
	if u.User != nil {
		c.User = u.User.Username()
		c.Password, _ = u.User.Password()
	}
	q := u.Query()
	c.Database = q.Get("database")
	return c, nil
}

func parseADO(s string) (Conn, error) {
	c := Conn{Engine: SQLServer}
	for _, part := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch k {
		case "server", "data source", "address", "addr":
			v = strings.TrimPrefix(strings.TrimPrefix(v, "tcp:"), "TCP:")
			h, p := v, ""
			if i := strings.LastIndex(v, ","); i >= 0 {
				h, p = v[:i], v[i+1:]
			}
			c.Hosts, c.Port = []string{h}, p
		case "database", "initial catalog":
			c.Database = v
		case "user id", "uid", "user":
			c.User = v
		case "password", "pwd":
			c.Password = v
		}
	}
	if len(c.Hosts) == 0 {
		return Conn{}, ErrConn
	}
	return c, nil
}

// LocalHost reports a host on this machine: localhost, a loopback address,
// a Unix socket path, the SQL Server shorthands (".", "(local)") with or
// without a named instance, or a docker-compose service the manifest lists
// in test_db.hosts.
func LocalHost(h string, extra []string) bool {
	h = strings.TrimSpace(h)
	if i := strings.Index(h, `\`); i >= 0 { // SQL Server named instance
		h = h[:i]
	}
	l := strings.ToLower(strings.Trim(h, "[]"))
	switch {
	case l == "" || l == "localhost" || l == "." || l == "(local)" || l == "(localdb)":
		return true
	case strings.HasPrefix(l, "/"): // a Unix socket directory
		return true
	}
	if ip := net.ParseIP(l); ip != nil {
		return ip.IsLoopback()
	}
	for _, e := range extra {
		if strings.EqualFold(strings.TrimSpace(e), l) {
			return true
		}
	}
	return false
}

// Local reports whether every host the connection names is local (and a
// hostaddr, when set, is a loopback address).
func (c Conn) Local(extra []string) bool {
	if len(c.Hosts) == 0 {
		return false
	}
	for _, h := range c.Hosts {
		if !LocalHost(h, extra) {
			return false
		}
	}
	if c.HostAddr != "" {
		ip := net.ParseIP(c.HostAddr)
		if ip == nil || !ip.IsLoopback() {
			return false
		}
	}
	return true
}
