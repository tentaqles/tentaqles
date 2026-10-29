package db

// The db pack's commands (spec D §13, §14), mounted under `tq pack db`:
//
//	tq pack db drill <name> [--keep]   run a restore drill into a local scratch database
//	tq pack db approve --env <env>     ask a human to approve migrating that environment
//
// approve only asks (annotation tq.human-only: ask): the human answers in
// their own terminal with tq hub answer, never an agent and never the board.
// There is no executor: once the human answered apply, the agent runs the
// migration unchanged and the tq/db-migrate-env guard matches it to the
// approval.

import (
	"context"
	"errors"
	"fmt"
	"github.com/tentaqles/tentaqles/internal/paths"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/plans"
	"github.com/tentaqles/tentaqles/internal/packs/db/drill"
	"github.com/tentaqles/tentaqles/internal/packs/db/gate"
	"github.com/tentaqles/tentaqles/internal/packs/db/lint"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
)

func commands() []*cobra.Command {
	return []*cobra.Command{newDrillCmd(), newApproveCmd()}
}

// cmdContext is the run context of a db subcommand inside a workspace with
// the pack enabled.
func cmdContext(c *cobra.Command) (*packs.Input, error) {
	in, err := packs.FromCommand(c)
	if err != nil {
		return nil, err
	}
	if in.Workspace() == "" {
		return nil, errors.New("tq pack db needs a trusted workspace (run it inside one)")
	}
	if !in.Config.IsEnabled("db") {
		return nil, errors.New("the db pack is not enabled in this workspace (packs.db in .tentaqles.yaml)")
	}
	if in.StateDir() == "" {
		return nil, errors.New("the db pack has no state directory for this workspace")
	}
	return in, nil
}

// ---- drill ---------------------------------------------------------------

// drillGetenv is the drill's variable lookup (tests replace it).
var drillGetenv = os.Getenv

