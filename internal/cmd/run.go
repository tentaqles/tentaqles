package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/guardplugin"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// claudeSubcommands are claude's subcommands (claude --help, "Commands:").
// The launcher adds no permission flag in front of them.
var claudeSubcommands = []string{"agents", "attach", "auth", "auto-mode", "doctor", "gateway", "import", "install",
	"logs", "mcp", "plugin", "plugins", "purge", "respawn", "rm", "setup-token", "stop", "kill", "ultrareview",
	"update", "upgrade"}

// launchClaudeArgs is the launcher's claude command line: it adds
// --dangerously-skip-permissions (tq sets no permission mode of its own
// beyond this skip; the identity and secret rules run in the hook in every
// mode). A claude --permission-mode or --dangerously-skip-permissions typed
// on this command line replaces it, and claude's subcommands, --help and --version get
// nothing. An agent's nested claude (agent true) never gets bypass from the
// launcher: it keeps claude's own prompts.
func launchClaudeArgs(args []string, agent bool) []string {
	if agent {
		return args
	}
	if len(args) > 0 && slices.Contains(claudeSubcommands, args[0]) {
		return args
	}
	for _, a := range args {
		switch {
		case a == "--permission-mode", strings.HasPrefix(a, "--permission-mode="),
			a == "--dangerously-skip-permissions",
			a == "--help", a == "-h", a == "--version", a == "-v":
			return args
		}
	}
	return append([]string{"--dangerously-skip-permissions"}, args...)
}

// claudeIsSession reports whether claude's arguments start a session: not one of its
// subcommands, and not --help or --version. Only a session needs the guard (REQ-4, REQ-6):
// `claude plugin install` and `claude --version` run with none, so the fix line itself runs.
func claudeIsSession(args []string) bool {
	if len(args) > 0 && slices.Contains(claudeSubcommands, args[0]) {
		return false
	}
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "--version" || a == "-v" {
			return false
		}
	}
	return true
}

// guardRefusal is the refusal of a session that would start with no tentaqles guard:
// the reason in one line, then the fix to run, one copyable line each, in order. It is the error of
// tq run, not of claude.
func guardRefusal(ws string, g guardplugin.GuardHook) error {
	return fmt.Errorf("claude won't start a session for %s without the tentaqles guard: %s\n  %s",
		ws, g.Why, fixBlock(guardplugin.GuardHookFix(ws, g)))
}

// fixBlock is the fix of a refusal: one line after "fix:", or the lines after "fix, in this order:".
func fixBlock(lines []string) string {
	if len(lines) == 1 {
		return "fix: " + lines[0]
	}
	return "fix, in this order:\n    " + strings.Join(lines, "\n    ")
}

// agentProg names the agent a program is: "claude" for that name, with or
// without a directory and a .exe/.cmd/.bat/.ps1 extension, and "" for any other.
func agentProg(name string) string {
	b := strings.ToLower(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		b = strings.TrimSuffix(b, ext)
	}
	if b == "claude" {
		return b
	}
	return ""
}

// execCommand runs name+args with env and the process's stdio, and exits
// with the child's code when it fails. Tests replace it, so no real claude ever runs.
var execCommand = func(name string, args, env []string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.ExitCode())
	}
	return err
}

// launch is one tq run: ws (nil: neutral) and the command.
type launch struct {
	ws   *resolve.Workspace
	name string
	args []string
}

