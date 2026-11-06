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
	_, agent := agentSession()
	args := l.args
	switch agentProg(l.name) {
	case "claude":
		args = launchClaudeArgs(args, agent)
	}
	env := envplan.Environ(l.ws, os.Environ())
	name := l.name
	if layer := envplan.LayerFor(l.ws); layer != nil {
		// ADR-0008: an out-of-scope CLI is refused however it is spelled (aws,
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

// cwdWorkspace resolves `.`: the current folder's trusted workspace, or nil
// when the folder is neutral. A folder that is a workspace but untrusted
// is refused (PRD v2 §8.3): the reason names the first changed file and
// the command that fixes it (tq hooks verify after a mismatch, tq allow
// for a never-trusted one). Nothing launches without the identity.
func cwdWorkspace() (*resolve.Workspace, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	cwd, _ := os.Getwd()
	res := resolve.Resolve(cwd, cfg)
	if res.Workspace == nil && res.Untrusted != nil {
		return nil, fmt.Errorf("workspace %s is %s", res.Untrusted.Name, res.Reason)
	}
	return res.Workspace, nil
}

func newRunCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "run <workspace>|. -- <command> [args...]",
		Short: "Run a command with a workspace's identity without cd-ing into it",
		Long: "Run a command with a workspace's identity. `.` is the current folder's workspace (neutral when none).\n" +
			"For claude, the launcher starts in bypass (tq's guard is the hook, in every permission mode).",
		Example: "  tq run acme -- claude\n  tq run . -- claude -p \"...\"",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			defer trust.MemoScope()()
			// Flags are not interspersed: after <workspace> only an optional
			// -- may come before the command word, and everything after it
			// is the command's own.
			rest := args[1:]
			if len(rest) > 0 && rest[0] == "--" {
				rest = rest[1:]
			}
			args = append(args[:1:1], rest...)
			if len(args) < 2 {
				return errors.New("usage: tq run <workspace>|. -- <command> [args...]")
			}
			var ws *resolve.Workspace
			if args[0] == "." {
				w, err := cwdWorkspace()
				if err != nil {
					return err
				}
				ws = w
			} else {
				w, err := findWorkspace(args[0])
				if err != nil {
					return err
				}
				if v := trust.Check(w.Name, w.Hash); !v.OK {
					return fmt.Errorf("workspace %s is %s", w.Name, v.Reason)
				}
				ws = w
			}
			return launch{ws: ws, name: args[1], args: args[2:]}.run()
		},
	}
	c.Flags().SetInterspersed(false)
	return c
}
