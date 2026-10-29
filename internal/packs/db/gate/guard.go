package gate

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/db/drill"
	"github.com/tentaqles/tentaqles/internal/packs/db/lint"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Guard ids (spec D §16).
const (
	IDMigrate = "tq/db-migrate-env"
	IDRestore = "tq/restore-target"
)

// Events are the tools the guards read.
var Events = []string{"Bash", "PowerShell"}

// Supersedes is the core rule the migrate gate replaces for the calls it
// handles (§16).
var Supersedes = []string{"tq/remote-migration"}

// Getenv reads the hook's own environment (tests replace it). Values are
// compared, never printed.
var Getenv = os.Getenv

var (
	coreOnce sync.Once
	coreRe   *regexp.Regexp // tq/remote-migration's command regex, without its local exception
)

func core() *regexp.Regexp {
	coreOnce.Do(func() {
		for _, r := range policy.Builtin() {
			if r.ID == "tq/remote-migration" && r.Command != "" {
				coreRe = regexp.MustCompile("(?i)" + r.Command)
			}
		}
		if coreRe == nil {
			coreRe = regexp.MustCompile(`(?i)\bmigrat`) // fail closed if the core rule ever moves
		}
	})
	return coreRe
}

// golang-migrate: migrate -database URL -path P up|down|goto|force|drop.
var golangMigrateRe = regexp.MustCompile(`(?i)^migrate\b.*\s(up|down|goto|force|drop)\b`)

// inert are programs that only print or search their arguments.
var inert = map[string]bool{"echo": true, "printf": true, "grep": true, "egrep": true, "rg": true, "findstr": true, "git": true,
	"write-host": true, "write-output": true, "select-string": true, "man": true, "tldr": true}

// interpreters run a script named by their first argument (python
// manage.py migrate, php artisan migrate).
var interpreters = map[string]bool{"python": true, "python3": true, "py": true, "php": true, "node": true, "ruby": true}

