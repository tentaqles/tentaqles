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
	c := &cobra.Command{Use: "settings", Short: "Manage the tq-owned part of each identity's Claude settings.json"}
	c.AddCommand(newSettingsRenderCmd())
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

Only entries tq wrote earlier are ever removed; anything added by hand stays.`,
		RunE: func(c *cobra.Command, args []string) error {
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
			tqExe := ""
			if !noStatus {
				// Prefer the installed tq on PATH: os.Executable may be a
				// one-off build (go run, a temp dir) that later disappears.
				if exe, err := exec.LookPath("tq"); err == nil {
					tqExe = exe
				} else if exe, err := os.Executable(); err == nil {
					tqExe = exe
				}
			}
			n := 0
			for i := range wss {
				ws := &wss[i]
				if !all && !want[ws.Name] {
					continue
				}
				n++
				if !trust.IsTrusted(ws.Hash) {
					fmt.Fprintf(c.OutOrStdout(), "%s: skipped (manifest not trusted; run tq allow %s)\n", ws.Name, ws.Name)
					continue
				}
				dir := paths.IdentityDir(ws.Name, "claude")
				d := claudesettings.Desired(ws.Manifest, trust.IsBypassAllowed(ws.Hash), tqExe)
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
