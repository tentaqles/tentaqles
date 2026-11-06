package cmd

import (
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/humanonly"
)

// The human-only check lives in internal/humanonly (PRD v2 §8.1): a command
// that writes under the config area refuses when stdin or stdout is not a
// terminal, a harness is in the process tree or the environment carries a
// harness marker, and otherwise confirms before the first write. These are
// the thin wrappers the commands and their tests use.

// errHumanOnly is wrapped by every refusal.
var errHumanOnly = humanonly.ErrHumanOnly

// agentMarkers are the environment variables that mark a harness session.
var agentMarkers = humanonly.Markers

// agentSession reports whether a harness marker is set and names it. It is
// the one detector the launcher consults (a nested claude gets no bypass).
func agentSession() (string, bool) { return humanonly.EnvMarker() }

// stdinIsTerminal reports whether r is an interactive terminal (a seam for tests).
var stdinIsTerminal = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && humanonly.IsTerminal(f)
}

// refuseAgent refuses a human-only command unless a human at a terminal runs
// it: it runs the three detectors, logs a refusal to the denial log and never
// prompts. what names the command in the error, which wraps errHumanOnly.
func refuseAgent(what string) error { return humanonly.Check(what) }

// humanGate is refuseAgent followed by the mandatory confirmation: it prints
// about (one line saying what is about to be written) and asks the human to
// type yes. Anything else aborts with an error wrapping humanonly.ErrAborted
// (exit 1) and nothing written. Call it before the first write.
func humanGate(c *cobra.Command, what, about string) error {
	return humanonly.Gate(what, about, c.OutOrStdout())
}
