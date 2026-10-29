package drill

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/dotenv"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/lint"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/redact"
	"gopkg.in/yaml.v3"
)

// TestDB is the part of packs.db.test_db the drill reads (the key itself is
// D-DR's; tq connects only to the variable named by url_env).
type TestDB struct {
	URLEnv string
	Hosts  []string // docker-compose services that count as local
	Dotenv []string // dotenv files url_env is looked up in (default .env)
}

// ReadTestDB decodes packs.db.test_db; ok false when it is unset.
func ReadTestDB(s packs.Settings) (TestDB, bool) {
	n := s.Node("test_db")
	if n == nil || n.Kind != yaml.MappingNode {
		return TestDB{}, false
	}
	var t TestDB
	t.URLEnv = s.String("test_db.url_env")
	t.Hosts = s.Strings("test_db.hosts")
	t.Dotenv = s.Strings("test_db.dotenv")
	return t, true
}

// Options configure one drill run.
type Options struct {
	In    *packs.Input // bound to the db pack, with a workspace
	Drill Drill
	Keep  bool      // keep the dump files (never the scratch database)
	Out   io.Writer // progress: names and numbers only
	// Getenv looks a variable up in the process environment (tests replace
	// it); the dotenv files come second, as `tq dotenv run` loads them.
	Getenv func(string) string
}

// Outcome is a drill run's result.
type Outcome struct {
	Line    Line
	Scratch string
	Kept    string // the dump folder, with --keep
	Log     string // the tool output log (0600, under the state dir)
}

// ErrNotLocal is a drill target that is not a local server.
var ErrNotLocal = errors.New("the drill restores only into a scratch database on the local test_db server: test_db must point at localhost, a loopback address, a Unix socket or a service listed in test_db.hosts; tq never restores into a shared or declared environment's server")

var nowFn = func() time.Time { return time.Now().UTC() }

