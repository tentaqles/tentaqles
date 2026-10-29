package db

// The db pack's doctor probes and detection hints (spec D §19, workstream
// D-OI). tq doctor and tq pack doctor call them through Pack.Doctor; F's
// packs-suggest evaluates the hints of a disabled pack.
//
// Codes:
//   db-env-unresolvable, db-env-overlap   the environments block (shared
//                                         with the cloud pack: cloud.EnvFindings)
//   db-supabase-ref-undeclared            the linked Supabase project is in
//                                         no environment's match.project_refs
//   db-test-db-unset                      test_db's variable is not set
//   db-test-db-invalid                    the block is invalid, or its connection string does not parse
//   db-test-db-not-local                  it points at a non-local host
//   db-test-db-down                       the local test database does not answer
//   db-drill-invalid, db-drill-log-unreadable, db-drill-stale
//                                         each drill's age against its freshness
//                                         (decided at spec review: doctor warns)
//
// The test_db connection string is resolved the way the pgtap check does,
// and never printed. Only a local host is probed (a 2 s TCP connect): a
// non-local target is refused before anything connects, and a
// docker-compose service name is not dialled (it would need a DNS lookup).

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud"
	"github.com/tentaqles/tentaqles/internal/packs/db/drill"
	"github.com/tentaqles/tentaqles/internal/packs/db/pgtap"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
)

// migrationDirs are the folders of MigrationDirRE (plugin gate.py
// MIGRATION_DIR_RE); supabase/, prisma/ and sql/migrations fall under
// migrations, db/migrate under migrate.
var migrationDirs = []string{"migrations", "migrate", "alembic/versions", "flyway"}

// Hints are the db pack's detection hints (§19, §22 E): a file under a
// migration folder.
var Hints = func() []packs.Hint {
	var out []packs.Hint
	for _, d := range migrationDirs {
		out = append(out, packs.Hint{Glob: "**/" + d + "/**/*", Why: "database migrations"})
	}
	return out
}()

func init() {
	packs.RegisterPack(packs.Pack{Name: "db", Hints: Hints, Doctor: Doctor})
}

// Doctor is the db pack's Pack.Doctor.
func Doctor(in *packs.Input) []doctor.Finding {
	d := &probe{ws: in.Workspace(), out: cloud.EnvFindings("db", envgate.DB, in)}
	d.supabaseRef(in)
	d.testDB(in)
	d.drills(in)
	return d.out
}

type probe struct {
	ws  string
	out []doctor.Finding
}

func (d *probe) add(level, code, msg, fix string) {
	d.out = append(d.out, doctor.Finding{Level: level, Code: code, Workspace: d.ws, Msg: msg, Fix: fix})
}

// supabaseRef: the project the Supabase CLI is linked to belongs to a
// declared environment (§14), so a migration to it resolves to that
// environment and tq pack db drill accepts it. The ref is never printed.
func (d *probe) supabaseRef(in *packs.Input) {
	if in.Root == "" {
		return
	}
	ref := drill.LinkedRef(in.Root, os.Getenv)
	if ref == "" {
		return
	}
	gt := envgate.FromManifest(envgate.DB, in.Manifest, in.Config)
	if len(gt.Owners("project_refs", ref)) > 0 {
		return
	}
	msg := "the linked Supabase project (SUPABASE_PROJECT_ID or supabase/.temp/project-ref) is not listed in any environment's match.project_refs: a migration to it counts as the default environment, and tq pack db drill refuses it"
	if len(gt.Envs()) == 0 {
		msg = "a Supabase project is linked (SUPABASE_PROJECT_ID or supabase/.temp/project-ref) but packs.db declares no environments: migrations to it are gated as one unnamed environment"
	}
	d.add("warn", "db-supabase-ref-undeclared", msg, "list the project ref under packs.db.environments.<env>.match.project_refs in the manifest")
}

