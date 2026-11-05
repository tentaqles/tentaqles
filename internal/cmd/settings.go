package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/claudesettings"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/statusline"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func newSettingsCmd() *cobra.Command {
	c := &cobra.Command{Use: "settings", Short: "Manage the tq-owned part of each identity's Claude settings.json, and the terminals the human-only check accepts"}
	c.AddCommand(newSettingsRenderCmd(), newSettingsAnchorsCmd())
	return c
}

func newSettingsRenderCmd() *cobra.Command {
	var all, dryRun, noStatus bool
	c := &cobra.Command{
		Use:   "render [workspace...]",
		Short: "Write the baseline permissions, env and status line into identity settings.json",
		Long: `Brings each workspace's Claude identity settings.json in line with tq's
baseline plus the manifest's claude.permission_mode / claude.permissions /
claude.env:

  - permissions.defaultMode (manifest permission_mode; empty means auto)
  - permissions.allow: common read-only commands; deny: credential stores
  - env: tuning for costly plugins (e.g. no per-turn Opus security review)
  - statusLine: tq statusline (replaces unpinned npx status lines only)

Only entries tq wrote earlier are ever removed; anything added by hand stays.

Human-only: a session is refused, and you confirm before the files are written
(--dry-run writes nothing and asks nothing).`,
		RunE: func(c *cobra.Command, args []string) error {
			// A dry run is a read (ADR-0005: dry runs are free); a render
			// refuses a session before anything is resolved.
			if !dryRun {
				if err := refuseAgent("tq settings render"); err != nil {
					return err
				}
			}
			cfg, err := registry.Load()
			if err != nil {
				return err
			}
			wss, errs := resolve.ListWorkspaces(cfg)
			for _, e := range errs {
				fmt.Fprintln(c.ErrOrStderr(), "warning:", e)
			}
			if !all && len(args) == 0 {
				return fmt.Errorf("name one or more workspaces, or pass --all")
			}
			want := map[string]bool{}
			for _, a := range args {
				want[a] = true
			}
			if !dryRun {
				which := "every registered workspace"
				if !all {
					which = strings.Join(args, ", ")
				}
				about := fmt.Sprintf("tq settings render: about to write settings.json and the Codex files under %s for %s", paths.IdentitiesRoot(), which)
				if err := humanGate(c, "tq settings render", about); err != nil {
					return err
				}
			}
			tqExe := ""
			if !noStatus {
				tqExe = statuslineExe()
			}
			n := 0
			for i := range wss {
				ws := &wss[i]
				if !all && !want[ws.Name] {
					continue
				}
				n++
				if v := trust.Check(ws.Name, ws.Hash); !v.OK {
					fmt.Fprintf(c.OutOrStdout(), "%s: skipped (%s)\n", ws.Name, v.Reason)
					continue
				}
				dir := paths.IdentityDir(ws.Name, "claude")
				d := claudesettings.Desired(ws.Manifest, tqExe)
				res, err := claudesettings.Render(dir, d, dryRun)
				if err != nil {
					return fmt.Errorf("%s: %w", ws.Name, err)
				}
				verb := "updated"
				switch {
				case !res.Changed:
					verb = "up to date"
				case dryRun:
					verb = "would change"
				}
				fmt.Fprintf(c.OutOrStdout(), "%s: %s (%s)\n", ws.Name, verb, res.Path)
				for _, ch := range res.Changes {
					fmt.Fprintf(c.OutOrStdout(), "  %s\n", ch)
				}
				codexChanged := renderCodex(c, ws, dryRun)
				if !dryRun && (res.Changed || codexChanged) {
					// settings.json and the Codex files are covered by the
					// integrity record: a human's render signs it again,
					// an agent session leaves it stale.
					resignCovered(c, ws)
				}
			}
			if n == 0 {
				return fmt.Errorf("no matching workspace")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "render every registered workspace")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing")
	c.Flags().BoolVar(&noStatus, "no-statusline", false, "leave statusLine alone")
	return c
}

// statuslineExe is the tq the status line runs: the installed tq on PATH,
// since os.Executable may be a one-off build (go run, a temp dir) that later
// disappears.
func statuslineExe() string {
	if exe, err := exec.LookPath("tq"); err == nil {
		return exe
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return ""
}

// renderIdentityFiles re-renders ws's Claude identity settings (for a
// trusted workspace) and its Codex files after a trust change, so they
// match at once rather than at the next tq settings render. Errors are
// warnings: tq doctor reports the drift.
func renderIdentityFiles(c *cobra.Command, ws *resolve.Workspace) {
	renderIdentityFilesFor(c, ws, false)
}

// renderIdentityFilesFor is renderIdentityFiles; force renders the Claude
// settings whether or not ws is trusted yet. tq allow forces it: it renders
// for the manifest it is about to sign, before the record that covers the
// rendered files is written.
func renderIdentityFilesFor(c *cobra.Command, ws *resolve.Workspace, force bool) {
	if ws == nil {
		return
	}
	if force || trust.IsTrusted(ws.Name, ws.Hash) {
		dir := paths.IdentityDir(ws.Name, "claude")
		if _, err := os.Stat(dir); err == nil {
			d := claudesettings.Desired(ws.Manifest, statuslineExe())
			if _, err := claudesettings.Render(dir, d, false); err != nil {
				fmt.Fprintf(c.ErrOrStderr(), "warning: %s: Claude settings not re-rendered: %v (run tq settings render %s)\n", ws.Name, err, ws.Name)
			}
		}
	}
	renderCodex(c, ws, false)
}

// resignCovered signs ws's integrity record again after a tq command wrote
// a covered file (settings.json#tq, the Codex hook files) for a workspace
// that was trusted when the command started. Signing is a human act and
// takes the same gate as tq allow (refuseAgent: the three detectors); a
// refused re-sign leaves the record stale and names what the human runs.
func resignCovered(c *cobra.Command, ws *resolve.Workspace) {
	if ws == nil {
		return
	}
	if err := refuseAgent("tq: sign the integrity record of " + ws.Name); err != nil {
		fmt.Fprintf(c.ErrOrStderr(), "%s: a covered file changed but the integrity record was not signed again (%v); it is stale until a human runs: tq hooks verify --accept %s\n", ws.Name, err, ws.Name)
		return
	}
	if err := trust.Allow(ws.Name, ws.Hash); err != nil {
		fmt.Fprintf(c.ErrOrStderr(), "warning: %s: integrity record not signed again: %v (run tq hooks verify --accept %s)\n", ws.Name, err, ws.Name)
	}
}

func newStatuslineCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "statusline",
		Short:  "Claude Code status line: client, git email, model, context size, cost",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var in statusline.Input
			_ = json.NewDecoder(c.InOrStdin()).Decode(&in)
			cwd := in.Workspace.CurrentDir
			if cwd == "" {
				cwd = in.Cwd
			}
			if cwd == "" {
				cwd, _ = os.Getwd()
			}
			var f statusline.Facts
			if cfg, err := registry.Load(); err == nil {
				if ws := resolve.Resolve(cwd, cfg).Workspace; ws != nil {
					f.Client = ws.Manifest.Client
				}
			}
			f.GitEmail = gitEmailIn(cwd)
			fmt.Fprintln(c.OutOrStdout(), statusline.Render(in, f, os.Getenv("NO_COLOR") == ""))
			return nil
		},
	}
}

// gitEmailIn returns the effective git email in dir, or "" (fast-fail: a
// status line must never hang).
func gitEmailIn(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "config", "user.email")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
