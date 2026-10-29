package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/baseline"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// packCLIError is a usage or config error of tq pack: exit 2.
type packCLIError struct{ err error }

func (e packCLIError) Error() string { return e.err.Error() }

func usageErr(format string, a ...any) error { return packCLIError{fmt.Errorf(format, a...)} }

func newPackCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "pack",
		Short: "Pack checks, doctor, crew, approvals and baselines (data, ml, cloud, db)",
		Long: `Packs add checks, guard rules, reviewers and commands for one kind of work.
Enable them in the manifest's packs: block; a repo may add to it (never
loosen it) with tq-packs.yaml at its root. See docs/PACKS.md.`,
	}
	c.AddCommand(newPackListCmd(), newPackCheckCmd(), newPackDoctorCmd(), newPackCrewCmd(), newPackApprovalsCmd(), newPackBaselineCmd())
	for _, name := range packs.Names() {
		sub := &cobra.Command{Use: name, Short: "Commands of the " + name + " pack"}
		for _, pc := range packs.Commands(name) {
			sub.AddCommand(pc)
		}
		c.AddCommand(sub)
	}
	return c
}

// packRepoRoot is the git top level of cwd, or cwd when it is not a repo.
func packRepoRoot(cwd string) string {
	if r, err := packs.RepoRoot(context.Background(), cwd); err == nil {
		return r
	}
	return cwd
}

// packInputFor builds the Input for cwd: the trusted workspace's manifest
// (nil outside one), the repo layer, git, the tool runner. ok is false only
// when the repo file is invalid (the Input still carries the manifest-only
// config).
func packInputFor(cwd string, ws *resolve.Workspace) (*packs.Input, bool) {
	in, err := packInput(cwd, ws)
	return in, err == nil
}

func packInput(cwd string, ws *resolve.Workspace) (*packs.Input, error) {
	root := packRepoRoot(cwd)
	in := &packs.Input{Root: root, Git: packs.GitRepo{Root: root}}
	if ws != nil && ws.Manifest != nil {
		in.Manifest = ws.Manifest
		in.SetWorkspace(ws.Name, ws.Root)
		packs.SetEventWorkspace(ws.Name)
	}
	cfg, err := packs.LoadConfig(in.Manifest, root)
	in.Config = cfg
	in.Tools = packs.NewToolRunner(root, cfg)
	return in, err
}

func packCwd() (string, *resolve.Workspace) {
	cwd, _ := os.Getwd()
	ws, _ := resolveTrusted(cwd)
	return cwd, ws
}

// packRun runs fn and maps a packCLIError to exit 2.
func packRun(c *cobra.Command, fn func() error) error {
	err := fn()
	var ue packCLIError
	if errors.As(err, &ue) {
		fmt.Fprintln(c.ErrOrStderr(), "tq pack: "+ue.Error())
		exitFunc(2)
		return nil
	}
	return err
}

func newPackListCmd() *cobra.Command {
	var enabledOnly, asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "The packs, the merged config and its config_hash, and each check",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, ws := packCwd()
			in, err := packInput(cwd, ws)
			rep := packs.List(in.Config, in.Workspace(), enabledOnly)
			if err != nil {
				rep.Problems = append(rep.Problems, err.Error())
			}
			out := c.OutOrStdout()
			if asJSON {
				return packs.EncodeListJSON(out, rep)
			}
			if len(rep.Packs) == 0 {
				fmt.Fprintln(out, "no packs enabled")
			}
			for _, p := range rep.Packs {
				st := "disabled"
				if p.Enabled {
					st = "enabled"
				}
				tools := "-"
				if len(p.Tools) > 0 {
					tools = strings.Join(p.Tools, ", ")
				}
				fmt.Fprintf(out, "%-6s %-8s tools: %s\n", p.Name, st, tools)
				for _, ch := range p.Checks {
					req := "advisory"
					if ch.Required {
						req = "required"
					}
					tool := ""
					if ch.Tool != "" {
						tool = " (" + ch.Tool + ")"
					}
					fmt.Fprintf(out, "  %-34s %-8s %s%s\n", ch.ID, ch.Kind, req, tool)
				}
			}
			fmt.Fprintf(out, "config_hash %s\n", rep.ConfigHash)
			for _, p := range rep.Problems {
				fmt.Fprintln(out, "warning: "+p)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&enabledOnly, "enabled", false, "only the enabled packs")
	c.Flags().BoolVar(&asJSON, "json", false, "print tq.packs/v1 JSON")
	return c
}