// testDB: packs.db.test_db is set, local, and answers (§12).
func (d *probe) testDB(in *packs.Input) {
	s := in.Settings()
	if n := s.Node("test_db"); n != nil {
		// pgtap.Settings is nil for an invalid block too: say which it is
		if err := pgtap.ValidateTestDB(*n); err != nil {
			d.add("error", "db-test-db-invalid", "packs.db.test_db is invalid ("+err.Error()+"): db/pgtap has no test database",
				"fix packs.db.test_db in the manifest: {url_env, start, hosts, dotenv}")
			return
		}
	}
	cfg := pgtap.Settings(s)
	if cfg == nil || cfg.URLEnv == "" {
		switch {
		case s.Declared("pg_prove") && cfg == nil:
			d.add("warn", "db-test-db-unset", "pg_prove is declared but packs.db.test_db is not set: db/pgtap fails without a local test database",
				"set packs.db.test_db.url_env (and start) in the manifest")
		case supabaseMode(in):
			port := pgtap.SupabaseDBPort
			if c := readConfig(in); c != nil && c.DBPort > 0 {
				port = c.DBPort
			}
			if !answers("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) {
				d.add("info", "db-test-db-down", fmt.Sprintf("the local Supabase database (127.0.0.1:%d) does not answer: db/pgtap starts it only through test_db.start", port),
					"run supabase start, or set packs.db.test_db.start")
			}
		}
		return
	}
	v := pgtap.ResolveURL(in.Root, *cfg)
	if v == "" {
		d.add("warn", "db-test-db-unset", cfg.URLEnv+" is not set (environment or the test_db dotenv files): db/pgtap has no test database",
			"point "+cfg.URLEnv+" at a local test database")
		return
	}
	t, err := pgtap.ParseTarget(v)
	if err != nil || len(t.Hosts) == 0 {
		d.add("error", "db-test-db-invalid", "the test_db connection string ("+cfg.URLEnv+") cannot be parsed (postgres://… or key=value form)", "fix "+cfg.URLEnv)
		return
	}
	if t.Local(cfg.Hosts) != nil {
		d.add("error", "db-test-db-not-local", cfg.URLEnv+" points at a host that is not local: tq runs the pgTAP tests only against localhost, a loopback address, a Unix socket or a service in test_db.hosts",
			"point "+cfg.URLEnv+" at a local or ephemeral database")
		return
	}
	h := t.Hosts[0]
	network, addr := "tcp", net.JoinHostPort(h.Host, strconv.Itoa(h.Port))
	switch {
	case h.Socket():
		network, addr = "unix", filepath.ToSlash(filepath.Join(h.Host, ".s.PGSQL."+strconv.Itoa(h.Port)))
	case !loopback(h.Host):
		return // a docker-compose service: not dialled from here
	}
	if !answers(network, addr) {
		fix := "start it, or set packs.db.test_db.start"
		if cfg.Start != "" {
			fix = "db/pgtap runs test_db.start first; run it yourself to check it works"
		}
		d.add("warn", "db-test-db-down", "the test database ("+cfg.URLEnv+", "+h.String()+") does not answer a 2 s probe", fix)
	}
}

func loopback(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func answers(network, addr string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), pgtap.ProbeTimeout)
	defer cancel()
	return pgtap.Dial(ctx, network, addr) == nil
}

// drills: each declared drill has a fresh successful line in the evidence
// log (§13.6). The check blocks the DoD only when a migration changed;
// doctor warns either way. The drill_age_days gauge is written by the check.
func (d *probe) drills(in *packs.Input) {
	ds, err := drill.Parse(in.Settings())
	if err != nil {
		d.add("error", "db-drill-invalid", "packs.db.drill is invalid: "+err.Error(), "fix packs.db.drill in the manifest")
		return
	}
	if len(ds) == 0 || in.Root == "" {
		return
	}
	raw, err := drill.ReadLog(in.Root)
	if err != nil {
		d.add("warn", "db-drill-log-unreadable", drill.EvidenceFile+" cannot be read: "+err.Error(), "restore the evidence log from git")
		return
	}
	entries, _ := drill.ParseLog(raw)
	now := drill.Now()
	for _, dr := range ds {
		if f := drill.Check(entries, dr, now); !f.Fresh {
			d.add("warn", "db-drill-stale", "drill "+dr.Name+": "+f.Why, drill.StaleText(dr.Name))
		}
	}
}
