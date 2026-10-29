package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
)

// agentSession reports whether this process runs under an agent and names
// the first marker found: hubAgent's (Claude Code, the plugin, a headless or
// hub run) plus Codex's sandbox vars (spec E Assumption A4).
func agentSession() (string, bool) {
	if hubAgent() {
		for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK"} {
			if strings.TrimSpace(os.Getenv(k)) != "" {
				return k, true
			}
		}
		return "agent", true
	}
	for _, k := range []string{"CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"} {
		if strings.TrimSpace(os.Getenv(k)) != "" {
			return k, true
		}
	}
	return "", false
}

// stdoutIsTerminal reports whether w is an interactive terminal (a seam for tests).
var stdoutIsTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// refuseAgent refuses an agent session, for verbs that must never run under
// an agent but may run without a terminal (tq run --off, and tq env --off,
// whose output the shell function captures). what names the verb in the
// error, which wraps runner.ErrHumanOnly.
func refuseAgent(what string) error {
	if m, ok := agentSession(); ok {
		return fmt.Errorf("%s: %w (agent session: %s is set)", what, runner.ErrHumanOnly, m)
	}
	return nil
}

// requireHuman is refuseAgent plus an interactive stdin and stdout, for the
// human-only verbs (tq mode set|toggle|override|edit, tq allow --mode).
func requireHuman(in io.Reader, out io.Writer, what string) error {
	if err := refuseAgent(what); err != nil {
		return err
	}
	if !stdinIsTerminal(in) || !stdoutIsTerminal(out) {
		return fmt.Errorf("%s: %w (needs an interactive terminal)", what, runner.ErrHumanOnly)
	}
	return nil
}