func newPackCheckCmd() *cobra.Command {
	var packFilter, paths []string
	var staged, all, asJSON, builtinOnly bool
	var base string
	var budget time.Duration
	c := &cobra.Command{
		Use:   "check [--pack X]... [--staged | --paths a b ... | --base <ref> | --all]",
		Short: "Run the applicable checks of the enabled packs (exit 0 ok, 1 a required check failed, 2 usage or config error)",
		Long: `Runs the checks of the enabled packs over the changed files:

  (none)        the working tree vs HEAD, plus untracked files
  --staged      the index vs HEAD
  --base <ref>  the working tree vs <ref>, plus untracked files
  --paths a b   the listed paths
  --all         every tracked file (diff checks skip)

Built-in checks of an enabled pack are required. An external check is
required when its tool is declared in packs.<pack>.tools: declared but
missing fails, undeclared and missing skips, undeclared but installed runs
as an advisory check. --json prints tq.packs/v1.`,
		RunE: func(c *cobra.Command, args []string) error {
			return packRun(c, func() error {
				if len(args) > 0 && len(paths) == 0 {
					return usageErr("unexpected arguments %v (use --paths to name files)", args)
				}
				paths = append(paths, args...)
				if budget <= 0 || budget > packs.MaxBudget {
					return usageErr("--budget must be between 1s and %s", packs.MaxBudget)
				}
				for _, p := range packFilter {
					if !slicesContains(packs.Names(), p) {
						return usageErr("--pack %q: unknown pack (known: %s)", p, strings.Join(packs.Names(), ", "))
					}
				}
				cwd, ws := packCwd()
				in, err := packInput(cwd, ws)
				if err != nil {
					return usageErr("%v", err)
				}
				out := c.OutOrStdout()
				ctx := context.Background()
				if len(in.Config.Enabled()) > 0 {
					changed, b, err := packs.Select(ctx, in.Root, packs.Selection{Staged: staged, Paths: paths, Base: base, All: all})
					if errors.Is(err, packs.ErrSelection) {
						return usageErr("%v", err)
					}
					if err != nil {
						return usageErr("selecting the changed files: %v", err)
					}
					in.Changed, in.Base, in.Staged, in.All = changed, b, staged, all
					set, _, err := baseline.LoadForConfig(in.Root, in.Config)
					if err != nil {
						return usageErr("%v", err)
					}
					in.Baseline = set
				}
				in.Budget = time.Now().Add(budget)
				rep := packs.RunChecks(ctx, in, packs.RunOptions{Packs: packFilter, BuiltinOnly: builtinOnly, Classes: hubClasses(ws)})
				if asJSON {
					if err := packs.EncodeJSON(out, rep); err != nil {
						return err
					}
				} else if len(in.Config.Enabled()) == 0 {
					fmt.Fprintln(out, "no packs enabled")
				} else {
					fmt.Fprint(out, rep.Text())
				}
				if rep.Failed() {
					exitFunc(1)
				}
				return nil
			})
		},
	}
	c.Flags().StringArrayVar(&packFilter, "pack", nil, "only this pack (repeatable)")
	c.Flags().BoolVar(&staged, "staged", false, "check the staged changes")
	c.Flags().StringSliceVar(&paths, "paths", nil, "check these paths (further arguments are paths too)")
	c.Flags().StringVar(&base, "base", "", "check the working tree against this ref")
	c.Flags().BoolVar(&all, "all", false, "check every tracked file")
	c.Flags().BoolVar(&asJSON, "json", false, "print tq.packs/v1 JSON")
	c.Flags().BoolVar(&builtinOnly, "builtin-only", false, "run only the built-in (pure Go) checks")
	c.Flags().DurationVar(&budget, "budget", packs.DefaultBudget, "deadline for the whole run (max 1800s)")
	return c
}

