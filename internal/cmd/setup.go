package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/cli/internal/detect"
	"github.com/tentaqles/tentaqles/cli/internal/gitcfg"
	"github.com/tentaqles/tentaqles/cli/internal/hooks"
	"github.com/tentaqles/tentaqles/cli/internal/providers"
	"github.com/tentaqles/tentaqles/cli/internal/setup"
)

// setupRunGit is the seam tests use to fake git for `tq setup --from --yes`.
var setupRunGit = gitcfg.RunGit

// setupIsTTY reports whether stdin is an interactive terminal. Tests
// override it to exercise the non-interactive refusal path.
var setupIsTTY = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// setupConfirm prompts and reads a single line from stdin, treating
// "y"/"yes" (case-insensitive) as confirmation. Tests override it directly
// rather than faking stdin.
var setupConfirm = func(prompt string) (bool, error) {
	fmt.Print(prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false, err
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes", nil
}

// setupOutput is what --json prints: the preview, tool check, and (once
// applied) the report, all together.
type setupOutput struct {
	Preview   []setup.Change             `json:"preview"`
	ToolCheck map[string][]detect.Result `json:"toolcheck"`
	Report    *setup.Report              `json:"report,omitempty"`
}

func loadProviderCatalog(errw func(format string, a ...any)) *providers.Catalog {
	cat, err := providers.Load()
	if err != nil {
		errw("warning: failed to load providers (%v); falling back to embedded catalog\n", err)
		return providers.MustLoad()
	}
	return cat
}

func newSetupCmd() *cobra.Command {
	var example bool
	var from string
	var dryRun, yes, asJSON bool

	c := &cobra.Command{
		Use:   "setup",
		Short: "Scaffold multiple client workspaces from a plan",
		RunE: func(c *cobra.Command, _ []string) error {
			out := c.OutOrStdout()

			if example {
				fmt.Fprint(out, setup.ExampleYAML())
				return nil
			}

			if from == "" {
				fmt.Fprintln(c.ErrOrStderr(), "error: interactive wizard arrives in the next task; use --from <file> or --example")
				exitFunc(1)
				return nil
			}

			plan, err := setup.LoadPlan(from)
			if err != nil {
				return err
			}

			cat := loadProviderCatalog(func(format string, a ...any) {
				fmt.Fprintf(c.ErrOrStderr(), format, a...)
			})

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

			if !yes {
				if !setupIsTTY() {
					return fmt.Errorf("refusing to apply without --yes on a non-interactive stdin")
				}
				ok, err := setupConfirm("Apply these changes? [y/N] ")
				if err != nil {
					return err
				}
				if !ok {
					return nil
				}
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
	c.Flags().BoolVar(&dryRun, "dry-run", false, "preview only; never write anything")
	c.Flags().BoolVar(&yes, "yes", false, "apply without prompting for confirmation")
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
