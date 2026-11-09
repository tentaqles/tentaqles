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
	"github.com/tentaqles/tentaqles/internal/codexconfig"
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
// beyond this skip; the identity check runs in the hook in every mode). A claude --permission-mode or --dangerously-skip-permissions typed
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
// subcommands, and not --help or --version. Only a session needs the guard:
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

// codexSubcommands are the codex subcommands that run no agent session: they run no tool of the
// agent. Everything else is a session, so a subcommand this list does not know (a new one, say)
// needs the guard. exec, review, resume, fork, app-server, app, remote-control, sandbox, debug,
// apply, cloud, queue and exec-server run tools or apply changes, so they are sessions
// (codex --help, "Commands:").
var codexSubcommands = []string{"agents", "archive", "completion", "delete", "doctor", "features", "help",
	"login", "logout", "mcp", "migrate-rollouts", "plugin", "unarchive", "update"}

// codexIsSession reports whether codex's arguments start a session: not one of the subcommands above,
// and not --help or --version. The launcher adds nothing to codex's arguments.
func codexIsSession(args []string) bool {
	if len(args) > 0 && slices.Contains(codexSubcommands, args[0]) {
		return false
	}
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "--version" || a == "-V" {
			return false
		}
	}
	return true
}

// codexHooksOff reports the first codex option in args that turns the hooks off for the session:
// --disable hooks, --disable codex_hooks, and -c/--config features.hooks=false or
// features.codex_hooks=false (also a features table that sets either to false). Each form is
// matched with its value after a space, after =, or attached (-cfeatures.hooks=false). The
// tentaqles guard runs in those hooks, so a session with one of them has no guard.
func codexHooksOff(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		var flag, value string
		switch {
		case a == "--disable" || a == "--config" || a == "-c":
			flag = a
			if i+1 < len(args) {
				value = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--disable="):
			flag, value = "--disable", strings.TrimPrefix(a, "--disable=")
		case strings.HasPrefix(a, "--config="):
			flag, value = "--config", strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-c") && !strings.HasPrefix(a, "--"):
			flag, value = "-c", strings.TrimPrefix(strings.TrimPrefix(a, "-c"), "=")
		default:
			continue
		}
		switch flag {
		case "--disable":
			if value == "hooks" || value == "codex_hooks" {
				return flag + " " + value, true
			}
		default:
			if configTurnsHooksOff(value) {
				return flag + " " + value, true
			}
		}
	}
	return "", false
}

// configTurnsHooksOff reports a -c value (key=value, the value TOML) that sets a hook switch to false.
func configTurnsHooksOff(v string) bool {
	v = strings.ReplaceAll(v, " ", "")
	key, val, ok := strings.Cut(v, "=")
	if !ok {
		return false
	}
	if key == "features" {
		return strings.Contains(val, "hooks=false")
	}
	if rest, found := strings.CutPrefix(key, "features."); found {
		return (rest == "hooks" || rest == "codex_hooks") && val == "false"
	}
	return false
}

// codexGuardRefusal is the refusal of a codex session that would start with no tentaqles guard, in
// the same form as guardRefusal. The fix is the one line the check gives: tentaqles settings render.
func codexGuardRefusal(ws string, g codexconfig.Guard) error {
	return fmt.Errorf("codex won't start a session for %s without the tentaqles guard: %s\n  %s",
		ws, g.Why, fixBlock(g.Fix))
}

// guardRefusal is the refusal of a session that would start with no tentaqles guard:
// the reason in one line, then the fix to run, one copyable line each, in order. It is the error of
// tentaqles run, not of claude.
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

// agentProg names the agent a program is: "claude" or "codex" for that name, with or without a
// directory and a .exe/.cmd/.bat/.ps1 extension, and "" for any other.
func agentProg(name string) string {
	b := strings.ToLower(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		b = strings.TrimSuffix(b, ext)
	}
	switch b {
	case "claude", "codex":
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

// launch is one tentaqles run: ws (nil: neutral) and the command.
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
	case "codex":
		// A codex session in an identity's folder starts only with the tentaqles hooks in the Codex
		// config it runs with (CODEX_HOME, enabled and trusted), and no option that turns the hooks
		// off for the session. A neutral folder, or a client without codex, is unchanged here.
		if l.ws != nil && codexIsSession(args) {
			if opt, off := codexHooksOff(args); off {
				return fmt.Errorf("codex won't start a session for %s without the tentaqles guard: %s turns the Codex hooks off for this session\n  fix: run it without that option",
					l.ws.Name, opt)
			}
			if g := codexconfig.CheckGuard(envplan.Desired(l.ws)["CODEX_HOME"], l.ws.Name); !g.OK {
				return codexGuardRefusal(l.ws.Name, g)
			}
		}
	}
	env := envplan.Environ(l.ws, os.Environ())
	name := l.name
	if layer := envplan.LayerFor(l.ws); layer != nil {
		// An out-of-scope CLI is refused however it is spelled (aws,
		// aws.exe, its install path); anything else resolves on the identity PATH, not on tq's
		// own, so tentaqles run <ws> -- aws from outside the folder does not find the aws it dropped.
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
// flows that drive claude's own subcommands (e.g. `tentaqles login <ws> claude`), where
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

// refuseOtherWorkspace refuses tentaqles run <name> from inside a Claude Code session unless name is the
// client of the folder the command runs in: a session runs the identity of its own folder, never
// another client's. A developer's own terminal runs any client.
func refuseOtherWorkspace(name string) error {
	if !insideAgent() {
		return nil
	}
	if cw, err := cwdWorkspace(); err == nil && cw != nil && cw.Name == name {
		return nil
	}
	return errors.New("tentaqles run: " + name + " is another client. A Claude Code session can only run under the client of its own folder.")
}
func newRunCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "run <client>|. -- <command> [args...]",
		Short: "Run a command with a client's identity without cd-ing into its folder",
		Long: "Run a command with a client's identity. `.` is the current folder's client (none in a neutral folder).\n" +
			"For claude, the launcher starts in bypass, and refuses to start a session without the tentaqles plugin.\n" +
			"For codex, a session refuses to start without the tentaqles hooks in its Codex config (tentaqles settings render),\n" +
			"and with an option that turns those hooks off (--disable hooks, -c features.hooks=false).\n" +
			"From inside a Claude Code session, only the client of the current folder runs.",
		Example: "  tentaqles run acme -- claude\n  tentaqles run . -- claude -p \"...\"",
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
				return errors.New("usage: tentaqles run <client>|. -- <command> [args...]")
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
