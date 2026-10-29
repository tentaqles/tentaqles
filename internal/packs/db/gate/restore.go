package gate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/drill"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// restoreWordRe spots a restore in a payload tq cannot open (docker exec,
// ssh, a script argument): such a restore is denied, since its target
// cannot be read.
var restoreWordRe = regexp.MustCompile(`(?i)\b(pg_restore\b|sqlpackage\b[^\n]*[/-]a(ction)?:\s*import|restore\s+database\b|supabase\s+db\s+reset\b[^\n]*--(linked|db-url)|psql\b[^\n]*(\s-[a-z0-9]*f|\s--file|<|\\(i|ir|include|include_relative)\b))|\|\s*(\S*/)?psql\b`)

var restoreDBRe = regexp.MustCompile(`restore\s+database\b`)

// psqlWordRe spots psql in an opaque payload; psqlOtherRe a psql -c or -l,
// which give psql something other than its stdin to run. Only the no-value
// switches may come before them in a bundle (-Xc, -Xl, -el): a glued value
// flag whose value ends in c or l (-dportal, -Uetl) is not -c or -l.
var (
	psqlWordRe  = regexp.MustCompile(`(?i)(^|[\s"'/\\=;(&|])psql(\.exe)?($|[\s"';)&|])`)
	psqlOtherRe = regexp.MustCompile(`(^|\s)(-[` + psqlSwitches + `]*(l|c\S*)|--command|--list)([\s=]|$)`)
)

// pipedPsql: a piped opaque leaf runs psql on its stdin. Only that psql's
// own words count as its flags: the words after it (ssh -l user box psql
// acme is not psql -l), up to the first separator, quote, backtick or
// parenthesis, so a -c or -l of a later command in the same payload does
// not count. A word tq cannot separate that way is not read, so the psql
// reads its stdin (fail closed). Every psql of the payload is checked.
func pipedPsql(l Leaf) bool {
	if !l.Piped {
		return false
	}
	raw := l.Raw
	for {
		at := psqlWordRe.FindStringIndex(raw)
		if at == nil {
			return false
		}
		rest := raw[at[1]-1:]
		own := rest
		if k := strings.IndexAny(rest, ";&|\n\r\"'`()"); k >= 0 {
			own = rest[:k]
		}
		if !psqlOtherRe.MatchString(own) {
			return true
		}
		raw = rest
		if raw == "" {
			return false
		}
		raw = raw[1:]
	}
}

// includeRe is a psql include meta-command (\i, \ir, \include,
// \include_relative): in a -c value it runs a file, like psql -f.
var includeRe = regexp.MustCompile(`(^|[\s;=])\\(i|ir|include|include_relative)(\s|$)`)

// The short switches that take no value, which may come before a value
// flag in a bundle (psql -Xf dump.sql, psql -1f dump.sql, pg_restore -Cd
// acme).
const (
	psqlSwitches      = "aAbeEHlnqsStxXz01wW"
	pgRestoreSwitches = "acCelOsxv1wW"
)

// Restore kinds.
const (
	kindPgRestore = "pg_restore"
	kindPsqlFile  = "psql -f"
	kindPsqlStdin = "psql < dump"
)

// restoreKind names what a leaf restores with, or "".
func restoreKind(l Leaf) string {
	t := l.Text()
	switch l.Prog {
	case "pg_restore":
		// without -d it only prints the SQL script
		if _, ok := pgFlag(l, 'd', "--dbname", pgRestoreSwitches); ok {
			return kindPgRestore
		}
	case "psql":
		if _, ok := pgFlag(l, 'f', "--file", psqlSwitches); ok {
			return kindPsqlFile
		}
		// -c '\i dump.sql' (also \ir, \include): every -c value counts,
		// and an argument holding the meta-command is one
		for _, a := range l.Args {
			if includeRe.MatchString(a) {
				return kindPsqlFile
			}
		}
		if l.Stdin {
			return kindPsqlStdin
		}
		// psql runs what it reads on stdin unless -c or -l gives it
		// something else to do: in a pipeline that is another command's
		// output (gunzip -c x.gz | psql)
		_, cmd := pgFlag(l, 'c', "--command", psqlSwitches)
		_, list := pgFlag(l, 'l', "--list", psqlSwitches)
		if l.Piped && !cmd && !list {
			return kindPsqlStdin
		}
	case "sqlpackage":
		if a, ok := spFlag(l, "a", "action"); ok && strings.EqualFold(a, "import") {
			return "sqlpackage /a:Import"
		}
	case "sqlcmd", "invoke-sqlcmd", "osql":
		if restoreDBRe.MatchString(t) {
			return "RESTORE DATABASE"
		}
	case "supabase":
		if strings.Contains(t, "supabase db reset") && (l.Has("--linked") || l.Has("--db-url")) {
			return "supabase db reset"
		}
	}
	return ""
}

