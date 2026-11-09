package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/claudesettings"
	"github.com/tentaqles/tentaqles/internal/codexconfig"
	"github.com/tentaqles/tentaqles/internal/envplan"
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

  - its Claude settings.json: the manifest's claude.env, the client's language and the
    tentaqles status line (which replaces a missing status line, an @latest or ccstatusline
    command, or a previous tentaqles one; any other status line stays);
  - when codex is in its scope, its Codex config.toml: tentaqles' PreToolUse and SessionStart
    hooks (enabled and trusted) and the client's language line;
  - its PATH mask: a stub for each CLI of a provider outside its scope;
  - its browser shims, when the client lists a browser;
  - the folder of a client that has none recorded (a client created before folders were recorded).

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
				if _, ok := cfg.Folder(ws.Name); !ok {
					if dryRun {
						fmt.Fprintf(c.OutOrStdout(), "%s: would record its folder %s\n", ws.Name, ws.Root)
					} else if err := cfg.Bind(ws.Name, ws.Root); err != nil {
						fmt.Fprintf(c.ErrOrStderr(), "warning: %s: folder not recorded: %v\n", ws.Name, err)
					} else if err := cfg.Save(); err != nil {
						return fmt.Errorf("%s: folder not recorded: %w", ws.Name, err)
					} else {
						fmt.Fprintf(c.OutOrStdout(), "%s: recorded its folder %s\n", ws.Name, ws.Root)
					}
				}
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
				if err := renderCodex(c, ws, dryRun); err != nil {
					return fmt.Errorf("%s: %w", ws.Name, err)
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

// statuslineExe is the binary the status line runs: the installed tentaqles (or its alias tq) on
// PATH, since os.Executable may be a one-off build (go run, a temp dir) that later disappears.
func statuslineExe() string {
	for _, name := range []string{"tentaqles", "tq"} {
		if exe, err := exec.LookPath(name); err == nil {
			return exe
		}
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return ""
}

// renderIdentityFiles renders ws's Claude settings, PATH mask and browser shims, after reach
// wrote or changed its manifest. Errors are warnings: tentaqles doctor reports the drift.
func renderIdentityFiles(c *cobra.Command, ws *resolve.Workspace) {
	if ws == nil {
		return
	}
	dir := paths.IdentityDir(ws.Name, "claude")
	if _, err := os.Stat(dir); err == nil {
		d := claudesettings.Desired(ws.Manifest, statuslineExe())
		if _, err := claudesettings.Render(dir, d, false); err != nil {
			fmt.Fprintf(c.ErrOrStderr(), "warning: %s: Claude settings not rendered: %v (run tentaqles settings render %s)\n", ws.Name, err, ws.Name)
		}
	}
	if err := renderCodex(c, ws, false); err != nil {
		fmt.Fprintf(c.ErrOrStderr(), "warning: %s: codex config not rendered: %v (run tentaqles settings render %s)\n", ws.Name, err, ws.Name)
	}
	renderScope(c, ws, false)
}

// codexScope is the catalog provider whose scope puts a client's Codex config under tentaqles' care.
const codexScope = "codex"

// renderCodex writes a client's Codex config when its scope includes codex: tentaqles' PreToolUse and
// SessionStart hooks, their trust, and the client's language as developer_instructions. A client
// without codex is left alone, and nothing is deleted. dryRun changes nothing and says so: the
// config is not compared, since codexconfig only writes.
func renderCodex(c *cobra.Command, ws *resolve.Workspace, dryRun bool) error {
	if ws == nil || ws.Manifest == nil || !slices.Contains(ws.Manifest.Scope(), codexScope) {
		return nil
	}
	home := envplan.Desired(ws)["CODEX_HOME"]
	if dryRun {
		fmt.Fprintf(c.OutOrStdout(), "%s: codex config not checked (dry run; %s)\n", ws.Name, codexconfig.ConfigPath(home))
		return nil
	}
	program, err := codexHookProgram()
	if err != nil {
		return err
	}
	changed, err := codexconfig.Render(home, codexconfig.Managed{Command: program, DeveloperInstructions: ws.Manifest.LanguageLine()})
	if err != nil {
		return err
	}
	verb := "up to date"
	if changed {
		verb = "written"
	}
	fmt.Fprintf(c.OutOrStdout(), "%s: codex config %s (%s)\n", ws.Name, verb, codexconfig.ConfigPath(home))
	return nil
}

// hookProgramPath is the tentaqles program a Codex hook runs, hookShortPath its 8.3 form and hookGOOS
// the system; tests replace them.
var (
	hookProgramPath = statuslineExe
	hookShortPath   = shortPath
	hookGOOS        = runtime.GOOS
)

// codexHookProgram is the program part of the Codex hook commands: the absolute path of the installed
// tentaqles. Codex treats a hook that fails to start as an allow, so the command has to run. On Windows
// a quoted program fails when Codex starts it (tested with Codex 0.154), so the path is written bare, or
// as its 8.3 short form when it holds a space or a character cmd reads. A path with no safe form is
// refused: no hook is better than one that silently never runs, because tentaqles run then refuses the
// session. Elsewhere Codex uses a POSIX shell, where quotes keep the path one word.
func codexHookProgram() (string, error) {
	exe := hookProgramPath()
	if exe == "" {
		return "", errors.New("cannot find the tentaqles program to write its Codex hooks")
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	if hookGOOS != "windows" {
		if strings.ContainsAny(abs, "\"$`") {
			return "", fmt.Errorf("the tentaqles program path %s holds a character a Codex hook command cannot carry", abs)
		}
		return `"` + abs + `"`, nil
	}
	if cmdSafe(abs) {
		return abs, nil
	}
	if short := hookShortPath(abs); short != "" && cmdSafe(short) {
		return short, nil
	}
	return "", fmt.Errorf("the tentaqles program path %s has a space or a special character and no short name, so a Codex hook could not run it; install tentaqles in a folder without them", abs)
}

// cmdSafe reports whether cmd.exe reads a path as one plain word.
func cmdSafe(p string) bool { return !strings.ContainsAny(p, " \t\"%&|<>^()!;,=") }

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
