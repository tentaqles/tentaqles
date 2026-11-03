package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// errHumanOnly is the error of the verbs an agent session may never run (tq
// secret, tq allow, tq deny, the mode verbs): the human runs them in their
// own terminal.
var errHumanOnly = errors.New("human-only: run it in your own terminal (an agent session is refused)")

// agentMarkers are the environment variables that mark a process as running
// under an agent: a headless run, the Claude Code plugin, Claude Code itself,
// a hub task of an older tq, and Codex's sandbox (spec E Assumption A4).
var agentMarkers = []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"}

// agentSession reports whether this process runs under an agent and names
// the first marker found.
func agentSession() (string, bool) {
	for _, k := range agentMarkers {
		if strings.TrimSpace(os.Getenv(k)) != "" {
			return k, true
		}
	}
	return "", false
}

// stdinIsTerminal reports whether r is an interactive terminal (a seam for tests).
var stdinIsTerminal = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
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
// error, which wraps errHumanOnly.
func refuseAgent(what string) error {
	if m, ok := agentSession(); ok {
		return fmt.Errorf("%s: %w (agent session: %s is set)", what, errHumanOnly, m)
	}
	return nil
}

// requireHuman is refuseAgent plus an interactive stdin and stdout, for the
// human-only verbs (the mode verbs and the mode grant of tq allow).
func requireHuman(in io.Reader, out io.Writer, what string) error {
	if err := refuseAgent(what); err != nil {
		return err
	}
	if !stdinIsTerminal(in) || !stdoutIsTerminal(out) {
		return fmt.Errorf("%s: %w (needs an interactive terminal)", what, errHumanOnly)
	}
	return nil
}
