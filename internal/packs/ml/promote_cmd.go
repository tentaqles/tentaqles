package ml

// tq pack ml promote request|apply|status and tq pack ml drift.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
)

// exitFunc ends the process with a code (tests replace it).
var exitFunc = os.Exit

// stdinTTY reports whether stdin is a terminal (tests replace it).
var stdinTTY = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// envFor is the env of a tq pack ml subcommand, from packs.FromCommand.
func envFor(c *cobra.Command) (*promoteEnv, error) {
	in, err := packs.FromCommand(c)
	if err != nil {
		return nil, err
	}
	return promoteEnvFor(in)
}

// stdinFile is the command's stdin as a file, for approval.Confirm's own
// terminal check (anything else is not a terminal).
func stdinFile(c *cobra.Command) *os.File {
	f, _ := c.InOrStdin().(*os.File)
	return f
}

func newPromoteCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "promote",
		Short: "Gate model promotions to protected stages or aliases behind a human approval",
		Long: `A promotion to a target in packs.ml.promotion.protected needs a hub
approval a human answers. request (agents may run it) checks the model card,
drift and reproducibility, binds the evidence and asks; the human answers in
their own terminal with tq hub answer (never the board, never an agent);
apply (human only) re-checks the evidence, shows the declared promotion
command, asks for a typed yes, claims the approval and runs it.`,
	}
	c.AddCommand(newPromoteRequestCmd(), newPromoteApplyCmd(), newPromoteStatusCmd())
	return c
}

func newPromoteRequestCmd() *cobra.Command {
	var to string
	var asJSON bool
	c := &cobra.Command{
		Use:   "request <model> <version> --to <target>",
		Short: "Check the model and ask a human to approve its promotion",
		// It only asks (approval.Ask); tq/human-answer lets agents run it.
		Annotations: map[string]string{packs.AnnotationHumanOnly: packs.ApprovalAsk},
		Args:        cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			if to == "" {
				return errors.New("--to <target> is required")
			}
			e, err := envFor(c)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			res, err := promoteRequest(ctx, e, args[0], args[1], to)
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			if asJSON {
				return json.NewEncoder(out).Encode(map[string]any{"schema": "tq.ml-promote/v1", "model": res.Model, "version": res.Version,
					"target": res.Target, "protected": res.Protected, "decision": res.Decision, "card": res.Card, "evidence": evidenceNames(res.Evidence),
					"require": res.Require, "clamped": nonNil(res.Clamped)})
			}
			if !res.Protected {
				fmt.Fprintf(out, "%s is not protected, no approval needed\n", to)
				return nil
			}
			printClamped(c.ErrOrStderr(), res.Clamped)
			fmt.Fprintf(out, "asked: decision %s on card %s\n", res.Decision, res.Card)
			fmt.Fprintf(out, "checks run: %s\n", strings.Join(res.Require, ", "))
			fmt.Fprintf(out, "evidence: %s\n", strings.Join(evidenceNames(res.Evidence), ", "))
			fmt.Fprintf(out, "a human answers it in their own terminal (agents never answer it):\n  tq hub answer %s %s --choice approve\nthen runs, in that terminal:\n  tq pack ml promote apply %s\n", res.Card, res.Decision, res.Decision)
			return nil
		},
	}
	c.Flags().StringVar(&to, "to", "", "the target stage or alias")
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// printClamped names the tq-packs.yaml values load tightened.
func printClamped(w io.Writer, clamped []string) {
	for _, c := range clamped {
		fmt.Fprintln(w, "note: "+c)
	}
}

func evidenceNames(ev map[string]string) []string {
	out := []string{}
	for k := range ev {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func newPromoteApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply <decision>",
		Short: "Human only: verify the approval, show the promotion command, and run it after a typed yes",
		// The executor: HumanOnly, Confirm, Claim; tq/human-answer denies it to agents.
		Annotations: map[string]string{packs.AnnotationHumanOnly: packs.HumanOnlyExecute},
		Args:        cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			in, err := packs.FromCommand(c)
			if err != nil {
				return err
			}
			if err := packs.HumanOnly(); err != nil {
				return err
			}
			e, err := promoteEnvFor(in)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			out := c.OutOrStdout()
			confirm := func(cmd string) error { return approval.Confirm(c, stdinFile(c), out, cmd) }
			return promoteApply(ctx, c, e, args[0], confirm, out)
		},
	}
}

func newPromoteStatusCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status [<model>]",
		Short: "The promotion requests of this workspace and their state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			e, err := envFor(c)
			if err != nil {
				return err
			}
			model := ""
			if len(args) == 1 {
				model = args[0]
			}
			rows, err := promoteStatus(e, model)
			if err != nil {
				return err
			}
			return printStatus(c.OutOrStdout(), rows, asJSON)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

func printStatus(out io.Writer, rows []PromotionRow, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(out).Encode(map[string]any{"schema": "tq.ml-promote/v1", "promotions": rows})
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, "no promotion requests")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "DECISION\tMODEL\tVERSION\tTARGET\tSTATE\tEXPIRES")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Decision, r.Model, r.Version, r.Target, r.State, r.ExpiresAt.Format(time.RFC3339))
	}
	return tw.Flush()
}

func newDriftCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "drift <model>",
		Short: "Compare the model's current profile with its reference (exit 0 ok or warn, 1 fail, 2 input error)",
		Long: `Reads packs.ml.drift.reference and current (a CSV or a tq.drift-profile/v1
JSON), computes PSI or Jensen-Shannon per feature, checks the metric bounds
against the model's metrics file, and writes .tq/drift/<model>-result.json
(gitignore .tq/drift/). When current is missing or older than 24h and
drift.current_command is set, tq runs it first (600s); a command that comes
from tq-packs.yaml rather than the manifest needs a typed yes on a terminal.
Output names features and numbers only, never category labels.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			usage := func(err error) error {
				fmt.Fprintln(c.ErrOrStderr(), "tq pack ml drift: "+err.Error())
				exitFunc(2)
				return nil
			}
			e, err := envFor(c)
			if err != nil {
				return usage(err)
			}
			m, ok := e.St.Model(args[0])
			if !ok {
				return usage(fmt.Errorf("model %q is not declared in packs.ml.models", args[0]))
			}
			out := c.OutOrStdout()
			printClamped(c.ErrOrStderr(), e.St.Clamped)
			opts := driftOptions{}
			if f := stdinFile(c); f != nil && stdinTTY() && !packs.AgentSession() {
				opts.Confirm = func(cmd string) error {
					fmt.Fprintln(out, "drift.current_command comes from tq-packs.yaml, not the manifest.")
					return confirmTyped(f, out, cmd)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), CurrentTimeout+time.Minute)
			defer cancel()
			res, err := runDrift(ctx, e.In, e.St, m, opts)
			if err != nil {
				return usage(err) // an input or config error, not a drift verdict
			}
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(res); err != nil {
					return err
				}
			} else {
				printDrift(out, res)
			}
			if res.Status == "fail" {
				exitFunc(1)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print tq.drift-result/v1 JSON")
	return c
}

func printDrift(out io.Writer, r DriftResult) {
	fmt.Fprintf(out, "%s  model %s (%s)\n", r.Status, r.Model, r.Metric)
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "FEATURE\tTYPE\tVALUE\tWARN\tFAIL\tSTATUS")
	for _, f := range r.Features {
		fmt.Fprintf(tw, "%s\t%s\t%g\t%g\t%g\t%s\n", f.Name, f.Type, f.Value, f.Warn, f.Fail, f.Status)
	}
	tw.Flush()
	for _, m := range r.Metrics {
		v := "missing"
		if m.Value != nil {
			v = fmt.Sprintf("%g", *m.Value)
		}
		fmt.Fprintf(out, "metric %s %s (min %g) %s\n", m.Name, v, m.Min, m.Status)
	}
	for _, s := range r.Reasons {
		fmt.Fprintln(out, "fail: "+s)
	}
	fmt.Fprintf(out, "result: %s\n", resultPath(r.Model))
}

// confirmTyped is the drift command's prompt before a current_command that
// comes from tq-packs.yaml. drift is not an executor (agents may run it), so
// this is a plain typed yes on a terminal, not approval.Confirm (which only
// executors may call). The caller sets it only on a terminal outside an
// agent session.
func confirmTyped(in *os.File, out io.Writer, command string) error {
	if packs.AgentSession() || in == nil || !term.IsTerminal(in.Fd()) {
		return errors.New("this command needs a terminal: type yes to confirm the command it runs")
	}
	fmt.Fprintf(out, "tq will run:\n\n  %s\n\nType yes to run it: ", command)
	line, _ := bufio.NewReader(in).ReadString('\n')
	if strings.TrimSpace(line) != "yes" {
		return approval.ErrNotConfirmed
	}
	return nil
}
