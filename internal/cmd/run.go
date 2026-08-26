package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/cli/internal/envplan"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
	"github.com/tentaqles/tentaqles/cli/internal/trust"
)

func claudeArgs(ws *resolve.Workspace, args []string, warn func(string)) []string {
	mode := ws.Manifest.Claude.PermissionMode
	switch mode {
	case "", "default":
		return args
	case "bypass":
		if trust.IsBypassAllowed(ws.Hash) {
			return append([]string{"--dangerously-skip-permissions"}, args...)
		}
		warn(fmt.Sprintf("workspace %s asks for bypass but `tq allow --bypass %s` was not run; using acceptEdits", ws.Name, ws.Name))
		return append([]string{"--permission-mode", "acceptEdits"}, args...)
	default:
		// Invariant: manifest.Load allowlists permission_mode, so only
		// "acceptEdits" and "plan" can reach this arm.
		return append([]string{"--permission-mode", mode}, args...)
	}
}

// execIn runs name+args with ws's environment. applyClaudeArgs must be false for
// flows that drive claude's own subcommands (e.g. `tq login <ws> claude`), where
// injecting --permission-mode / --dangerously-skip-permissions is wrong.
func execIn(ws *resolve.Workspace, name string, args []string, applyClaudeArgs bool, errw func(string)) error {
	if applyClaudeArgs && name == "claude" {
		args = claudeArgs(ws, args, errw)
	}
	cmd := exec.Command(name, args...)
	cmd.Env = envplan.Environ(ws, os.Environ())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.ExitCode())
	}
	return err
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <workspace> -- <command> [args...]",
		Short: "Run a command with a workspace's identity without cd-ing into it",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			ws, err := findWorkspace(args[0])
			if err != nil {
				return err
			}
			if !trust.IsTrusted(ws.Hash) {
				return fmt.Errorf("workspace %s is untrusted; run: tq allow %s", ws.Name, ws.Name)
			}
			return execIn(ws, args[1], args[2:], true, func(s string) { fmt.Fprintln(c.ErrOrStderr(), "warning:", s) })
		},
	}
}
