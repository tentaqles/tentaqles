package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// newHooksVerifyCmd is `tq hooks verify [<workspace>] [--accept]`: the
// signed integrity record (PRD v2 §8.3) checked against the files it
// covers, printed as a report of what changed, exit 1 on a mismatch.
// --accept signs the current state again; it is human-only
// (refuseAgent), the same gate as tq allow.
func newHooksVerifyCmd() *cobra.Command {
	var accept bool
	c := &cobra.Command{
		Use:   "verify [workspace]",
		Short: "Check the signed integrity record (manifest, hooks, shims, tq) and list what changed; --accept re-signs (human only)",
		Long: `tq hooks verify checks a workspace's signed integrity record against the files
it covers: the protected manifest and the root marker, the identity's Claude
settings.json, the installed tentaqles plugin's hooks/, scripts/ and
.claude-plugin/, the Codex hooks.json and the tq-owned block of config.toml,
every file under ~/.tentaqles/shims/ and the tq binary that runs. It prints
each change with the old and new hash and exits 1 when anything differs, when
the signature does not verify, or when there is no record.

Without a workspace it checks the current folder's.

--accept signs the state as it is now, after you have read the report:
human-only, an agent session is refused.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if accept {
				if err := refuseAgent("tq hooks verify --accept"); err != nil {
					return err
				}
			}
			ws, err := verifyTarget(args)
			if err != nil {
				return err
			}
			rep, err := trust.Verify(ws.Name)
			if err != nil {
				return err
			}
			w := c.OutOrStdout()
			printVerifyReport(w, ws, rep)
			if accept {
				// The report above is what the human confirms.
				_, err := trustWorkspace(c, ws.Name, "tq hooks verify --accept")
				return err
			}
			switch {
			case !rep.Exists:
				return fmt.Errorf("%s has no integrity record; run: tq allow %s", ws.Name, ws.Name)
			case rep.Invalid != "":
				return fmt.Errorf("%s: %s; a human runs: tq hooks verify --accept %s", ws.Name, rep.Invalid, ws.Name)
			case !rep.Trusted(ws.Hash):
				return fmt.Errorf("%s: integrity mismatch; read the changes above, then a human runs: tq hooks verify --accept %s", ws.Name, ws.Name)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&accept, "accept", false, "sign the current state as trusted (human only)")
	return c
}

// verifyTarget is the workspace named, or the current folder's (trusted or
// not).
func verifyTarget(args []string) (*resolve.Workspace, error) {
	if len(args) == 1 {
		return findWorkspace(args[0])
	}
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	cwd, _ := os.Getwd()
	res := resolve.Resolve(cwd, cfg)
	switch {
	case res.Workspace != nil:
		return res.Workspace, nil
	case res.Untrusted != nil:
		return res.Untrusted, nil
	}
	return nil, errors.New("not in a workspace (" + res.Reason + "): name one, tq hooks verify <workspace>")
}

// printVerifyReport prints the record's state and every change, one per
// line with the kind, the path and the old and new hash prefixes.
func printVerifyReport(w io.Writer, ws *resolve.Workspace, rep trust.Report) {
	switch {
	case !rep.Exists:
		fmt.Fprintf(w, "%s: no integrity record (%s)\n", ws.Name, trust.SignedRecordPath(ws.Name))
		return
	case rep.Invalid != "":
		fmt.Fprintf(w, "%s: %s (%s)\n", ws.Name, rep.Invalid, trust.SignedRecordPath(ws.Name))
		return
	}
	fmt.Fprintf(w, "%s: integrity record signed %s over %d covered files, manifest %s\n", ws.Name, rep.SignedAt, rep.Files, rep.ManifestHash[:12])
	if rep.ManifestHash != ws.Hash {
		fmt.Fprintf(w, "  manifest hash now %s\n", ws.Hash[:12])
	}
	for _, ch := range rep.Changes {
		fmt.Fprintf(w, "  %-8s %s  %s -> %s\n", ch.Kind, ch.Path, shortHash(ch.Old), shortHash(ch.New))
	}
	switch n := len(rep.Changes); {
	case n == 0 && rep.ManifestHash == ws.Hash:
		fmt.Fprintf(w, "%s: integrity ok\n", ws.Name)
	case n == 1:
		fmt.Fprintf(w, "%s: 1 change\n", ws.Name)
	default:
		fmt.Fprintf(w, "%s: %d changes\n", ws.Name, n)
	}
}

func shortHash(h string) string {
	switch {
	case h == "":
		return "(absent)"
	case len(h) > 12 && h[0] != '!':
		return h[:12]
	}
	return h
}
