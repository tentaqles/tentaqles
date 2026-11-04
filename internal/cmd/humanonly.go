package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// errHumanOnly is the error of the verbs an agent session may never run (tq
// secret, tq allow, tq deny, tq run --off): the human runs them in their
// own terminal.
var errHumanOnly = errors.New("human-only: run it in your own terminal (an agent session is refused)")

// agentMarkers are the environment variables that mark a process as running
// under an agent: a headless run, the Claude Code plugin, Claude Code itself,
// a hub task of an older tq, and Codex's sandbox.
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

// refuseAgent refuses an agent session, for the human-only verbs (tq allow,
// tq deny, tq run --off). It never prompts and needs no terminal. what
// names the verb in the error, which wraps errHumanOnly.
func refuseAgent(what string) error {
	if m, ok := agentSession(); ok {
		return fmt.Errorf("%s: %w (agent session: %s is set)", what, errHumanOnly, m)
	}
	return nil
}
