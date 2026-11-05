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
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// claudeSubcommands are claude's subcommands (claude --help, "Commands:").
// The launcher adds no permission flag in front of them.
var claudeSubcommands = []string{"agents", "attach", "auth", "auto-mode", "doctor", "gateway", "import", "install",
	"logs", "mcp", "plugin", "plugins", "purge", "respawn", "rm", "setup-token", "stop", "kill", "ultrareview",
	"update", "upgrade"}

// launchClaudeArgs is the launcher's claude command line: it adds
// --dangerously-skip-permissions, whatever claude.permission_mode says (tq
// sets no permission mode of its own beyond this skip, docs/prd.md D-30;
// the identity and secret rules run in the hook in every mode). A
// --permission-mode or --dangerously-skip-permissions typed on this command
// line replaces it, and claude's subcommands, --help and --version get
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

// offClaudeSettings switches the tentaqles plugin off for an off launch
// (--settings merges enabledPlugins per key).
const offClaudeSettings = `{"enabledPlugins":{"tentaqles@tentaqles":false}}`

// offBanner is printed on stderr by every off launch.
const offBanner = "tq: off — main logins, no identity, no guard (git, cloud and MCP use your default accounts)"

// agentProg names the agent a program is: "claude" or "codex" for those
// names, with or without a directory and a .exe/.cmd/.bat/.ps1 extension,
// and "" for any other program.
func agentProg(name string) string {
	b := strings.ToLower(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		b = strings.TrimSuffix(b, ext)
	}
	if b == "claude" || b == "codex" {
		return b
	}
	return ""
}

// offArgs is an off launch's command line: claude in bypass with the plugin
// switched off, codex with approvals and sandbox bypassed. A permission flag
// the user typed replaces the bypass flag; subcommands get nothing.
func offArgs(name string, args []string) []string {
	switch agentProg(name) {
	case "claude":
		if len(args) > 0 && slices.Contains(claudeSubcommands, args[0]) {
			return args
		}
		pre := []string{"--settings", offClaudeSettings}
		if !slices.ContainsFunc(args, func(a string) bool {
			return a == "--permission-mode" || strings.HasPrefix(a, "--permission-mode=") || a == "--dangerously-skip-permissions"
		}) {
			pre = append([]string{"--dangerously-skip-permissions"}, pre...)
		}
		return append(pre, args...)
	case "codex":
		if slices.ContainsFunc(args, func(a string) bool {
			return a == "--dangerously-bypass-approvals-and-sandbox" || a == "--yolo"
		}) {
			return args
		}
		return append([]string{"--dangerously-bypass-approvals-and-sandbox"}, args...)
	}
	return args
}

// execCommand runs name+args with env and the process's stdio, and exits
// with the child's code when it fails. Tests replace it, so no real claude or
// codex ever runs.
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

// run starts the launch with ws's environment (envplan.Environ): claude
// through launchClaudeArgs, codex with the pinned Codex flags when the
// workspace isolates a codex identity.
func (l launch) run() error {
	_, agent := agentSession()
	args := l.args
	switch agentProg(l.name) {
	case "claude":
		args = launchClaudeArgs(args, agent)
	case "codex":
		args = codexArgs(l.ws, args)
	}
	return execCommand(l.name, args, envplan.Environ(l.ws, os.Environ()))
}

// runOff is tq run --off: the main install and logins, no tq. The plugin is
// switched off and the identity environment cleaned; no variable marks the
// launch. It is human-only: refused from a session, confirmed by the human.
func runOff(c *cobra.Command, name string, args []string, errw func(string)) error {
	if err := refuseAgent("tq run --off"); err != nil {
		return err
	}
	if agentProg(name) == "" {
		return fmt.Errorf("tq run --off starts claude or codex only, not %q: tq run --off -- <claude|codex> [args]", name)
	}
	if err := humanGate(c, "tq run --off", "tq run --off: about to start "+name+" with no identity and no guard (your main logins, the plugin off)"); err != nil {
		return err
	}
	errw(offBanner)
	return execCommand(name, offArgs(name, args), envplan.OffEnviron(os.Environ()))
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

// cwdWorkspace resolves `.`: the current folder's trusted workspace, or nil
// (neutral) with a warning when it is untrusted or invalid.
func cwdWorkspace(warn func(string)) (*resolve.Workspace, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	cwd, _ := os.Getwd()
	res := resolve.Resolve(cwd, cfg)
	if res.Workspace == nil && res.Untrusted != nil {
		warn(fmt.Sprintf("workspace %s is untrusted; running without its identity (tq allow %s)", res.Untrusted.Name, res.Untrusted.Name))
	}
	return res.Workspace, nil
}

func newRunCmd() *cobra.Command {
	var off bool
	c := &cobra.Command{
		Use:   "run <workspace>|. -- <command> [args...]",
		Short: "Run a command with a workspace's identity without cd-ing into it",
		Long: "Run a command with a workspace's identity. `.` is the current folder's workspace (neutral when none).\n" +
			"For claude, the launcher starts in bypass (tq's guard is the hook: identity drift and secrets, in every\n" +
			"permission mode). `tq run --off -- <claude|codex>` starts it with no tq at all: the main install and\n" +
			"logins, no identity, the plugin switched off.",
		Example: "  tq run acme -- claude\n  tq run . -- claude -p \"...\"\n  tq run --off -- claude",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			warn := func(s string) { fmt.Fprintln(c.ErrOrStderr(), "warning:", s) }
			if off {
				if c.ArgsLenAtDash() > 0 {
					return errors.New("tq run --off takes no workspace: tq run --off -- <claude|codex> [args]")
				}
				return runOff(c, args[0], args[1:], func(s string) { fmt.Fprintln(c.ErrOrStderr(), s) })
			}
			// Flags are not interspersed: after <workspace> only an optional
			// -- may come before the command word, and everything after it
			// is the command's own.
			rest, err := runTail(args[1:])
			if err != nil {
				return err
			}
			args = append(args[:1:1], rest...)
			if len(args) < 2 {
				return errors.New("usage: tq run <workspace>|. -- <command> [args...]")
			}
			var ws *resolve.Workspace
			if args[0] == "." {
				w, err := cwdWorkspace(warn)
				if err != nil {
					return err
				}
				ws = w
			} else {
				w, err := findWorkspace(args[0])
				if err != nil {
					return err
				}
				if !trust.IsTrusted(w.Name, w.Hash) {
					return fmt.Errorf("workspace %s is untrusted; run: tq allow %s", w.Name, w.Name)
				}
				ws = w
			}
			return launch{ws: ws, name: args[1], args: args[2:]}.run()
		},
	}
	c.Flags().BoolVar(&off, "off", false, "start claude or codex with no tq: main install and logins, no identity, the plugin off (human only)")
	c.Flags().SetInterspersed(false)
	return c
}

// runTail reads what follows <workspace> in tq run: an optional --, then
// the command and its args.
func runTail(args []string) ([]string, error) {
	if len(args) > 0 {
		switch a := args[0]; {
		case a == "--":
			return args[1:], nil
		case a == "--off" || strings.HasPrefix(a, "--off="):
			return nil, errors.New("tq run --off takes no workspace: tq run --off -- <claude|codex> [args]")
		}
	}
	return args, nil
}