func newDrillCmd() *cobra.Command {
	var keep bool
	c := &cobra.Command{
		Use:   "drill <name> [--keep]",
		Short: "Restore a declared backup into a local scratch database and record the evidence",
		Long: `Runs packs.db.drill <name>: reads the backup (read-only), restores it into a
scratch database tq_drill_<name>_<ts> on the local test_db server, records
row counts of the verify tables, the schema size and the migration version
as one line in docs/ops/restore-drills.jsonl, and drops the scratch database.
It refuses any target that is not local. Only names and numbers are recorded.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			in, err := cmdContext(c)
			if err != nil {
				return err
			}
			return runDrill(c.Context(), c.OutOrStdout(), in, args[0], keep)
		},
	}
	c.Flags().BoolVar(&keep, "keep", false, "keep the dump files under the db pack's state directory (never committed)")
	return c
}

func runDrill(ctx context.Context, w io.Writer, in *packs.Input, name string, keep bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ds, err := drill.Parse(in.Settings())
	if err != nil {
		return fmt.Errorf("packs.db.drill is invalid: %w", err)
	}
	d, ok := drill.Find(ds, name)
	if !ok {
		if len(ds) == 0 {
			return errors.New("no drill is declared in packs.db.drill")
		}
		return fmt.Errorf("no drill %q; declared: %s", name, strings.Join(drill.Names(ds), ", "))
	}
	o, err := drill.Run(ctx, drill.Options{In: in, Drill: d, Keep: keep, Out: w, Getenv: drillGetenv})
	if err != nil {
		return err
	}
	l := o.Line
	status := "ok"
	if !l.OK {
		status = "FAILED at " + l.FailedStep
	}
	fmt.Fprintf(w, "drill %s: %s in %ds\n", l.Name, status, l.DurationS)
	for _, t := range l.Tables {
		fmt.Fprintf(w, "  %s: %d rows\n", t.Name, t.Count)
	}
	fmt.Fprintf(w, "  schema: %d tables, %d indexes\n", l.Schema.Tables, l.Schema.Indexes)
	fmt.Fprintf(w, "  migration version: %s", l.MigrationVersion)
	if l.MigrationGap {
		fmt.Fprintf(w, " (the newest migration file is %s: the backup predates it)", l.MigrationFiles)
	}
	fmt.Fprintf(w, "\n  backup taken: %s (%s)\n", l.BackupTakenAt, l.BackupTimeFrom)
	fmt.Fprintf(w, "evidence appended to %s: stage it by name and commit it with the work\n", drill.EvidenceFile)
	if o.Kept != "" {
		fmt.Fprintln(w, "the dump files are kept in the db pack's state directory (--keep); they are never committed")
	}
	if !l.OK {
		return fmt.Errorf("drill %s failed at %s; the tool output is in the db pack's state directory (drills/logs/%s.log), for the human to read: it may hold row data", l.Name, l.FailedStep, o.Scratch)
	}
	return nil
}

// ---- approve ----------------------------------------------------------------

// approveNow is the approve command's clock (tests replace it).
var approveNow = func() time.Time { return time.Now().UTC() }

func newApproveCmd() *cobra.Command {
	var env string
	var noDrill, allowFailing bool
	c := &cobra.Command{
		Use:   "approve --env <env> [--no-drill] [--allow-failing]",
		Short: "Ask a human to approve migrating an environment (agents may ask; only a human answers)",
		Long: `Asks for a pack approval to run migrations against <env> (migrate: approve).
The evidence binds the content of every migration file and the newest restore
drill line; the request is refused when a declared drill is stale (a human may
pass --no-drill) or a db check fails (a human may pass --allow-failing).
The human answers in their own terminal: tq hub answer <card> <decision>
--choice apply. Then the migration runs once, unchanged, in a command of its
own; changing a migration file or recording a new drill voids the approval.`,
		Args: cobra.NoArgs,
		// It only asks (approval.Ask); the human answers in their own terminal.
		Annotations: map[string]string{packs.AnnotationHumanOnly: packs.ApprovalAsk},
		RunE: func(c *cobra.Command, _ []string) error {
			in, err := cmdContext(c)
			if err != nil {
				return err
			}
			if noDrill {
				if err := packs.HumanOnly(); err != nil {
					return fmt.Errorf("--no-drill: %w", err)
				}
			}
			if allowFailing {
				if err := packs.HumanOnly(); err != nil {
					return fmt.Errorf("--allow-failing: %w", err)
				}
			}
			return askMigrate(c.Context(), c.OutOrStdout(), in, env, noDrill, allowFailing)
		},
	}
	c.Flags().StringVar(&env, "env", "", "the environment to migrate (required)")
	c.Flags().BoolVar(&noDrill, "no-drill", false, "ask although a declared restore drill is stale (human only)")
	c.Flags().BoolVar(&allowFailing, "allow-failing", false, "ask although a db check failed (human only)")
	return c
}

func askMigrate(ctx context.Context, w io.Writer, in *packs.Input, env string, noDrill, allowFailing bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if env == "" {
		return errors.New("name the environment with --env")
	}
	gt := envgate.FromManifest(envgate.DB, in.Manifest, in.Config)
	if len(gt.Envs()) == 0 || !gt.Declared(env) {
		return fmt.Errorf("environment %q is not declared in packs.db.environments (declared: %s)", env, strings.Join(gt.Envs(), ", "))
	}
	switch pol := gt.Policy(env, envgate.Migrate); pol {
	case envgate.Approve:
	case envgate.Deny:
		return fmt.Errorf("environment %s has migrate: deny; no approval can allow it, a human runs it in their own terminal", env)
	default:
		return fmt.Errorf("environment %s has migrate: %s; it needs no approval", env, pol)
	}
	var roots []string
	for _, m := range lint.Migrations(in.Settings()) {
		roots = append(roots, m.Path)
	}
	if len(roots) == 0 {
		return errors.New("packs.db.migrations names no path: there is nothing to bind the approval to")
	}
	if gt.RequireMerged(env) {
		if err := requireMerged(ctx, in.Root, env, roots); err != nil {
			return err
		}
	}
	// the drill (§14: a prod migration approval binds a fresh drill)
	drills, err := drill.Parse(in.Settings())
	if err != nil {
		return fmt.Errorf("packs.db.drill is invalid: %w", err)
	}
	raw, err := drill.ReadLog(in.Root)
	if err != nil {
		return err
	}
	entries, _ := drill.ParseLog(raw)
	now := approveNow()
	var drillText []string
	var stale []string
	for _, d := range drills {
		f := drill.Check(entries, d, now)
		if f.Fresh {
			drillText = append(drillText, fmt.Sprintf("drill %s: fresh, %s old", d.Name, drill.Days(f.Age)))
			continue
		}
		stale = append(stale, d.Name)
		drillText = append(drillText, fmt.Sprintf("drill %s: NOT FRESH (%s)", d.Name, f.Why))
	}
	if len(stale) > 0 && !noDrill {
		return fmt.Errorf("the restore drill is not fresh (%s): run `tq pack db drill %s` and commit %s first (a human may pass --no-drill from their own terminal)", strings.Join(drillText, "; "), stale[0], drill.EvidenceFile)
	}
	drillEv := drill.Binding(raw, drills)
	if noDrill {
		drillEv = drill.BindingSkipped
	}
	// the db checks (built-in, over the default selection, as tq pack check)
	failed, results, err := dbChecks(ctx, in)
	if err != nil {
		return err
	}
	if len(failed) > 0 && !allowFailing {
		return fmt.Errorf("required db checks failed: %s. Fix them first (a human may pass --allow-failing from their own terminal)", strings.Join(failed, ", "))
	}
	h, err := gate.MigrationsHash(gate.DiskReader{Root: in.Root}, roots, time.Time{})
	if err != nil {
		return fmt.Errorf("the migrations cannot be bound: %w", err)
	}
	ev := map[string]string{gate.EvMigrations: h, gate.EvDrill: drillEv}
	var sum strings.Builder
	if len(failed) > 0 {
		fmt.Fprintf(&sum, "FAILING CHECKS (asked with --allow-failing): %s\n", strings.Join(failed, ", "))
	}
	if noDrill && len(drills) > 0 {
		sum.WriteString("RESTORE DRILL SKIPPED (asked with --no-drill)\n")
	}
	fmt.Fprintf(&sum, "migrate %s: the migrations under %s\n", env, strings.Join(roots, ", "))
	fmt.Fprintf(&sum, "db checks: %s\n", results)
	switch {
	case len(drills) == 0:
		sum.WriteString("no restore drill is declared (packs.db.drill)\n")
	default:
		sum.WriteString(strings.Join(drillText, "\n") + "\n")
	}
	sum.WriteString("binding: every migration file's content and the newest drill line; a changed file or a new drill voids it")
	dec, card, err := approval.Ask(in.Workspace(), approval.Request{Pack: "db", Action: envgate.Migrate, Subject: gate.Subject, Target: env,
		Evidence: ev, Summary: sum.String(), Options: []string{"apply", "reject"}, ValidFor: gt.TTL(env), ProjectDir: projectDir(in)})
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "asked: decision %s on card %s (migrate %s -> %s)\n", dec, card, gate.Subject, env)
	fmt.Fprintf(w, "a human answers in their own terminal (never an agent, never on the board): tq hub answer %s %s --choice apply\n", card, dec)
	fmt.Fprintln(w, "then run the migration once, unchanged, in a command of its own; a changed migration file or a new drill line voids the approval")
	return nil
}

// requireMerged: the migrations must be committed and on the default
// branch's remote ref (require_merged, the prod default).
func requireMerged(ctx context.Context, root, env string, roots []string) error {
	args := append([]string{"status", "--porcelain", "--"}, roots...)
	out, err := packs.Git(ctx, root, args...)
	if err != nil {
		return fmt.Errorf("environment %s requires merged migrations, and git status failed: %w", env, err)
	}
	if strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("environment %s requires merged migrations, and the migrations have uncommitted changes; commit and merge them first", env)
	}
	head, err := packs.Git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("environment %s requires merged migrations, and HEAD cannot be read: %w", env, err)
	}
	ref := plans.DefaultRemoteRef(ctx, root)
	switch {
	case ref == "":
		return fmt.Errorf("environment %s requires merged migrations, and the default branch's remote ref cannot be found (git fetch)", env)
	case !plans.Merged(ctx, root, strings.TrimSpace(string(head)), ref):
		return fmt.Errorf("environment %s requires merged migrations: HEAD is not on %s yet", env, strings.TrimPrefix(ref, "refs/remotes/"))
	}
	return nil
}

// dbChecks runs the db pack's built-in checks over the default selection
// and returns the failing required ids and a one-line result.
func dbChecks(ctx context.Context, in *packs.Input) (failed []string, line string, err error) {
	run := *in
	changed, base, err := packs.Select(ctx, in.Root, packs.Selection{})
	if err != nil {
		return nil, "", fmt.Errorf("the changed files cannot be selected for the db checks: %w", err)
	}
	run.Changed, run.Base = changed, base
	run.Budget = time.Now().Add(5 * time.Minute)
	rep := packs.RunChecks(ctx, &run, packs.RunOptions{Packs: []string{"db"}, BuiltinOnly: true, Classes: in.Classes()})
	var parts []string
	for _, r := range rep.Results {
		parts = append(parts, r.ID+" "+string(r.Status))
		if r.Status == packs.Fail && r.Required {
			failed = append(failed, r.ID)
		}
	}
	sort.Strings(failed)
	if len(parts) == 0 {
		return failed, "none applied", nil
	}
	return failed, strings.Join(parts, ", "), nil
}

// projectDir is the repo's folder relative to the workspace root ("" when
// they are the same or the repo is outside it).
func projectDir(in *packs.Input) string {
	r, err := paths.Rel(in.WorkspaceRoot(), in.Root)
	if err != nil || r == "." || strings.HasPrefix(r, "..") || filepath.IsAbs(r) {
		return ""
	}
	return filepath.ToSlash(r)
}
