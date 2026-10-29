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
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/registry"
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
Only the manifest's packs: block enables a pack; a repo may tighten it (never
loosen or enable) with tq-packs.yaml at its root. See docs/PACKS.md.`,
	}
	// A usage error of any tq pack command (an unknown flag, a bad --budget)
	// exits 2, never 1 (1 means a required check failed).
	c.SetFlagErrorFunc(func(cc *cobra.Command, err error) error {
		fmt.Fprintln(cc.ErrOrStderr(), "tq pack: "+err.Error())
		exitFunc(2)
		return err
	})
	c.AddCommand(newPackListCmd(), newPackCheckCmd(), newPackDoctorCmd(), newPackCrewCmd(), newPackApprovalsCmd(), newPackBaselineCmd())
	for _, name := range packs.Names() {
		pack := name
		sub := &cobra.Command{Use: pack, Short: "Commands of the " + pack + " pack",
			// The run context of every subcommand (packs.FromCommand). A
			// config error (invalid or untrusted manifest, invalid
			// tq-packs.yaml) stops the subcommand with exit 2.
			PersistentPreRunE: func(cc *cobra.Command, _ []string) error {
				cwd, _ := os.Getwd()
				in, err := packContext(cwd)
				in = in.ForPack(pack)
				cc.SetContext(packs.WithInput(cc.Context(), in, err))
				if err != nil {
					fmt.Fprintln(cc.ErrOrStderr(), "tq pack: "+err.Error())
					exitFunc(2)
					return err
				}
				return nil
			}}
		for _, pc := range packs.Commands(pack) {
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

// packWorkspace resolves cwd's workspace. No workspace (outside a base, no
// manifest) is (nil, nil); a manifest that is invalid or untrusted is an
// error: tq pack never runs as if the packs were off.
func packWorkspace(cwd string) (*resolve.Workspace, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, nil
	}
	res := resolve.Resolve(cwd, cfg)
	switch {
	case res.Workspace != nil:
		return res.Workspace, nil
	case res.Untrusted != nil:
		return nil, fmt.Errorf("the workspace manifest of %s is not trusted: review it, then tq allow %s", res.Untrusted.Name, res.Untrusted.Name)
	case strings.HasPrefix(res.Reason, "manifest invalid"):
		return nil, errors.New(res.Reason)
	}
	return nil, nil
}

// packInput builds the Input for cwd and ws (nil outside a workspace): the
// manifest, the repo layer, git, the tool runner, the exit classes. The
// error is a config error (an invalid repo file, a config that cannot be
// hashed); the Input then holds the manifest-only config.
func packInput(cwd string, ws *resolve.Workspace) (*packs.Input, error) {
	root := packRepoRoot(cwd)
	in := &packs.Input{Root: root, Git: packs.GitRepo{Root: root}}
	if ws != nil && ws.Manifest != nil {
		in.Manifest = ws.Manifest
		in.SetWorkspace(ws.Name, ws.Root)
		packs.SetEventWorkspace(ws.Name)
	}
	in.SetClasses(hubClasses(ws))
	cfg, err := packs.LoadConfig(in.Manifest, root)
	in.Config = cfg
	in.Tools = packs.NewToolRunner(root, cfg)
	return in, err
}

// packContext is packWorkspace plus packInput; any error is a config error.
func packContext(cwd string) (*packs.Input, error) {
	ws, werr := packWorkspace(cwd)
	in, err := packInput(cwd, ws)
	if werr != nil {
		return in, werr
	}
	return in, err
}

// packInputFor is the Input triage uses; ok is false when the repo layer is
// invalid (the Input keeps the manifest-only config).
func packInputFor(cwd string, ws *resolve.Workspace) (*packs.Input, bool) {
	in, err := packInput(cwd, ws)
	return in, err == nil
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
			return packRun(c, func() error {
				cwd, _ := os.Getwd()
				in, err := packContext(cwd)
				if err != nil {
					return usageErr("%v", err)
				}
				rep := packs.List(in.Config, in.Workspace(), enabledOnly)
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
			})
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
				cwd, _ := os.Getwd()
				in, err := packContext(cwd)
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
				rep := packs.RunChecks(ctx, in, packs.RunOptions{Packs: packFilter, BuiltinOnly: builtinOnly, Classes: in.Classes()})
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
	c.Flags().StringArrayVar(&paths, "paths", nil, "check this path (repeatable; further arguments are paths too); a directory means its files")
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
			cwd, _ := os.Getwd()
			ws, werr := packWorkspace(cwd)
			if werr != nil {
				fmt.Fprintln(c.ErrOrStderr(), "tq pack: "+werr.Error())
				exitFunc(2)
				return nil
			}
			in, rerr := packInput(cwd, ws)
			if r, ok := in.Tools.(*toolrun.Runner); ok {
				r.NoExecRepo = true // doctor never runs a repo-local binary
			}
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
			cwd, _ := os.Getwd()
			ws, werr := packWorkspace(cwd)
			if werr != nil {
				fmt.Fprintln(c.ErrOrStderr(), "tq pack: "+werr.Error())
				exitFunc(2)
				return nil
			}
			in, _ := packInput(cwd, ws) // an invalid repo file keeps the manifest crew
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
			cwd, _ := os.Getwd()
			ws, werr := packWorkspace(cwd)
			if werr != nil {
				fmt.Fprintln(c.ErrOrStderr(), "tq pack: "+werr.Error())
				exitFunc(2)
				return nil
			}
			if ws == nil {
				if asJSON {
					return json.NewEncoder(c.OutOrStdout()).Encode(map[string]any{"schema": packs.Schema, "workspace": "", "approvals": []any{}})
				}
				fmt.Fprintln(c.OutOrStdout(), "no pack approvals (not in a workspace)")
				return nil
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
				cwd, _ := os.Getwd()
				in, err := packContext(cwd)
				if err != nil {
					return usageErr("%v", err)
				}
				return writeBaselines(c.Context(), c.OutOrStdout(), in, pack, in.Classes())
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
			if r.Status == packs.Skip || (r.Status == packs.Fail && len(r.Findings) == 0) {
				return usageErr("%s did not run cleanly (%s: %s); the %s baseline is left as it is", r.Tool.Name, r.ID, r.Detail, p)
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
