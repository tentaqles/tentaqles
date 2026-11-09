package cmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// The OpenAI Codex CLI hook: the same check as the Claude Code hook (claudehook.go), with the
// Codex input and output contract (hookDialect.codex). Codex reads a JSON payload on stdin like
// Claude Code does, and it fails open: a deny only counts as one JSON object on stdout, so every
// error path of pre-tool-use answers that object with exit 0.

func newCodexHookCmd() *cobra.Command {
	c := &cobra.Command{Use: "codex-hook", Short: "OpenAI Codex CLI hook adapter (reads the hook JSON on stdin)"}
	c.AddCommand(newCodexPreToolUseCmd(), newCodexSessionStartCmd())
	return c
}

func newCodexPreToolUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pre-tool-use",
		Short: "Allow or deny the shell command in the PreToolUse payload (Codex CLI)",
		Long: `Reads a Codex CLI PreToolUse payload on stdin. A Bash command is checked against the client the
folder belongs to: the manifest's blocked commands, the git email, the gh user and the cloud CLI.
A command that would run under another identity, or a remote git, gh or cloud command outside
every client folder, is denied: one JSON object on stdout with permissionDecision "deny" and the
BLOCKED [identity/<rule>]: <reason> text, the same reason on stderr, exit 0. Every other tool
call is allowed: nothing on stdout, exit 0.

A payload tq cannot read, a client manifest it cannot read, and a check that does not finish
within the hook deadline are denied as BLOCKED [tq/guard-error]. Codex runs a call whose hook
gives no deny, so every error here is answered with the deny object.`,
		RunE: func(c *cobra.Command, _ []string) error {
			if code := runPreToolUse(c.InOrStdin(), c.OutOrStdout(), c.ErrOrStderr(), codexDialect); code != 0 {
				exitFunc(code)
			}
			return nil
		},
	}
}

// newCodexSessionStartCmd prints the same preamble as the Claude Code session-start hook. It never
// fails: a panic prints nothing, and the session starts either way.
func newCodexSessionStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "session-start",
		Short: "SessionStart hook: print the client identity preamble (Codex CLI)",
		Long: `Reads a Codex CLI SessionStart payload on stdin and prints the client's identity preamble, the
same text as tentaqles claude-hook session-start. Codex adds the printed text to the model's
developer context. It never fails: a session always starts.`,
		RunE: func(c *cobra.Command, _ []string) error {
			defer func() { _ = recover() }()
			sessionStart(c.InOrStdin(), c.OutOrStdout())
			return nil
		},
	}
}

// codexDeny is the object Codex reads as a PreToolUse decision.
type codexDeny struct {
	HookSpecificOutput codexDenyOutput `json:"hookSpecificOutput"`
}

type codexDenyOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

// emitCodexDeny answers a deny: the decision object on stdout (the only thing Codex reads), and the
// reason on stderr for a human. A deny without a reason would fail open, so an empty one gets the
// guard-error reason.
func emitCodexDeny(out, errw io.Writer, reason string) {
	if reason == "" {
		reason = "BLOCKED [" + guardErrorID + "]: tq denies a call it cannot judge"
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(codexDeny{HookSpecificOutput: codexDenyOutput{
		HookEventName:            "PreToolUse",
		PermissionDecision:       "deny",
		PermissionDecisionReason: reason,
	}})
	fmt.Fprintln(errw, reason)
}