// shortFlag reads a short flag in every libpq tool spelling: -d acme,
// -dacme, and bundled after switches that take no value (-Xf dump.sql,
// -Cd acme, -qfdump.sql). It returns the first value.
func shortFlag(l Leaf, c byte, switches string) (string, bool) {
	for i, a := range l.Args {
		if len(a) < 2 || a[0] != '-' || a[1] == '-' {
			continue
		}
		for j := 1; j < len(a); j++ {
			if a[j] == c {
				if j+1 < len(a) {
					return strings.TrimPrefix(a[j+1:], "="), true
				}
				if i+1 < len(l.Args) {
					return l.Args[i+1], true
				}
				return "", true
			}
			if strings.IndexByte(switches, a[j]) < 0 {
				break
			}
		}
	}
	return "", false
}

// pgFlag reads a libpq tool flag by its long name or its short letter.
func pgFlag(l Leaf, c byte, long, switches string) (string, bool) {
	if v, ok := l.Flag(long); ok {
		return v, true
	}
	return shortFlag(l, c, switches)
}

// conninfoRe spots a keyword connection string (host=… dbname=…).
var conninfoRe = regexp.MustCompile(`(?i)(^|[\s;])(host|hostaddr|service)\s*=`)

// restoreTarget reads where a restore writes.
func restoreTarget(l Leaf, kind string, g packs.GuardCtx) Target {
	var t Target
	extra := extraHosts(g)
	for _, u := range urls(l) {
		t.addURL(u, extra)
	}
	pgHost := func(switches string) {
		if h, ok := pgFlag(l, 'h', "--host", switches); ok {
			if computed(h) {
				t.Unknown = true
			} else {
				for _, x := range strings.Split(h, ",") {
					t.Hosts = append(t.Hosts, x)
					t.Tier1 = append(t.Tier1, hostSignals(x, "")...)
				}
			}
		}
		if d, ok := pgFlag(l, 'd', "--dbname", switches); ok {
			switch {
			case computed(d):
				t.Unknown = true
			case strings.Contains(d, "="):
				t.connInfo(d)
			}
		}
		// psql's dbname argument may be a keyword connection string
		for _, a := range l.Args {
			if conninfoRe.MatchString(a) {
				t.connInfo(a)
			}
		}
		if l.Dotenvs {
			// the file may set PGHOST or PGHOSTADDR, which tq does not read
			t.Unknown = true
		}
		for _, k := range []string{"PGHOST", "PGHOSTADDR"} {
			if v := lookup(l, k); v != "" {
				if computed(v) {
					t.Unknown = true
					continue
				}
				for _, h := range strings.Split(v, ",") {
					t.Hosts = append(t.Hosts, h)
					t.Tier1 = append(t.Tier1, hostSignals(h, "")...)
				}
			}
		}
		if lookup(l, "PGSERVICE") != "" || l.Has("--service") {
			t.Unknown = true
		}
		if l.Unknown {
			t.Unknown = true // an earlier statement may have set PGHOST
		}
		if len(t.Hosts) == 0 {
			t.Hosts = []string{""} // libpq's default: the local socket or localhost
		}
	}
	switch kind {
	case kindPgRestore:
		pgHost(pgRestoreSwitches)
	case kindPsqlFile, kindPsqlStdin:
		pgHost(psqlSwitches)
	case "RESTORE DATABASE":
		named := t.sqlServers(l)
		switch {
		case named:
		case l.Dotenvs || l.Unknown:
			t.Unknown = true // SQLCMDSERVER may come from what tq did not read
		case lookup(l, "SQLCMDSERVER") != "":
			t.serverFlag(lookup(l, "SQLCMDSERVER"))
		default:
			t.Hosts = append(t.Hosts, "localhost")
		}
	case "sqlpackage /a:Import":
		got := false
		if v, ok := spFlag(l, "tsn", "targetservername"); ok {
			t.serverFlag(v)
			got = true
		}
		if v, ok := spFlag(l, "tcs", "targetconnectionstring"); ok {
			if computed(v) {
				t.Unknown = true
			} else {
				t.connInfo(v)
			}
			got = true
		}
		if !got {
			t.Unknown = true
		}
	case "supabase db reset":
		if l.Has("--linked") {
			t.Linked = true
			if ref, ok := linkedRef(l, g); ok && ref != "" {
				t.Tier3 = append(t.Tier3, envgate.Signal{Key: "project_refs", Value: ref})
			}
		}
		if v, ok := l.Flag("--db-url"); ok {
			t.flagValue(v, extra)
		}
	}
	t.Local = !t.Unknown && !t.Linked && t.AllLocal(extra)
	return t
}

// envOwners names the declared environments a restore's identifying
// signals select: dotenv files, project refs and servers. A local host
// never counts: the local server is where the drill and local work restore.
func envOwners(gt envgate.Gate, l Leaf, t Target, extra []string) []string {
	var out []string
	add := func(key, v string) {
		for _, e := range gt.Owners(key, v) {
			out = append(out, e)
		}
	}
	if l.Dotenvs {
		for _, f := range l.Dotenv {
			add("dotenv", f)
		}
	}
	for _, tier := range []envgate.Tier{t.Tier1, t.Tier3, t.Tier4} {
		for _, s := range tier {
			switch s.Key {
			case "project_refs":
				add(s.Key, s.Value)
			case "servers", "hosts":
				if !drill.LocalHost(s.Value, extra) {
					add(s.Key, s.Value)
				}
			}
		}
	}
	return out
}

