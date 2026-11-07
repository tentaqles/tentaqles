package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/identitystatus"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
	"github.com/tentaqles/tentaqles/internal/verify"
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
		Short: "The doctor report; with --json this client's setup for the onboarding skill; with --denials the denial log",
		Long: `tq status prints what tq doctor prints (tq doctor stays).

tq status --json prints one JSON object, the setup of the client the current folder
belongs to, and writes nothing: identity, root, scope, one entry per provider in scope
(id, category, cli, cli_resolves, credential, connection, verified), browsers and
other_providers. It is the onboarding skill's read: the skill cannot read the manifest or
the Claude config. credential and connection are present, missing or none; verified is true
only when the provider's read-only verify command passed in this run (5 s each). No token,
credential or MCP env value is in it. In a neutral folder identity is null; in an untrusted
client it holds identity, trusted false and reason, and no provider data. The health report
as JSON is tq doctor --json.

tq status --denials reads ~/.tentaqles/denials.jsonl, where every deny is one
line: time, identity, category, rule, layer (hook, filesystem, human-only, command),
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
				if asJSON {
					if c.Flags().Changed("verify") {
						return fmt.Errorf("--verify goes with the doctor report (tq doctor --json --verify), not with tq status --json")
					}
					return runStatusJSON(c)
				}
				return runDoctor(c, asJSON, verifyMode)
			}
			if err := humanonly.CheckRead("tq status --denials"); err != nil {
				return err
			}
			if last < 0 {
				return fmt.Errorf("--last must be 0 (all) or more")
			}
			ds, err := lastDenials(identity, category, last)
			if err != nil {
				return fmt.Errorf("reading the denial log: %w", err)
			}
			for i := range ds {
				ds[i] = sanitizeDenial(ds[i])
			}
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

// The seams of tq status --json: tests replace them, so no test runs a real CLI.
var (
	statusRunCLI      verify.Runner = runVerifyCmd
	statusLookPath                  = exec.LookPath
	statusVerifyLimit time.Duration // 0: identitystatus.VerifyLimit
)

// statusResolve is resolve.Resolve; a test replaces it with one that blocks, standing in for a
// keychain read that does not answer (internal/trust keeps its keychain seam unexported).
var statusResolve = resolve.Resolve

// statusKeyLimit bounds the resolve of status --json (the integrity key's keychain read).
var statusKeyLimit = identitystatus.KeychainLimit

// resolveWithin runs the resolve of cwd and waits at most limit for it. answered is false when
// it did not finish in time; the resolve goroutine is then left to finish on its own (the command
// prints and exits, and a late answer is discarded).
func resolveWithin(cwd string, cfg *registry.Config, limit time.Duration) (res resolve.Result, answered bool) {
	done := make(chan resolve.Result, 1)
	go func() { done <- statusResolve(cwd, cfg) }()
	t := time.NewTimer(limit)
	defer t.Stop()
	select {
	case r := <-done:
		return r, true
	case <-t.C:
		return resolve.Result{}, false
	}
}

// runStatusJSON prints the setup of the client the current folder belongs to
// (internal/identitystatus). It reads and writes nothing under the tq home.
func runStatusJSON(c *cobra.Command) error {
	defer trust.MemoScope()()
	cfg, err := registry.Load()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// Resolve reads the integrity key from the keychain, with no deadline of its own: it runs
	// under the same limit as a keychain lookup (ADR-0017). A key that does not answer in time
	// gives the untrusted shape, which names the timeout.
	res, answered := resolveWithin(cwd, cfg, statusKeyLimit)
	var st identitystatus.Status
	if answered {
		st = identitystatus.Build(res, identitystatus.Deps{
			LookPath: statusLookPath, RunCLI: statusRunCLI, VerifyLimit: statusVerifyLimit,
		})
	} else {
		name, root := resolve.FolderName(cwd, cfg)
		st = identitystatus.Unanswered(name, root, statusKeyLimit)
	}
	enc := json.NewEncoder(c.OutOrStdout())
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(st)
}

// lastDenials streams the log (audit.EachDenial) and keeps the newest last
// entries of identity and category (either empty: any; last 0: all),
// oldest first, without holding the whole log.
func lastDenials(identity, category string, last int) ([]audit.Denial, error) {
	var out []audit.Denial
	err := audit.EachDenial(func(d audit.Denial) bool {
		if identity != "" && !strings.EqualFold(d.Identity, identity) {
			return true
		}
		if category != "" && !strings.EqualFold(d.Category, category) {
			return true
		}
		out = append(out, d)
		if last > 0 && len(out) > 2*last {
			out = append(out[:0:0], out[len(out)-last:]...)
		}
		return true
	})
	if last > 0 && len(out) > last {
		out = out[len(out)-last:]
	}
	return out, err
}

// sanitizeDenial escapes every control character in every text field (C0
// but the newline, DEL, C1: ESC, OSC, CSI, CR, BEL, ...), so a logged string
// cannot drive the terminal that reads it (an OSC 52 clipboard write, a line
// erase, a fake line). The newline survives only in the reason, which is
// printed one line at a time; in the other fields it is escaped too.
func sanitizeDenial(d audit.Denial) audit.Denial {
	for _, f := range []*string{&d.Category, &d.Layer, &d.Caller, &d.Harness, &d.Tool, &d.Command, &d.Subject, &d.Rule, &d.Identity, &d.Cwd} {
		*f = escapeControl(*f, false)
	}
	d.Reason = escapeControl(d.Reason, true)
	return d
}

// escapeControl writes a C0 control character or DEL as \xNN, and a C1
// control character or a Unicode bidi override (U+202A–U+202E,
// U+2066–U+2069, which can make a line read in another order than it is)
// as \uNNNN; keepNewline leaves a newline as it is.
func escapeControl(s string, keepNewline bool) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' && keepNewline:
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r >= 0x80 && r < 0xa0,
			r >= 0x202a && r <= 0x202e, // bidi embeddings and overrides
			r >= 0x2066 && r <= 0x2069: // bidi isolates
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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
			fmt.Fprintf(w, "    %s\n", d.Command)
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