func (o Options) lookup(name string, files []string) string {
	get := o.Getenv
	if get == nil {
		get = os.Getenv
	}
	if v := get(name); v != "" {
		return v
	}
	if len(files) == 0 {
		files = []string{".env"}
	}
	v := ""
	for _, f := range files {
		f = strings.ReplaceAll(strings.TrimSpace(f), `\`, "/")
		if f == "" || strings.HasPrefix(f, "/") || (len(f) > 1 && f[1] == ':') || strings.Contains("/"+f+"/", "/../") || !dotenv.IsEnvFile(f) {
			continue
		}
		if x := dotenv.Lookup(filepath.Join(o.In.Root, filepath.FromSlash(f)), name); x != "" {
			v = x
		}
	}
	return v
}

// run is one drill's state.
type run struct {
	o       Options
	ctx     context.Context
	conn    Conn
	tdb     TestDB
	scratch string
	tmp     string
	log     *os.File
	logPath string
	tools   map[string]string
	created bool
	bakTime time.Time // a .bak's BackupFinishDate
}

// Target resolves and checks the drill's target before anything runs: the
// test_db connection, which must be local and of the drill's engine.
func Target(o Options) (Conn, TestDB, error) {
	tdb, ok := ReadTestDB(o.In.Settings())
	if !ok || tdb.URLEnv == "" {
		return Conn{}, tdb, errors.New("packs.db.test_db.url_env is not set: the drill needs a local test database to restore into")
	}
	url := o.lookup(tdb.URLEnv, tdb.Dotenv)
	if url == "" {
		return Conn{}, tdb, fmt.Errorf("%s is not set (environment or the test_db dotenv files); point it at the local test database", tdb.URLEnv)
	}
	c, err := ParseConn(url)
	if err != nil {
		return Conn{}, tdb, fmt.Errorf("%s: %w", tdb.URLEnv, err)
	}
	if c.Engine != o.Drill.Engine {
		return Conn{}, tdb, fmt.Errorf("drill %s is a %s drill, but %s is a %s connection", o.Drill.Name, o.Drill.Engine, tdb.URLEnv, c.Engine)
	}
	if !c.Local(tdb.Hosts) {
		return Conn{}, tdb, ErrNotLocal
	}
	return c, tdb, nil
}

// Run runs one drill: source, scratch database, restore, verify, evidence
// line, drop. A refusal (non-local target, a source that is not declared
// readable) is an error and records nothing; a failed restore or verify
// records an ok:false line.
func Run(ctx context.Context, o Options) (Outcome, error) {
	if o.Out == nil {
		o.Out = io.Discard
	}
	sd := o.In.StateDir()
	if sd == "" || o.In.Workspace() == "" {
		return Outcome{}, errors.New("the drill needs a trusted workspace")
	}
	conn, tdb, err := Target(o)
	if err != nil {
		return Outcome{}, err
	}
	start := nowFn()
	r := &run{o: o, ctx: ctx, conn: conn, tdb: tdb, scratch: ScratchName(o.Drill.Name, start), tools: map[string]string{}}
	r.tmp = filepath.Join(sd, "drills", "tmp", r.scratch)
	if err := os.MkdirAll(r.tmp, 0o700); err != nil {
		return Outcome{}, err
	}
	logDir := filepath.Join(sd, "drills", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return Outcome{}, err
	}
	r.logPath = filepath.Join(logDir, r.scratch+".log")
	if r.log, err = os.OpenFile(r.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err != nil {
		return Outcome{}, err
	}
	defer r.log.Close()
	out := Outcome{Scratch: r.scratch, Log: r.logPath}
	defer func() {
		if o.Keep {
			out.Kept = r.tmp
			return
		}
		os.RemoveAll(r.tmp)
	}()

	line := Line{Name: o.Drill.Name, Engine: o.Drill.Engine, Source: o.Drill.Source.Kind, HostKind: "local", Tables: []Table{}}
	fail := func(step string) {
		line.OK, line.FailedStep = false, step
	}
	src, err := r.source(&line)
	switch {
	case errors.Is(err, errRefused):
		return out, err
	case err != nil:
		fmt.Fprintf(r.log, "source: %v\n", r.mask(err.Error()))
		fail("source")
	}
	if line.FailedStep == "" {
		if err := r.createAndRestore(src); err != nil {
			fmt.Fprintf(r.log, "%v\n", r.mask(err.Error()))
			var se stepErr
			if errors.As(err, &se) {
				fail(se.step)
			} else {
				fail("restore")
			}
		}
	}
	if line.FailedStep == "" {
		line.OK = true
		if !r.verify(&line) {
			fail("verify")
		}
	}
	if r.created {
		if err := r.drop(); err != nil {
			fmt.Fprintf(r.log, "drop: %v\n", r.mask(err.Error()))
			fmt.Fprintf(o.Out, "warning: the scratch database %s could not be dropped; drop it by hand on the local server\n", r.scratch)
		}
	}
	end := nowFn()
	line.RestoredAt = end.Format(time.RFC3339)
	line.DurationS = int64(end.Sub(start).Seconds())
	if line.BackupTakenAt == "" {
		// no backup time: the line cannot prove freshness (fail closed)
		line.BackupTakenAt, line.BackupTimeFrom = time.Unix(0, 0).UTC().Format(time.RFC3339), "unknown"
		if line.OK {
			fail("backup-time")
		}
	}
	line.ToolVersions = r.tools
	if err := Append(o.In.Root, line); err != nil {
		return out, fmt.Errorf("the evidence line could not be written: %w", err)
	}
	if line.OK {
		packs.Count("db", "drill:ok", 1)
	} else {
		packs.Count("db", "drill:fail", 1)
	}
	out.Line = line
	return out, nil
}

var errRefused = errors.New("refused")

type stepErr struct {
	step string
	err  error
}

func (e stepErr) Error() string { return e.step + ": " + e.err.Error() }
func (e stepErr) Unwrap() error { return e.err }

// mask removes the connection's password from text written to the log.
func (r *run) mask(s string) string {
	if r.conn.Password != "" {
		s = strings.ReplaceAll(s, r.conn.Password, "[REDACTED:test_db]")
	}
	return redact.Redact(s, r.o.In.Classes())
}

// tool runs one external tool and logs its output (never printed).
func (r *run) tool(name string, args []string, env []string, pass []string) (toolrun.Output, error) {
	p := r.o.In.Tools.Probe("db", name)
	if !p.Found {
		return toolrun.Output{}, fmt.Errorf("%s is not installed (or not on PATH)", name)
	}
	if rl, ok := r.o.In.Tools.(*toolrun.Runner); ok && rl.RepoLocal(p) && r.conn.Password != "" && carriesPassword(env, args, r.conn.Password) {
		return toolrun.Output{}, fmt.Errorf("%s resolves inside the repository; tq does not pass the test_db password to it. Install it outside the repository or set packs.db.tool_paths.%s in the manifest", name, name)
	}
	if p.Version != "" {
		r.tools[name] = p.Version
	} else if _, ok := r.tools[name]; !ok {
		r.tools[name] = "unknown"
	}
	out, err := r.o.In.Tools.Run(r.ctx, toolrun.Spec{Pack: "db", Tool: name, Args: args, Env: env, PassEnv: pass, Dir: r.o.In.Root, Timeout: r.o.Drill.Timeout})
	fmt.Fprintf(r.log, "$ %s (exit %d, %s)\n%s\n%s\n", name, out.ExitCode, out.Duration.Round(time.Millisecond), r.mask(string(out.Stdout)), r.mask(string(out.Stderr)))
	switch {
	case err != nil:
		return out, err
	case out.TimedOut:
		return out, fmt.Errorf("%s timed out after %s", name, r.o.Drill.Timeout)
	case out.ExitCode != 0:
		return out, fmt.Errorf("%s exited %d", name, out.ExitCode)
	}
	return out, nil
}

func carriesPassword(env, args []string, pw string) bool {
	for _, kv := range append(append([]string(nil), env...), args...) {
		if strings.Contains(kv, pw) {
			return true
		}
	}
	return false
}

// ---- source ---------------------------------------------------------------

// dump is what the restore reads.
type dump struct {
	files  []string // in restore order (supabase: roles, schema, data)
	format DumpFormat
	bak    bool // a SQL Server .bak (RESTORE DATABASE)
	bacpac bool
}

func (r *run) source(line *Line) (dump, error) {
	d := r.o.Drill
	switch d.Source.Kind {
	case KindFile, KindBacpac:
		p := r.o.lookup(d.Source.PathEnv, r.tdb.Dotenv)
		if p == "" {
			return dump{}, fmt.Errorf("%w: %s is not set (environment or the test_db dotenv files)", errRefused, d.Source.PathEnv)
		}
		if d.Engine == SQLServer && d.Source.Kind == KindFile {
			// a .bak is read by the server (often a container path)
			line.BackupTimeFrom = "backup-header"
			return dump{files: []string{p}, bak: true}, nil
		}
		return r.localDump(p, d.Source.Kind == KindBacpac, line)
	case KindCommand:
		outFile := filepath.Join(r.tmp, "dump")
		o, err := r.tool(d.Source.Command[0], d.Source.Command[1:], []string{"TQ_DRILL_OUT=" + outFile}, nil)
		if err != nil {
			return dump{}, err
		}
		p := outFile
		if _, err := os.Stat(p); err != nil {
			lines := strings.Split(strings.TrimSpace(string(o.Stdout)), "\n")
			p = strings.TrimSpace(lines[len(lines)-1])
		}
		if p == "" || !filepath.IsAbs(p) {
			return dump{}, errors.New("the fetch command printed no absolute dump path and wrote nothing to $TQ_DRILL_OUT")
		}
		return r.localDump(p, d.Engine == SQLServer, line)
	case KindSupabase:
		if err := r.linkedEnv(); err != nil {
			return dump{}, err
		}
		roles, schema, data := filepath.Join(r.tmp, "roles.sql"), filepath.Join(r.tmp, "schema.sql"), filepath.Join(r.tmp, "data.sql")
		// supabase db dump dumps the schema only by default: the data and the
		// roles need their own dumps (--data-only, --role-only).
		for _, args := range [][]string{
			{"db", "dump", "--linked", "--role-only", "-f", roles},
			{"db", "dump", "--linked", "-f", schema},
			{"db", "dump", "--linked", "--data-only", "-f", data},
		} {
			if _, err := r.tool("supabase", args, nil, supabasePass); err != nil {
				return dump{}, err
			}
		}
		for _, f := range []string{roles, schema, data} {
			os.Chmod(f, 0o600)
		}
		line.BackupTakenAt, line.BackupTimeFrom = nowFn().Format(time.RFC3339), "dump-time"
		return dump{files: []string{roles, schema, data}, format: DumpPlain}, nil
	}
	return dump{}, fmt.Errorf("unknown source kind %q", d.Source.Kind)
}

// supabasePass are the variables supabase db dump reads (its access token
// and database password, the project id, Docker for the pg_dump container).
var supabasePass = []string{"SUPABASE_ACCESS_TOKEN", "SUPABASE_DB_PASSWORD", "SUPABASE_PROJECT_ID", "SUPABASE_PROFILE",
	"DOCKER_HOST", "DOCKER_CONFIG", "DOCKER_CONTEXT", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME",
	"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR"}

var refRe = regexp.MustCompile(`^[a-z0-9]{20}$`)

// LinkedRef is the Supabase project ref the CLI would use: SUPABASE_PROJECT_ID,
// then supabase/.temp/project-ref (the CLI's own order after --project-ref).
func LinkedRef(root string, getenv func(string) string) string {
	if v := strings.TrimSpace(getenv("SUPABASE_PROJECT_ID")); v != "" {
		return v
	}
	b, err := os.ReadFile(filepath.Join(root, "supabase", ".temp", "project-ref"))
	if err != nil || len(b) > 256 {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// linkedEnv checks that the linked project is a declared environment (§13.1:
// the --linked target is resolved and must match an environment).
func (r *run) linkedEnv() error {
	get := r.o.Getenv
	if get == nil {
		get = os.Getenv
	}
	ref := LinkedRef(r.o.In.Root, get)
	if ref == "" || !refRe.MatchString(ref) {
		return fmt.Errorf("%w: the linked Supabase project cannot be resolved (supabase link, or SUPABASE_PROJECT_ID); the drill dumps only a project a declared environment lists in match.project_refs", errRefused)
	}
	g := envgate.FromManifest(envgate.DB, r.o.In.Manifest, r.o.In.Config)
	if len(g.Owners("project_refs", ref)) == 0 {
		return fmt.Errorf("%w: the linked Supabase project is not listed in any environment's match.project_refs; declare it before dumping it", errRefused)
	}
	return nil
}

// localDump checks a dump file and reads its backup time.
func (r *run) localDump(p string, bacpac bool, line *Line) (dump, error) {
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() {
		return dump{}, errors.New("the dump file does not exist or is not a regular file")
	}
	line.BackupTakenAt, line.BackupTimeFrom = st.ModTime().UTC().Format(time.RFC3339), "file-mtime"
	if bacpac {
		return dump{files: []string{p}, bacpac: true}, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return dump{}, err
	}
	head := make([]byte, 4096)
	n, _ := io.ReadFull(f, head)
	f.Close()
	format, at, ok := PGDumpHeader(head[:n])
	if format == DumpUnknown {
		return dump{}, errors.New("the dump is neither a pg_dump archive nor a plain SQL dump")
	}
	if ok {
		line.BackupTakenAt, line.BackupTimeFrom = at.Format(time.RFC3339), "dump-header"
	}
	return dump{files: []string{p}, format: format}, nil
}

// ---- postgres -------------------------------------------------------------

func (r *run) pgEnv() []string {
	env := []string{"PGCONNECT_TIMEOUT=10", "PGAPPNAME=tq-drill"}
	if h := strings.Join(r.conn.Hosts, ","); h != "" {
		env = append(env, "PGHOST="+h)
	}
	if r.conn.HostAddr != "" {
		env = append(env, "PGHOSTADDR="+r.conn.HostAddr)
	}
	if r.conn.Port != "" {
		env = append(env, "PGPORT="+r.conn.Port)
	}
	if r.conn.User != "" {
		env = append(env, "PGUSER="+r.conn.User)
	}
	if r.conn.Password != "" {
		env = append(env, "PGPASSWORD="+r.conn.Password)
	}
	return env
}

func (r *run) adminDB() string {
	if r.o.Drill.Engine == SQLServer {
		return "master"
	}
	if r.conn.Database != "" && r.conn.Database != r.scratch {
		return r.conn.Database
	}
	return "postgres"
}

func (r *run) psql(db string, extra ...string) (toolrun.Output, error) {
	args := append([]string{"-X", "-q", "-v", "ON_ERROR_STOP=1", "-d", db}, extra...)
	return r.tool("psql", args, r.pgEnv(), nil)
}

// ---- sql server -----------------------------------------------------------

func (r *run) msServer() string {
	s := r.conn.Host()
	if s == "" {
		s = "localhost"
	}
	if r.conn.Port != "" {
		s += "," + r.conn.Port
	}
	return s
}

func (r *run) sqlcmd(db, query string) (toolrun.Output, error) {
	args := []string{"-S", r.msServer(), "-d", db, "-b", "-h", "-1", "-W", "-s", "|", "-C", "-Q", "SET NOCOUNT ON; " + query}
	env := []string{}
	if r.conn.User != "" {
		args = append(args, "-U", r.conn.User)
	} else {
		args = append(args, "-E")
	}
	if r.conn.Password != "" {
		env = append(env, "SQLCMDPASSWORD="+r.conn.Password)
	}
	return r.tool("sqlcmd", args, env, nil)
}

func msQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }

// ---- create, restore, verify, drop ------------------------------------------

func (r *run) createAndRestore(d dump) error {
	switch {
	case r.o.Drill.Engine == Postgres:
		if _, err := r.psql(r.adminDB(), "-c", "CREATE DATABASE "+r.scratch); err != nil {
			return stepErr{"create", err}
		}
		r.created = true
		if d.format == DumpArchive {
			if _, err := r.tool("pg_restore", []string{"--no-owner", "--no-acl", "-d", r.scratch, d.files[0]}, r.pgEnv(), nil); err != nil {
				return stepErr{"restore", err}
			}
			return nil
		}
		for i, f := range d.files {
			if len(d.files) == 3 && i == 0 {
				// roles are cluster-wide: one that exists already is not a failure
				if _, err := r.tool("psql", []string{"-X", "-q", "-d", r.scratch, "-f", f}, r.pgEnv(), nil); err != nil && !strings.Contains(err.Error(), "exited") {
					return stepErr{"restore", err}
				}
				continue
			}
			if _, err := r.psql(r.scratch, "-f", f); err != nil {
				return stepErr{"restore", err}
			}
		}
		return nil
	case d.bacpac:
		// sqlpackage creates the database itself
		args := []string{"/a:Import", "/sf:" + d.files[0], "/tsn:" + r.msServer(), "/tdn:" + r.scratch, "/ttsc:True"}
		if r.conn.User != "" {
			args = append(args, "/tu:"+r.conn.User)
		}
		if r.conn.Password != "" {
			args = append(args, "/tp:"+r.conn.Password)
		}
		r.created = true
		if _, err := r.tool("sqlpackage", args, nil, nil); err != nil {
			return stepErr{"restore", err}
		}
		return nil
	case d.bak:
		return r.restoreBak(d.files[0])
	}
	return stepErr{"restore", errors.New("the dump cannot be restored")}
}

var bakTimeRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)

// restoreBak restores a SQL Server .bak with MOVE into the server's default
// data and log folders.
func (r *run) restoreBak(p string) error {
	q := msQuote(p)
	head, err := r.sqlcmd("master", "RESTORE HEADERONLY FROM DISK = N'"+q+"'")
	if err != nil {
		return stepErr{"source", err}
	}
	// HEADERONLY column 19 is BackupFinishDate (server local time)
	for _, l := range strings.Split(string(head.Stdout), "\n") {
		cols := strings.Split(strings.TrimSpace(l), "|")
		if len(cols) > 18 {
			if m := bakTimeRe.FindString(cols[18]); m != "" {
				if t, err := time.ParseInLocation("2006-01-02 15:04:05", m, time.Local); err == nil {
					r.bakTime = t.UTC()
				}
			}
			break
		}
	}
	files, err := r.sqlcmd("master", "RESTORE FILELISTONLY FROM DISK = N'"+q+"'")
	if err != nil {
		return stepErr{"source", err}
	}
	dirs, err := r.sqlcmd("master", "SELECT CAST(SERVERPROPERTY('InstanceDefaultDataPath') AS nvarchar(400)) + '|' + CAST(SERVERPROPERTY('InstanceDefaultLogPath') AS nvarchar(400))")
	if err != nil {
		return stepErr{"create", err}
	}
	dl := strings.Split(strings.TrimSpace(firstLine(dirs.Stdout)), "|")
	if len(dl) < 2 || dl[0] == "" {
		return stepErr{"create", errors.New("the server's default data folder is unknown")}
	}
	var moves []string
	for i, l := range strings.Split(string(files.Stdout), "\n") {
		cols := strings.Split(strings.TrimSpace(l), "|")
		if len(cols) < 3 || cols[0] == "" {
			continue
		}
		dir, ext := dl[0], ".mdf"
		if strings.EqualFold(strings.TrimSpace(cols[2]), "L") {
			dir, ext = dl[1], ".ldf"
		}
		moves = append(moves, fmt.Sprintf("MOVE N'%s' TO N'%s%s_%d%s'", msQuote(strings.TrimSpace(cols[0])), msQuote(dir), r.scratch, i, ext))
	}
	if len(moves) == 0 {
		return stepErr{"source", errors.New("the backup lists no files")}
	}
	r.created = true
	if _, err := r.sqlcmd("master", "RESTORE DATABASE ["+r.scratch+"] FROM DISK = N'"+q+"' WITH "+strings.Join(moves, ", ")+", RECOVERY"); err != nil {
		return stepErr{"restore", err}
	}
	return nil
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func (r *run) scalar(query string) (string, error) {
	if r.o.Drill.Engine == SQLServer {
		o, err := r.sqlcmd(r.scratch, query)
		if err != nil {
			return "", err
		}
		return firstLine(o.Stdout), nil
	}
	o, err := r.psql(r.scratch, "-A", "-t", "-c", query)
	if err != nil {
		return "", err
	}
	return firstLine(o.Stdout), nil
}

func pgIdent(t string) string {
	parts := strings.Split(t, ".")
	for i, p := range parts {
		parts[i] = `"` + p + `"`
	}
	return strings.Join(parts, ".")
}

func msIdent(t string) string {
	parts := strings.Split(t, ".")
	for i, p := range parts {
		parts[i] = "[" + p + "]"
	}
	return strings.Join(parts, ".")
}

// verify records the counts and the migration version; false when a count
// fails, a verified table is empty or the schema did not load.
func (r *run) verify(line *Line) bool {
	ok := true
	count := func(q string) (int64, bool) {
		v, err := r.scalar(q)
		if err != nil {
			fmt.Fprintf(r.log, "verify: %v\n", r.mask(err.Error()))
			return 0, false
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n, err == nil
	}
	if r.o.Drill.Engine == SQLServer {
		line.Schema.Tables, _ = count("SELECT COUNT(*) FROM sys.tables WHERE is_ms_shipped = 0")
		line.Schema.Indexes, _ = count("SELECT COUNT(*) FROM sys.indexes i JOIN sys.tables t ON t.object_id = i.object_id WHERE t.is_ms_shipped = 0 AND i.index_id > 0")
	} else {
		sys := "('pg_catalog','information_schema','pg_toast')"
		line.Schema.Tables, _ = count("select count(*) from pg_tables where schemaname not in " + sys)
		line.Schema.Indexes, _ = count("select count(*) from pg_indexes where schemaname not in " + sys)
	}
	if line.Schema.Tables == 0 {
		ok = false
	}
	for _, t := range r.o.Drill.Verify {
		q := "select count(*) from " + pgIdent(t)
		if r.o.Drill.Engine == SQLServer {
			q = "SELECT COUNT_BIG(*) FROM " + msIdent(t)
		}
		n, good := count(q)
		line.Tables = append(line.Tables, Table{Name: t, Count: n})
		if !good || n == 0 {
			ok = false
		}
	}
	r.version(line)
	if r.bakTime.After(time.Time{}) && line.BackupTimeFrom == "backup-header" {
		line.BackupTakenAt = r.bakTime.Format(time.RFC3339)
	}
	return ok
}

// version reads the migration version table and compares it with the
// newest migration file of the engine's migrations entry.
func (r *run) version(line *Line) {
	line.MigrationVersion = "unknown"
	var m lint.Migration
	found := false
	for _, x := range lint.Migrations(r.o.In.Settings()) {
		if x.Engine == r.o.Drill.Engine {
			m, found = x, true
			break
		}
	}
	if !found {
		return
	}
	if q := VersionQuery(r.o.Drill.Engine, m.Migrator); q != "" {
		if v, err := r.scalar(q); err == nil && v != "" {
			line.MigrationVersion = NormVersion(m.Migrator, v)
		}
	}
	line.MigrationFiles = NewestFileVersion(m.Migrator, listNames(filepath.Join(r.o.In.Root, filepath.FromSlash(m.Path))))
	if line.MigrationFiles != "" && line.MigrationVersion != line.MigrationFiles {
		line.MigrationGap = true
	}
}

// listNames lists the entries of a migrations folder (names only, sorted).
func listNames(dir string) []string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func (r *run) drop() error {
	if r.o.Drill.Engine == SQLServer {
		_, err := r.sqlcmd("master", "IF DB_ID(N'"+r.scratch+"') IS NOT NULL BEGIN ALTER DATABASE ["+r.scratch+"] SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE ["+r.scratch+"]; END")
		return err
	}
	if _, err := r.psql(r.adminDB(), "-c", "DROP DATABASE IF EXISTS "+r.scratch+" WITH (FORCE)"); err != nil {
		// before Postgres 13 there is no FORCE
		_, err = r.psql(r.adminDB(), "-c", "DROP DATABASE IF EXISTS "+r.scratch)
		return err
	}
	return nil
}