// run starts the launch with ws's environment (envplan.Environ), claude
// through launchClaudeArgs.
func (l launch) run() error {
	agent := insideAgent()
	args := l.args
	switch agentProg(l.name) {
	case "claude":
		// A session in an identity's folder starts only with the guard: the plugin's PreToolUse
		// hook in the Claude config the session will run with, CLAUDE_CONFIG_DIR as the identity
		// environment sets it, and no project settings of the folders it starts in switching it
		// off. A neutral folder is unchanged.
		if l.ws != nil && claudeIsSession(args) {
			// A working folder that cannot be read (Getwd fails: cwd is "") is refused by the check itself.
			cwd, _ := os.Getwd()
			if g := guardplugin.CheckSessionGuard(envplan.Desired(l.ws)["CLAUDE_CONFIG_DIR"], cwd, l.ws.Root); !g.OK {
				return guardRefusal(l.ws.Name, g)
			}
		}
		args = launchClaudeArgs(args, agent)
	}
	env := envplan.Environ(l.ws, os.Environ())
	name := l.name
	if layer := envplan.LayerFor(l.ws); layer != nil {
		// An out-of-scope CLI is refused however it is spelled (aws,
		// aws.exe, its install path); anything else resolves on the identity PATH, not on tq's
		// own, so tq run <ws> -- aws from outside the folder does not find the aws it dropped.
		if layer.IsMasked(name) {
			return fmt.Errorf("%s: command not found on the PATH of identity %s (outside its scope)", name, l.ws.Name)
		}
		p, err := envplan.LookPath(name, env)
		if err != nil {
			return fmt.Errorf("%s: command not found on the PATH of identity %s (%v)", name, l.ws.Name, err)
		}
		name = p
	}
	return execCommand(name, args, env)
}

// execIn runs name+args with ws's environment. applyClaudeArgs must be false for
// flows that drive claude's own subcommands (e.g. `tq login <ws> claude`), where
// injecting --dangerously-skip-permissions is wrong; true is the launcher
// (launch.run).
func execIn(ws *resolve.Workspace, name string, args []string, applyClaudeArgs bool, errw func(string)) error {
	if applyClaudeArgs {
		return launch{ws: ws, name: name, args: args}.run()
	}
	return execCommand(name, args, envplan.Environ(ws, os.Environ()))
}

// cwdWorkspace resolves `.`: the current folder's client, or nil when the folder is neutral.
func cwdWorkspace() (*resolve.Workspace, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	cwd, _ := os.Getwd()
	return resolve.Resolve(cwd, cfg).Workspace, nil
}

// findWorkspace is the client named name.
func findWorkspace(name string) (*resolve.Workspace, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	return resolve.Find(cfg, name)
}

// insideAgent reports whether tq runs inside a Claude Code session (Claude Code sets CLAUDECODE
// for the commands it runs). It is a convenience check, not a boundary: a session can unset it.
func insideAgent() bool { return os.Getenv("CLAUDECODE") != "" }

// refuseOtherWorkspace refuses tq run <name> from inside a Claude Code session unless name is the
// client of the folder the command runs in: a session runs the identity of its own folder, never
// another client's. A developer's own terminal runs any client.
func refuseOtherWorkspace(name string) error {
	if !insideAgent() {
		return nil
	}
	if cw, err := cwdWorkspace(); err == nil && cw != nil && cw.Name == name {
		return nil
	}
	return errors.New("tq run: " + name + " is another client. A Claude Code session can only run under the client of its own folder.")
}
func newRunCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "run <client>|. -- <command> [args...]",
		Short: "Run a command with a client's identity without cd-ing into its folder",
		Long: "Run a command with a client's identity. `.` is the current folder's client (none in a neutral folder).\n" +
			"For claude, the launcher starts in bypass, and refuses to start a session without the tentaqles plugin.\n" +
			"From inside a Claude Code session, only the client of the current folder runs.",
		Example: "  tq run acme -- claude\n  tq run . -- claude -p \"...\"",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			// Flags are not interspersed: after <workspace> only an optional
			// -- may come before the command word, and everything after it
			// is the command's own.
			rest := args[1:]
			if len(rest) > 0 && rest[0] == "--" {
				rest = rest[1:]
			}
			args = append(args[:1:1], rest...)
			if len(args) < 2 {
				return errors.New("usage: tq run <client>|. -- <command> [args...]")
			}
			var ws *resolve.Workspace
			if args[0] == "." {
				w, err := cwdWorkspace()
				if err != nil {
					return err
				}
				ws = w
			} else {
				if err := refuseOtherWorkspace(args[0]); err != nil {
					return err
				}
				w, err := findWorkspace(args[0])
				if err != nil {
					return err
				}
				ws = w
			}
			return launch{ws: ws, name: args[1], args: args[2:]}.run()
		},
	}
	c.Flags().SetInterspersed(false)
	return c
}
