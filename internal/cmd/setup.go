package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/detect"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/hooks"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/setup"
)

// setupRunGit is the seam tests use to fake git for `tq setup --from`.
var setupRunGit = gitcfg.RunGit

// setupOutput is what --json prints: the preview, tool check, and (once
// applied) the report, all together.
type setupOutput struct {
	Preview   []setup.Change             `json:"preview"`
	ToolCheck map[string][]detect.Result `json:"toolcheck"`
	Report    *setup.Report              `json:"report,omitempty"`
}

// declinedOutput is what --json prints when the human declines the
// confirmation: the same preview/toolcheck plus an explicit applied:false,
// so a caller never has to infer it from a missing report.
type declinedOutput struct {
	Preview   []setup.Change             `json:"preview"`
	ToolCheck map[string][]detect.Result `json:"toolcheck"`
	Applied   bool                       `json:"applied"`
}

func loadProviderCatalog(errw func(format string, a ...any)) *providers.Catalog {
	cat, err := providers.Load()
	if err != nil {
		errw("warning: failed to load providers (%v); falling back to embedded catalog\n", err)
		return providers.MustLoad()
	}
	return cat
}

// setupAbout is the one line the human confirms before a plan is applied.
func setupAbout(plan *setup.SetupPlan, writePlan string) string {
	names := make([]string, 0, len(plan.Companies))
	for _, co := range plan.Companies {
		names = append(names, co.Name)
	}
	trusted := "untrusted"
	if plan.Trust {
		trusted = "trusted"
	}
	about := fmt.Sprintf("tq setup: about to create %d workspace(s) under %s (%s, %s), register the base in %s and set the git include", len(names), plan.Base, strings.Join(names, ", "), trusted, paths.Config())
	if len(plan.Hooks) > 0 {
		about += ", and write the activation block into the " + strings.Join(plan.Hooks, ", ") + " profile(s)"
	}
	if writePlan != "" {
		about += ", and save the plan to " + writePlan
	}
	return about
}

