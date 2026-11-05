package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/humanonly"
)

// newStatusCmd is tq status, the successor name of tq doctor (DISCOVERY.md
// §7.1; tq doctor stays as it is). Without --denials it prints the doctor
// report; with --denials it reads the denial log (PRD v2 §7.3).
//
// --denials is a human-only READ (ADR-0006): the log holds every identity's
// denials, their folders and redacted commands, so a harness session is
// refused (the env-marker and process-tree detectors; no terminal is needed,
// so a human can pipe --json) and the refusal is logged. It writes nothing.
func newStatusCmd() *cobra.Command {
	var asJSON, denials bool
	var verifyMode, identity, category string
	var last int
	c := &cobra.Command{
		Use:   "status",
		Short: "The doctor report, or with --denials the last entries of the denial log",
		Long: `tq status prints what tq doctor prints (tq doctor stays).

tq status --denials reads ~/.tentaqles/denials.jsonl, where every deny is one
line: time, identity, category, rule, layer (hook, filesystem, human-only),
calling process (harness, subagent, no-tty), harness, tool, the redacted
command, the subject and the reason. It prints the last --last entries (20;
0 for all), oldest first, filtered by --identity and --category; --json
prints them as a JSON array. It is human-only: a harness session is refused
(it needs no terminal, so a human can pipe it).`,
		RunE: func(c *cobra.Command, _ []string) error {
			if !denials {
				if identity != "" || category != "" || c.Flags().Changed("last") {
					return fmt.Errorf("--identity, --category and --last go with --denials")
				}
				return runDoctor(c, asJSON, verifyMode)
			}
			if err := humanonly.CheckRead("tq status --denials"); err != nil {
				return err
			}
			if last < 0 {
				return fmt.Errorf("--last must be 0 (all) or more")
			}
			ds, err := audit.ReadDenials()
			if err != nil {
				return fmt.Errorf("reading the denial log: %w", err)
			}
			ds = filterDenials(ds, identity, category, last)
			if asJSON {
				if ds == nil {
					ds = []audit.Denial{}
				}
				enc := json.NewEncoder(c.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(ds)
			}
			printDenials(c.OutOrStdout(), ds)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	c.Flags().StringVar(&verifyMode, "verify", doctor.VerifyAuto, "as tq doctor --verify (without --denials)")
	c.Flags().BoolVar(&denials, "denials", false, "read the denial log instead of the doctor report (human-only)")
	c.Flags().IntVarP(&last, "last", "n", 20, "with --denials: how many of the newest entries (0: all)")
	c.Flags().StringVar(&identity, "identity", "", "with --denials: only this identity's entries")
	c.Flags().StringVar(&category, "category", "", "with --denials: only this category (identity, secrets, self-protection, tq/guard-error)")
	return c
}

// filterDenials keeps the entries of identity and category (either empty:
// any), then the newest last of them (0: all), oldest first.
func filterDenials(ds []audit.Denial, identity, category string, last int) []audit.Denial {
	var out []audit.Denial
	for _, d := range ds {
		if identity != "" && !strings.EqualFold(d.Identity, identity) {
			continue
		}
		if category != "" && !strings.EqualFold(d.Category, category) {
			continue
		}
		out = append(out, d)
	}
	if last > 0 && len(out) > last {
		out = out[len(out)-last:]
	}
	return out
}

// printDenials prints each denial as a header line (time, identity,
// category/rule, layer, caller, tool), then the redacted command and the
// first line of the reason, indented.
func printDenials(w io.Writer, ds []audit.Denial) {
	if len(ds) == 0 {
		fmt.Fprintln(w, "no denials")
		return
	}
	for _, d := range ds {
		ident := d.Identity
		if ident == "" {
			ident = "-"
		}
		rule := d.Category
		if d.Rule != "" && d.Rule != d.Category {
			rule += "/" + d.Rule
		}
		caller := d.Caller
		if d.Harness != "" {
			caller = strings.TrimPrefix(caller+" "+d.Harness, " ")
		}
		fmt.Fprintf(w, "%s  %s  %s  layer=%s  caller=%s  tool=%s\n", d.Time.Local().Format("2006-01-02 15:04:05"),
			ident, rule, orDash(d.Layer), orDash(caller), orDash(d.Tool))
		if d.Command != "" {
			fmt.Fprintf(w, "    %s\n", strings.ReplaceAll(d.Command, "\n", " ⏎ "))
		}
		reason, _, _ := strings.Cut(d.Reason, "\n")
		fmt.Fprintf(w, "    %s\n", reason)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