func slicesContains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func newPackDoctorCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Per tool: declared, installed, path, version, minimum; plus the packs' own probes",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, ws := packCwd()
			in, rerr := packInput(cwd, ws)
			tools := packs.Tools(in)
			files, _ := packs.GitRepo{Root: in.Root}.ListFiles()
			if files == nil {
				files = packs.WalkFiles(in.Root, packs.SuggestMaxFiles)
			}
			fs := packs.Doctor(in, files, rerr)
			missing := false
			for _, t := range tools {
				if t.Declared && !t.Installed {
					missing = true
				}
			}
			out := c.OutOrStdout()
			if asJSON {
				if tools == nil {
					tools = []packs.ToolStatus{}
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(map[string]any{"schema": packs.Schema, "workspace": in.Workspace(), "tools": tools, "findings": fs}); err != nil {
					return err
				}
			} else {
				if len(in.Config.Enabled()) == 0 {
					fmt.Fprintln(out, "no packs enabled")
				}
				tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
				if len(tools) > 0 {
					fmt.Fprintln(tw, "PACK\tTOOL\tDECLARED\tINSTALLED\tVERSION\tMINIMUM\tPATH")
				}
				for _, t := range tools {
					fmt.Fprintf(tw, "%s\t%s\t%v\t%v\t%s\t%s\t%s\n", t.Pack, t.Tool, yesNo(t.Declared), yesNo(t.Installed), dash(t.Version), dash(t.Minimum), dash(t.Path))
				}
				tw.Flush()
				for _, f := range fs {
					line := fmt.Sprintf("[%s] %s: %s", f.Level, f.Code, f.Msg)
					if f.Fix != "" {
						line += "  -> " + f.Fix
					}
					fmt.Fprintln(out, line)
				}
			}
			if missing {
				exitFunc(1)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func newPackCrewCmd() *cobra.Command {
	var flags []string
	var asJSON bool
	c := &cobra.Command{
		Use:   "crew --flags a,b",
		Short: "The reviewer agents the triage flags map to in the enabled packs",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, ws := packCwd()
			in, _ := packInput(cwd, ws)
			crew := packs.Crew(in.Config, flags)
			out := c.OutOrStdout()
			if asJSON {
				if flags == nil {
					flags = []string{}
				}
				return json.NewEncoder(out).Encode(map[string]any{"schema": packs.Schema, "flags": flags, "reviewers": crew})
			}
			for _, r := range crew {
				fmt.Fprintln(out, r)
			}
			return nil
		},
	}
	c.Flags().StringSliceVar(&flags, "flags", nil, "the flags tq decide triage printed (comma separated)")
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

func newPackApprovalsCmd() *cobra.Command {
	var pack string
	var asJSON bool
	c := &cobra.Command{
		Use:   "approvals",
		Short: "Pending, approved, rejected, used and expired pack approvals of this workspace",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			_, ws := packCwd()
			if ws == nil {
				return errors.New("not in a trusted workspace: pack approvals belong to a workspace")
			}
			rows, err := approval.List(ws.Name, pack)
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			if asJSON {
				return json.NewEncoder(out).Encode(map[string]any{"schema": packs.Schema, "workspace": ws.Name, "approvals": rows})
			}
			if len(rows) == 0 {
				fmt.Fprintln(out, "no pack approvals")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "DECISION\tPACK\tACTION\tSUBJECT\tTARGET\tSTATE\tEXPIRES")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Decision, r.Pack, r.Action, r.Subject, r.Target, r.State, r.ExpiresAt.Format(time.RFC3339))
			}
			return tw.Flush()
		},
	}
	c.Flags().StringVar(&pack, "pack", "", "only this pack")
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

func newPackBaselineCmd() *cobra.Command {
	var pack string
	c := &cobra.Command{
		Use:   "baseline [--pack X]",
		Short: "Human only: write or refresh the external-tool baseline from the current findings",
		Long: `Runs the external checks of the enabled packs (or --pack) over every
tracked file and writes their findings to packs.<pack>.baseline, dropping
entries that no longer match. Findings in the baseline are reported as
warn; new ones keep their level. Agent sessions are refused.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if hubAgent() {
				return runner.ErrHumanOnly
			}
			return packRun(c, func() error {
				cwd, ws := packCwd()
				in, err := packInput(cwd, ws)
				if err != nil {
					return usageErr("%v", err)
				}
				return writeBaselines(c.Context(), c.OutOrStdout(), in, pack, hubClasses(ws))
			})
		},
	}
	c.Flags().StringVar(&pack, "pack", "", "only this pack")
	return c
}

func writeBaselines(ctx context.Context, out io.Writer, in *packs.Input, only string, classes []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var targets []string
	for _, p := range in.Config.Enabled() {
		if only != "" && p != only {
			continue
		}
		if in.Config.Pack(p).Baseline != "" {
			targets = append(targets, p)
		}
	}
	if len(targets) == 0 {
		return usageErr("no enabled pack sets a baseline: add packs.<pack>.baseline: <repo path> to the manifest")
	}
	changed, _, err := packs.Select(ctx, in.Root, packs.Selection{All: true})
	if err != nil {
		return usageErr("listing the tracked files: %v", err)
	}
	in.Changed, in.All, in.Base, in.Baseline = changed, true, "", nil
	in.Budget = time.Now().Add(packs.DefaultBudget)
	sort.Strings(targets)
	for _, p := range targets {
		rep := packs.RunChecks(ctx, in, packs.RunOptions{Packs: []string{p}, Classes: classes})
		var entries []baseline.Entry
		for _, r := range rep.Results {
			if r.Tool == nil {
				continue // built-in checks use inline suppressions, not the baseline
			}
			for _, f := range r.Findings {
				entries = append(entries, baseline.EntryFor(r.ID, r.Tool.Name, f))
			}
		}
		path := filepath.Join(in.Root, filepath.FromSlash(in.Config.Pack(p).Baseline))
		if err := baseline.Write(path, entries); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: %d entries -> %s\n", p, len(entries), in.Config.Pack(p).Baseline)
	}
	return nil
}
