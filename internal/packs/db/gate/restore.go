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
var restoreWordRe = regexp.MustCompile(`(?i)\b(pg_restore\b|sqlpackage\b[^\n]*[/-]a(ction)?:\s*import|restore\s+database\b|supabase\s+db\s+reset\b[^\n]*--(linked|db-url)|psql\b[^\n]*(\s-f|\s--file|<))`)

// restoreKind names what a leaf restores with, or "".
func restoreKind(l Leaf) string {
	t := l.Text()
	switch l.Prog {
	case "pg_restore":
		// without -d it only prints the SQL script
		if l.Has("-d", "--dbname") {
			return "pg_restore"
		}
	case "psql":
		if l.Has("-f", "--file") || l.Stdin || hasShortFlag(l, 'f') {
			return "psql -f"
		}
	case "sqlpackage":
		if a, ok := spFlag(l, "a", "action"); ok && strings.EqualFold(a, "import") {
			return "sqlpackage /a:Import"
		}
	case "sqlcmd", "invoke-sqlcmd", "osql":
		if strings.Contains(t, "restore database") {
			return "RESTORE DATABASE"
		}
	case "supabase":
		if strings.Contains(t, "supabase db reset") && (l.Has("--linked") || l.Has("--db-url")) {
			return "supabase db reset"
		}
	}
	return ""
}

// hasShortFlag spots a bundled short flag (psql -Xf dump.sql, -qfdump.sql):
// every letter before it is a psql switch that takes no value.
func hasShortFlag(l Leaf, c byte) bool {
	const switches = "aAbeEHlnqsStxXz0"
	for _, a := range l.Args {
		if len(a) < 3 || a[0] != '-' || a[1] == '-' {
			continue
		}
		for i := 1; i < len(a); i++ {
			if a[i] == c {
				return true
			}
			if strings.IndexByte(switches, a[i]) < 0 {
				break
			}
		}
	}
	return false
}

// restoreTarget reads where a restore writes.
func restoreTarget(l Leaf, kind string, g packs.GuardCtx) Target {
	var t Target
	extra := extraHosts(g)
	for _, u := range urls(l) {
		t.addURL(u, extra)
	}
	pgHost := func() {
		if h, ok := l.Flag("-h", "--host"); ok {
			if computed(h) {
				t.Unknown = true
			} else {
				t.Hosts = append(t.Hosts, h)
				t.Tier1 = append(t.Tier1, hostSignals(h, "")...)
			}
		}
		if d, ok := l.Flag("-d", "--dbname"); ok {
			switch {
			case computed(d):
				t.Unknown = true
			case strings.Contains(d, "="):
				t.connInfo(d)
			}
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
	case "pg_restore", "psql -f":
		pgHost()
	case "RESTORE DATABASE":
		if v, ok := l.Flag("-S", "-ServerInstance", "-serverinstance"); ok {
			t.serverFlag(v)
		} else if v := lookup(l, "SQLCMDSERVER"); v != "" {
			t.serverFlag(v)
		} else if l.Unknown {
			t.Unknown = true
		} else {
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
			if l.Opaque && restoreWordRe.MatchString(l.Raw) {
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
