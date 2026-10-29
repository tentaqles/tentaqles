package packs

import (
	"errors"
	"os"
	"strings"
)

// ErrHumanOnly refuses a human-only pack command (any apply subcommand that
// executes an approved action, such as promote apply or cloud apply) in an
// agent session. An approve or promote request command only asks (it creates
// the pending decision) and never answers; agents may run it.
var ErrHumanOnly = errors.New("this command is human-only: an agent session cannot run it")

// AgentSession reports the hub's agent-session markers: TENTAQLES_HEADLESS=1,
// CLAUDECODE=1, CLAUDE_PLUGIN_ROOT or TQ_HUB_TASK set (the same check as
// tq secret and tq hub answer).
func AgentSession() bool {
	return os.Getenv("TENTAQLES_HEADLESS") == "1" || os.Getenv("CLAUDE_PLUGIN_ROOT") != "" ||
		os.Getenv("CLAUDECODE") == "1" || strings.TrimSpace(os.Getenv("TQ_HUB_TASK")) != ""
}

// AnnotationHumanOnly is the cobra annotation a pack command sets to
// declare what it does with an approval:
//   - HumanOnlyExecute: it executes an approved action (it calls
//     approval.Confirm or approval.Claim). Its name must contain apply, so
//     tq/human-answer denies it to agents; a framework test walks the
//     command tree and asserts the rule matches every such command.
//   - ApprovalAsk: an approve or request command; it only calls
//     approval.Ask (it creates the pending decision) and never answers.
const (
	AnnotationHumanOnly = "tq.human-only"
	HumanOnlyExecute    = "execute"
	ApprovalAsk         = "ask"
)

// HumanOnly returns ErrHumanOnly in an agent session. Every human-only pack
// subcommand calls it before anything else.
func HumanOnly() error {
	if AgentSession() {
		return ErrHumanOnly
	}
	return nil
}