// RestoreTarget is the tq/restore-target guard (§14): pg_restore, psql -f
// (or psql < dump), sqlpackage /a:Import, RESTORE DATABASE and supabase db
// reset --linked or --db-url are denied when the target resolves to a
// declared environment or to a host that is not local. A restore tq cannot
// read is denied too (fail closed).
func RestoreTarget(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	if !call.IsShell() || strings.TrimSpace(call.Command) == "" {
		return policy.Allow, "", false
	}
	gt := envgate.FromManifest(envgate.DB, g.Manifest(), g.Config())
	extra := extraHosts(g)
	var reasons []string
	deny := func(why, counter string) {
		reasons = append(reasons, IDRestore+": "+why)
		g.Count("restore_denied:"+counter, 1)
	}
	for _, l := range Analyze(call.Command, call.Tool == "PowerShell") {
		kind := restoreKind(l)
		if kind == "" {
			// a pipe into an opaque wrapper (gunzip -c x.gz | ssh box psql
			// acme): the split cut the pipe off the leaf's Raw, and psql reads
			// the pipe unless -c or -l gives it something else to run
			if l.Opaque && (pipedPsql(l) || restoreWordRe.MatchString(l.Raw)) {
				deny("a restore inside a command tq cannot read (a wrapper, a container or remote shell, a script): its target is unknown, so it is denied. Run the restore tool directly against the local server, or use `tq pack db drill`", "unreadable")
			}
			continue
		}
		t := restoreTarget(l, kind, g)
		envs := envOwners(gt, l, t, extra)
		switch {
		case t.Linked:
			deny(kind+" --linked resets the linked Supabase project, a shared database: a human runs it in their own terminal, outside Claude", "linked")
		case len(envs) > 0:
			deny(fmt.Sprintf("%s writes into declared environment %s; tq never restores into a declared environment (use `tq pack db drill`, which restores into a local scratch database)", kind, strings.Join(envs, ", ")), "env")
		case t.Unknown:
			deny(kind+": the target cannot be read (a computed host or connection string, a libpq service, or an environment an earlier statement changed); restore only into a local server named on the command line", "unresolved")
		case !t.Local:
			deny(kind+" targets a host that is not local; restores run only against localhost, a loopback address, a socket or a test_db.hosts service", "non-local")
		}
	}
	if len(reasons) == 0 {
		return policy.Allow, "", false
	}
	return policy.Deny, strings.Join(reasons, "; "), true
}

// sqlServers reads every way a RESTORE DATABASE line names its server:
// sqlcmd/osql -S (also glued, -Sacme), go-sqlcmd --server, Invoke-Sqlcmd
// -ServerInstance and -ConnectionString in any case and any unambiguous
// abbreviation (-Server, -SERVERINSTANCE:x), and Server= / Data Source=
// fragments anywhere on the line. It reports whether one was named.
func (t *Target) sqlServers(l Leaf) bool {
	named := false
	ps := l.Prog == "invoke-sqlcmd"
	for i, a := range l.Args {
		if len(a) < 2 || a[0] != '-' {
			continue
		}
		next := ""
		if i+1 < len(l.Args) {
			next = l.Args[i+1]
		}
		if ps {
			name, val, colon := strings.Cut(strings.ToLower(a[1:]), ":")
			if colon {
				_, val, _ = strings.Cut(a, ":") // keep the value's case
				next = val
			}
			switch {
			case len(name) >= 2 && strings.HasPrefix("serverinstance", name):
				t.serverFlag(next)
				named = true
			case len(name) >= 4 && strings.HasPrefix("connectionstring", name):
				if computed(next) {
					t.Unknown = true
				} else {
					t.connInfo(next)
				}
				named = true
			}
			continue
		}
		switch {
		case a == "-S" || a == "--server":
			t.serverFlag(next)
			named = true
		case strings.HasPrefix(a, "--server="):
			t.serverFlag(strings.TrimPrefix(a, "--server="))
			named = true
		case len(a) > 2 && a[1] == 'S':
			t.serverFlag(a[2:]) // sqlcmd's flags are one letter: -Sacme
			named = true
		}
	}
	before := len(t.Hosts)
	t.serverFragments(l.Raw)
	return named || len(t.Hosts) > before
}

// spFlag reads a sqlpackage parameter in any of its spellings: /a:Import,
// -a:Import, /Action:Import, /a Import (names lower case, without the
// prefix).
func spFlag(l Leaf, names ...string) (string, bool) {
	for i, a := range l.Args {
		if len(a) < 2 || (a[0] != '/' && a[0] != '-') {
			continue
		}
		k, v, colon := strings.Cut(strings.TrimLeft(a, "/-"), ":")
		for _, n := range names {
			if !strings.EqualFold(k, n) {
				continue
			}
			if colon {
				return v, true
			}
			if i+1 < len(l.Args) {
				return l.Args[i+1], true
			}
			return "", true
		}
	}
	return "", false
}