// migrationText is the leaf's words for matching: from the program, with an
// interpreter's script reduced to its base name.
func migrationText(l Leaf) string {
	w := l.Words()
	if len(w) >= 2 && interpreters[l.Prog] {
		w = append([]string{path.Base(strings.ReplaceAll(w[1], `\`, "/"))}, w[2:]...)
	}
	return strings.ToLower(strings.Join(w, " "))
}

// isMigration reports a leaf that runs migrations (the tools of
// tq/remote-migration, plus golang-migrate): the match must start at the
// program, so prose and other commands that only mention one do not count.
func isMigration(l Leaf) bool {
	if l.Opaque || inert[l.Prog] {
		return false
	}
	t := migrationText(l)
	if loc := core().FindStringIndex(t); loc != nil && loc[0] == 0 {
		return true
	}
	return l.Prog == "migrate" && (l.Has("-database", "--database") || l.Has("-path", "--path", "-source", "--source")) && golangMigrateRe.MatchString(t)
}

func lookup(l Leaf, name string) string {
	if v, ok := l.Env[name]; ok {
		return v
	}
	if l.Unknown {
		return ""
	}
	return Getenv(name)
}

func extraHosts(g packs.GuardCtx) []string { return g.Settings().Strings("test_db.hosts") }

// supabaseMode is how a supabase leaf chooses its database.
func supabaseMode(l Leaf) (linked, local bool) {
	t := l.Text()
	switch {
	case l.Has("--db-url"):
		return false, false
	case l.Has("--local"):
		return false, true
	case l.Has("--linked"):
		return true, false
	case strings.Contains(t, "supabase db push"), strings.Contains(t, "supabase db dump"), strings.Contains(t, "supabase db pull"):
		return true, false // --linked is the default
	case strings.Contains(t, "supabase migration up"), strings.Contains(t, "supabase db reset"):
		return false, true // --local is the default
	}
	return false, false
}

// linkedRef resolves the linked project the way the Supabase CLI does after
// --project-ref: SUPABASE_PROJECT_ID, then <workdir>/supabase/.temp/project-ref.
func linkedRef(l Leaf, g packs.GuardCtx) (string, bool) {
	if v := lookup(l, "SUPABASE_PROJECT_ID"); v != "" {
		if computed(v) {
			return "", false
		}
		return strings.ToLower(v), true
	}
	wd, _ := l.Flag("--workdir")
	if wd == "" {
		wd = lookup(l, "SUPABASE_WORKDIR")
	}
	if computed(wd) && wd != "" {
		return "", false
	}
	// a relative workdir is read from the repo root (ReadLocal's base), an
	// absolute one must be inside ReadLocal's roots
	p := path.Join(strings.ReplaceAll(wd, `\`, "/"), "supabase", ".temp", "project-ref")
	if cwd := g.Cwd(); cwd != "" && !filepath.IsAbs(filepath.FromSlash(p)) {
		p = filepath.Join(cwd, filepath.FromSlash(p))
	}
	b, err := g.ReadLocal(p, 256)
	if err != nil {
		// no ref file (or one tq may not read): no signal, the target
		// falls to the default environment
		return "", true
	}
	return ReadRef(b), true
}

// migrateTarget reads a migration leaf's target (§14 steps 1 to 4).
func migrateTarget(l Leaf, g packs.GuardCtx) Target {
	var t Target
	extra := extraHosts(g)
	for _, u := range urls(l) {
		t.addURL(u, extra)
	}
	for _, f := range []string{"--db-url", "--connection", "--url", "-url", "--database-url", "-database", "--dsn"} {
		if v, ok := l.Flag(f); ok {
			t.flagValue(v, extra)
		}
	}
	if v, ok := l.Flag("--project-ref"); ok && v != "" {
		t.Tier1 = append(t.Tier1, envgate.Signal{Key: "project_refs", Value: strings.ToLower(v)})
	}
	supabase := l.Prog == "supabase"
	if supabase {
		linked, local := supabaseMode(l)
		if local {
			t.Hosts = append(t.Hosts, "localhost")
		}
		if linked {
			t.Linked = true
			if ref, ok := linkedRef(l, g); ok && ref != "" {
				t.Tier3 = append(t.Tier3, envgate.Signal{Key: "project_refs", Value: ref})
			}
		}
	}
	if v, ok := l.Flag("-S", "--server"); ok {
		t.serverFlag(v)
	}
	t.serverFragments(l.Raw)
	t.Local = !t.Unknown && !t.Linked && t.AllLocal(extra)
	return t
}

// verdict is one leaf's outcome.
type verdict struct {
	action  policy.Action
	reason  string
	consume string
	counter string
}

func rank(a policy.Action) int {
	switch a {
	case policy.Deny:
		return 3
	case policy.Ask:
		return 2
	case policy.Allow:
		return 0
	}
	return 3
}

const howTo = "ask with `tq pack db approve --env %s` (it needs a fresh restore drill); after the human answers apply in their own terminal, run the migration unchanged in a command of its own"

// MigrateEnv is the tq/db-migrate-env guard (§14): every migration command
// resolves to an environment and gets its migrate policy. Without declared
// environments it leaves the call to the core tq/remote-migration rule.
func MigrateEnv(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	if !call.IsShell() || strings.TrimSpace(call.Command) == "" {
		return policy.Allow, "", false
	}
	gt := envgate.FromManifest(envgate.DB, g.Manifest(), g.Config())
	if len(gt.Envs()) == 0 {
		return policy.Allow, "", false
	}
	leaves := Analyze(call.Command, call.Tool == "PowerShell")
	var vs []verdict
	for _, l := range leaves {
		unreadable := false
		if !isMigration(l) {
			// the core rule matches a command the parser did not read as a
			// migration: behind an opaque wrapper, or a tool word in a form
			// tq does not unwrap. It counts as a migration to the default
			// environment (fail closed).
			if inert[l.Prog] || !core().MatchString(strings.ToLower(l.Raw)) || (!l.Opaque && !unquotedTool(l)) {
				continue
			}
			unreadable = true
		}
		vs = append(vs, evaluate(l, unreadable, gt, g))
	}
	if len(vs) == 0 {
		return policy.Allow, "", false
	}
	act, reasons, consumed := policy.Allow, []string{}, []string{}
	for _, v := range vs {
		if rank(v.action) > rank(act) {
			act = v.action
		}
		if v.reason != "" {
			reasons = append(reasons, v.reason)
		}
		if v.consume != "" {
			consumed = append(consumed, v.consume)
		}
		if v.counter != "" {
			g.Count(v.counter, 1)
		}
	}
	seen := map[string]bool{}
	for _, id := range consumed {
		if seen[id] {
			act = policy.Deny
			reasons = append(reasons, "one approval allows one migration run: approval "+id+" is used twice in this command")
		}
		seen[id] = true
	}
	if len(consumed) > 0 && act != policy.Allow {
		// an ask the human may decline would use the approval up
		act = policy.Deny
		reasons = append(reasons, "run the approved migration in a command of its own")
	}
	if act == policy.Allow {
		for _, id := range consumed {
			g.Consume(id)
		}
	}
	return act, strings.Join(reasons, "; "), true
}

// unquotedTool: the raw segment names a migration tool in a form the
// parser did not unwrap.
func unquotedTool(l Leaf) bool {
	for _, w := range l.Words() {
		switch strings.ToLower(strings.TrimSuffix(path.Base(strings.ReplaceAll(w, `\`, "/")), ".exe")) {
		case "prisma", "alembic", "flyway", "liquibase", "dbmate", "goose", "knex", "sqitch", "supabase", "drizzle-kit", "atlas", "rails", "artisan", "manage.py", "dotnet":
			return true
		}
	}
	return false
}

func describe(l Leaf) string {
	f := l.Words()
	if len(f) > 4 {
		f = f[:4]
	}
	if len(f) == 0 {
		return "a migration command"
	}
	return "`" + strings.Join(f, " ") + "`"
}

func evaluate(l Leaf, unreadable bool, gt envgate.Gate, g packs.GuardCtx) verdict {
	what := describe(l)
	t := migrateTarget(l, g)
	tiers := t.Tiers(l)
	var res envgate.Resolution
	floor := unreadable || l.Opaque || l.Unknown || t.Unknown
	if floor {
		res = gt.ResolveFloor(envgate.Migrate, tiers)
	} else {
		res = gt.Resolve(envgate.Migrate, tiers)
	}
	if !res.Resolved && t.Local && !floor {
		// a local database no environment lists: the core rule's local
		// exception
		return verdict{action: policy.Allow}
	}
	env := res.Env
	where := "environment " + env
	if !res.Resolved {
		where += " (the target could not be resolved, so it counts as the default environment)"
	}
	if unreadable {
		what = "a migration command tq cannot read"
	}
	pol := gt.Policy(env, envgate.Migrate)
	switch pol {
	case envgate.Auto:
		return verdict{action: policy.Allow}
	case envgate.Ask:
		return verdict{action: policy.Ask, reason: fmt.Sprintf("%s: %s runs migrations against %s (migrate: ask); confirm the target and that a backup or rollback path exists", IDMigrate, what, where)}
	case envgate.Approve:
		if unreadable {
			return deny(what, where, env, "tq cannot read the command, so no approval can be matched to it; run the migration tool directly", "unreadable", res)
		}
		return approve(what, where, env, g, res)
	}
	return verdict{action: policy.Deny, counter: "migrate_denied:policy",
		reason: fmt.Sprintf("%s: %s runs migrations against %s (migrate: deny). A human runs it in their own terminal, outside Claude", IDMigrate, what, where)}
}

func deny(what, where, env, why, counter string, res envgate.Resolution) verdict {
	if !res.Resolved && counter == "no-approval" {
		counter = "unresolved-env"
	}
	return verdict{action: policy.Deny, counter: "migrate_denied:" + counter,
		reason: fmt.Sprintf("%s: %s to %s needs a human approval of the exact migrations: %s. To migrate: %s", IDMigrate, what, where, why, fmt.Sprintf(howTo, env))}
}

// Now is the guard's clock for the hash budget (tests replace it).
var Now = time.Now

// GuardEvidence rebuilds the approval evidence from the repository: the
// migrations content hash and the drill binding of the evidence log.
func GuardEvidence(g packs.GuardCtx) (map[string]string, error) {
	var roots []string
	for _, m := range lint.Migrations(g.Settings()) {
		roots = append(roots, m.Path)
	}
	h, err := MigrationsHash(GuardReader{g}, roots, Now().Add(HashBudget))
	if err != nil {
		return nil, err
	}
	drills, err := drill.Parse(g.Settings())
	if err != nil {
		return nil, fmt.Errorf("packs.db.drill is invalid: %w", err)
	}
	var raw []byte
	if len(drills) > 0 {
		raw, err = g.ReadLocal(drill.EvidenceFile, drill.MaxEvidenceBytes)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s cannot be read: %w", drill.EvidenceFile, err)
		}
	}
	return map[string]string{EvMigrations: h, EvDrill: drill.Binding(raw, drills)}, nil
}

func approve(what, where, env string, g packs.GuardCtx, res envgate.Resolution) verdict {
	ev, err := GuardEvidence(g)
	if err != nil {
		return deny(what, where, env, "the migrations cannot be verified in the hook ("+err.Error()+"); a human runs it in their own terminal", "hash-mismatch", res)
	}
	want := approval.Binding{Pack: "db", Action: envgate.Migrate, Subject: Subject, Target: env}
	var firstErr error
	for _, d := range []string{ev[EvDrill], drill.BindingSkipped} {
		e := map[string]string{EvMigrations: ev[EvMigrations], EvDrill: d}
		a, err := approval.FindGuard(g, want, e)
		if err == nil {
			return verdict{action: policy.Allow, consume: a.Decision}
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return deny(what, where, env, firstErr.Error(), deniedWhy(firstErr), res)
}

func deniedWhy(err error) string {
	switch {
	case errors.Is(err, approval.ErrExpired):
		return "expired"
	case errors.Is(err, approval.ErrUsed):
		return "used"
	case errors.Is(err, approval.ErrEvidence):
		return "hash-mismatch"
	}
	return "no-approval"
}
