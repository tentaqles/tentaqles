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
)

func newSettingsCmd() *cobra.Command {
	c := &cobra.Command{Use: "settings", Short: "Render each client's Claude settings, PATH mask and browser shims"}
	c.AddCommand(newSettingsRenderCmd())
	return c
}

func newSettingsRenderCmd() *cobra.Command {
	var all, dryRun, noStatus bool
	c := &cobra.Command{
		Use:   "render [client...]",
		Short: "Write each client's Claude settings, PATH mask and browser shims",
		Long: `Brings each client's files in line with its manifest:

  - its Claude settings.json: the manifest's claude.env and the tq status line
    (which replaces only an unpinned npx status line);
  - its PATH mask: a stub for each CLI of a provider outside its scope;
  - its browser shims, when the client lists a browser.

Only entries tq wrote earlier are ever removed; anything added by hand stays.
--dry-run writes nothing.`,
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
				return fmt.Errorf("name one or more clients, or pass --all")
			}
			want := map[string]bool{}
			for _, a := range args {
				want[a] = true
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
				dir := paths.IdentityDir(ws.Name, "claude")
				res, err := claudesettings.Render(dir, claudesettings.Desired(ws.Manifest, tqExe), dryRun)
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
				renderScope(c, ws, dryRun)
			}
			if n == 0 {
				return fmt.Errorf("no matching client")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "render every client")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing")
	c.Flags().BoolVar(&noStatus, "no-statusline", false, "leave statusLine alone")
	return c
}

// statuslineExe is the tq the status line runs: the installed tq on PATH, since os.Executable
// may be a one-off build (go run, a temp dir) that later disappears.
func statuslineExe() string {
	if exe, err := exec.LookPath("tq"); err == nil {
		return exe
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return ""
}

// renderIdentityFiles renders ws's Claude settings, PATH mask and browser shims, after reach
// wrote or changed its manifest. Errors are warnings: tq doctor reports the drift.
func renderIdentityFiles(c *cobra.Command, ws *resolve.Workspace) {
	if ws == nil {
		return
	}
	dir := paths.IdentityDir(ws.Name, "claude")
	if _, err := os.Stat(dir); err == nil {
		d := claudesettings.Desired(ws.Manifest, statuslineExe())
		if _, err := claudesettings.Render(dir, d, false); err != nil {
			fmt.Fprintf(c.ErrOrStderr(), "warning: %s: Claude settings not rendered: %v (run tq settings render %s)\n", ws.Name, err, ws.Name)
		}
	}
	renderScope(c, ws, false)
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

// gitEmailIn returns the effective git email in dir, or "" (fast-fail: a status line must never
// hang).
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