func newSetupCmd() *cobra.Command {
	var example bool
	var from string
	var writePlan string
	var dryRun, yes, asJSON bool

	c := &cobra.Command{
		Use:   "setup",
		Short: "Scaffold multiple client workspaces from a plan (human only)",
		Long: "tq setup creates several client workspaces at once, from the wizard or from a plan\n" +
			"file (--from). It is human-only: a session is refused, and you confirm before anything\n" +
			"is written. --yes no longer skips that confirmation; it is accepted for older scripts.",
		RunE: func(c *cobra.Command, _ []string) error {
			out := c.OutOrStdout()

			if example {
				fmt.Fprint(out, setup.ExampleYAML())
				return nil
			}
			if err := refuseAgent("tq setup"); err != nil {
				return err
			}

			cat := loadProviderCatalog(func(format string, a ...any) {
				fmt.Fprintf(c.ErrOrStderr(), format, a...)
			})

			var plan *setup.SetupPlan

			if from == "" {
				wizardPlan, err := runWizard(cat, hooks.ProfilesFn())
				if err != nil {
					return err
				}
				plan = wizardPlan
				if err := humanGate(c, "tq setup", setupAbout(plan, writePlan)); err != nil {
					return err
				}
				if err := saveWizardPlan(plan, writePlan); err != nil {
					fmt.Fprintf(c.ErrOrStderr(), "warning: failed to save plan: %v\n", err)
				}
				applyReport, err := setup.Apply(plan, cat, setup.ApplyOptions{RunGit: setupRunGit, Profiles: hooks.ProfilesFn()})
				printReport(out, applyReport)
				if err != nil {
					fmt.Fprintf(c.ErrOrStderr(), "error: %v\n", err)
					exitFunc(1)
				}
				return nil
			}

			loadedPlan, err := setup.LoadPlan(from)
			if err != nil {
				return err
			}
			plan = loadedPlan

			if err := plan.Validate(cat); err != nil {
				return err
			}

			changes, err := setup.Preview(plan, hooks.ProfilesFn())
			if err != nil {
				return err
			}

			toolcheck := setup.ToolCheck(plan, cat, detect.DefaultDeps())

			if !asJSON {
				printPreview(out, changes)
				printToolCheck(out, cat, toolcheck)
			}

			if dryRun {
				if asJSON {
					return json.NewEncoder(out).Encode(setupOutput{Preview: changes, ToolCheck: toolcheck})
				}
				return nil
			}

			// The confirmation is mandatory, with or without --yes. With
			// --json its line goes to stderr so stdout stays one JSON document.
			aboutW := c.OutOrStdout()
			if asJSON {
				aboutW = c.ErrOrStderr()
			}
			if err := humanonly.Gate("tq setup", setupAbout(plan, ""), aboutW); err != nil {
				if asJSON && errors.Is(err, humanonly.ErrAborted) {
					_ = json.NewEncoder(out).Encode(declinedOutput{Preview: changes, ToolCheck: toolcheck, Applied: false})
				}
				return err
			}

			report, err := setup.Apply(plan, cat, setup.ApplyOptions{RunGit: setupRunGit, Profiles: hooks.ProfilesFn()})
			if asJSON {
				encErr := json.NewEncoder(out).Encode(setupOutput{Preview: changes, ToolCheck: toolcheck, Report: &report})
				if err != nil {
					exitFunc(1)
					return nil
				}
				return encErr
			}

			printReport(out, report)
			if err != nil {
				fmt.Fprintf(c.ErrOrStderr(), "error: %v\n", err)
				exitFunc(1)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&example, "example", false, "print an example setup plan YAML")
	c.Flags().StringVar(&from, "from", "", "load a setup plan YAML from this file")
	c.Flags().StringVar(&writePlan, "write-plan", "", "also save the wizard's answers as a setup plan YAML to this path")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "preview only; never write anything")
	c.Flags().BoolVar(&yes, "yes", false, "accepted for older scripts; the typed confirmation is always asked")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func printPreview(out interface{ Write([]byte) (int, error) }, changes []setup.Change) {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "KIND\tTARGET\tDETAIL")
	for _, ch := range changes {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", ch.Kind, ch.Target, ch.Detail)
	}
	tw.Flush()
}

func printToolCheck(out interface{ Write([]byte) (int, error) }, cat *providers.Catalog, tc map[string][]detect.Result) {
	deps := detect.DefaultDeps()
	for company, results := range tc {
		fmt.Fprintf(out, "%s:\n", company)
		for _, r := range results {
			p, ok := cat.Get(r.ID)
			switch {
			case !ok || p.CLI == nil:
				fmt.Fprintf(out, "  [n/a] %s — no CLI\n", r.ID)
			case r.Installed:
				version := r.Version
				if version == "" {
					version = "unknown version"
				}
				fmt.Fprintf(out, "  [ok] %s %s\n", r.ID, version)
			default:
				hint := "no install hint available"
				hints := detect.Hints(p, deps.GOOS)
				if len(hints) > 0 && hints[0] != "no CLI to install" {
					hint = hints[0]
				}
				fmt.Fprintf(out, "  [missing] %s → %s\n", r.ID, hint)
			}
		}
	}
}

func printReport(out interface{ Write([]byte) (int, error) }, report setup.Report) {
	for _, ch := range report.Changes {
		fmt.Fprintf(out, "%s %s %s\n", ch.Kind, ch.Target, ch.Detail)
	}
	for _, w := range report.Warnings {
		fmt.Fprintf(out, "warning: %s\n", w)
	}
	if len(report.Logins) > 0 {
		fmt.Fprintln(out, "Next — run these logins:")
		for _, l := range report.Logins {
			fmt.Fprintf(out, "  %s\n", l)
		}
	}
}
