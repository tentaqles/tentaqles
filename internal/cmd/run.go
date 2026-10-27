package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// codexArgs adds tq's unattended profile when cwd is a covered project of a
// projects-only setup and bypass is trusted; workspace-wide setups use the
// top-level keys instead, and an explicit --profile/-p is left alone.
func codexArgs(ws *resolve.Workspace, cwd string, args []string) []string {
	u := ws.Manifest.Guard.Unattended
	if !u.Configured() || u.Workspace || !trust.IsBypassAllowed(ws.Hash) {
		return args
	}
	if !slices.Contains(ws.Manifest.IdentityNames(), "codex") {
		return args
	}
	// Only point Codex at a profile file tq wrote; never at a missing or
	// user-authored one (a conflict leaves that file alone).
	if !codexcfg.OwnsProfile(paths.IdentityDir(ws.Name, "codex")) {
		return args
	}
	if ok, _ := guardcfg.Covers(ws.Manifest, ws.Root, cwd); !ok {
		return args
	}
	for _, a := range args {
		if a == "--profile" || a == "-p" || strings.HasPrefix(a, "--profile=") {
			return args
		}
	}
	return append([]string{"--profile", codexcfg.Profile}, args...)
}

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
		// "acceptEdits", "plan", "auto" and "dontAsk" can reach this arm.
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
	if applyClaudeArgs && name == "codex" {
		cwd, _ := os.Getwd()
		args = codexArgs(ws, cwd, args)
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
