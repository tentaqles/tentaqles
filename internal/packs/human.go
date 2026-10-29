package packs

import (
	"errors"
	"os"
	"strings"
)

// ErrHumanOnly refuses a human-only pack command (promote apply, a cloud
// approve or apply) in an agent session.
var ErrHumanOnly = errors.New("this command is human-only: an agent session cannot run it")

// AgentSession reports the hub's agent-session markers: TENTAQLES_HEADLESS=1,
// CLAUDECODE=1, CLAUDE_PLUGIN_ROOT or TQ_HUB_TASK set (the same check as
// tq secret and tq hub answer).
func AgentSession() bool {
	return os.Getenv("TENTAQLES_HEADLESS") == "1" || os.Getenv("CLAUDE_PLUGIN_ROOT") != "" ||
		os.Getenv("CLAUDECODE") == "1" || strings.TrimSpace(os.Getenv("TQ_HUB_TASK")) != ""
}

// HumanOnly returns ErrHumanOnly in an agent session. Every human-only pack
// subcommand calls it before anything else.
func HumanOnly() error {
	if AgentSession() {
		return ErrHumanOnly
	}
	return nil
}
