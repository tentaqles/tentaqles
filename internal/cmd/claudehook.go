package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/cli/internal/doctor"
	"github.com/tentaqles/tentaqles/cli/internal/envplan"
	"github.com/tentaqles/tentaqles/cli/internal/gitcfg"
	"github.com/tentaqles/tentaqles/cli/internal/guard"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
)

// hookPayload is the subset of the Claude Code hook JSON tq reads. It never
// mutates anything: the hook only decides allow (exit 0) or block (exit 2).
type hookPayload struct {
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

// lookupGHUser resolves the gh login active for the workspace env. It is a
// package var so tests can stub it and never shell out to the real gh.
var lookupGHUser = func(env map[string]string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "gh", "api", "user", "--jq", ".login")
	c.Env = ghEnv(os.Environ(), env)
	out, err := c.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ghEnv builds the child env for `gh`. When the workspace pins a GH_CONFIG_DIR
// we must also strip the ambient token/host vars: gh prefers GH_TOKEN /
// GITHUB_TOKEN over the config dir, so an inherited token would report the
// shell's login instead of the workspace's identity.
func ghEnv(base []string, ws map[string]string) []string {
	dir := ws["GH_CONFIG_DIR"]
	if dir == "" {
		return base
	}
	drop := map[string]bool{
		"GH_TOKEN": true, "GITHUB_TOKEN": true, "GH_HOST": true,
		"GH_ENTERPRISE_TOKEN": true, "GITHUB_ENTERPRISE_TOKEN": true,
		"GH_CONFIG_DIR": true,
	}
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		k, _, ok := strings.Cut(kv, "=")
		if ok && drop[k] {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GH_CONFIG_DIR="+dir)
}

func newClaudeHookCmd() *cobra.Command {
	c := &cobra.Command{Use: "claude-hook", Short: "Claude Code hook adapter (reads the hook JSON on stdin)"}
	c.AddCommand(newPreToolUseCmd(), newSessionStartCmd())
	return c
}

// newSessionStartCmd is a no-op stub so the hook can already be wired into
// settings.json.
// TODO(task4): emit the session-start identity context.
func newSessionStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "session-start",
		Short: "SessionStart hook (stub)",
		RunE:  func(*cobra.Command, []string) error { return nil },
	}
}

func newPreToolUseCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "pre-tool-use",
		Short: "Allow (exit 0) or block (exit 2) the Bash command in the PreToolUse payload",
		RunE: func(c *cobra.Command, _ []string) error {
			p, cmdline := readHookPayload(c.InOrStdin())
			if cmdline == "" {
				return nil
			}
			in, err := gatherGuardInput(p.Cwd, cmdline)
			if err != nil {
				// Registry/resolve failure: treat cwd as neutral, which fails
				// closed only for remote mutations.
				in = guard.Input{Command: cmdline, Neutral: true, NeutralReason: "tq error: " + err.Error()}
			}
			d := guard.Decide(in)
			if asJSON {
				if err := json.NewEncoder(c.OutOrStdout()).Encode(struct {
					Block  bool   `json:"block"`
					Rule   string `json:"rule"`
					Reason string `json:"reason"`
				}{d.Block, d.Rule, d.Reason}); err != nil {
					return err
				}
			}
			if d.Block {
				fmt.Fprintln(c.ErrOrStderr(), d.Reason)
				exitFunc(2)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the decision as JSON")
	return c
}

// readHookPayload decodes the hook JSON and extracts the Bash command. Any
// protocol problem (malformed JSON, non-Bash tool, no command) yields "",
// which the caller treats as allow: tq never blocks on its own parse errors.
func readHookPayload(r io.Reader) (hookPayload, string) {
	var p hookPayload
	// Bound stdin: the hook payload is small, and a runaway producer must not
	// make tq allocate without limit.
	raw, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return p, ""
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, ""
	}
	if p.ToolName != "Bash" {
		return p, ""
	}
	return p, toolInputCommand(p.ToolInput)
}

// toolInputCommand reads {"command":...} out of tool_input, which arrives as an
// object or as a JSON-encoded string containing that object.
func toolInputCommand(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return strings.TrimSpace(obj.Command)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return ""
	}
	return strings.TrimSpace(obj.Command)
}

// gatherGuardInput does all the I/O the guard needs: registry + cwd resolution,
// doctor findings, manifest facts and (only when the command is gh) the actual
// gh login.
func gatherGuardInput(cwd, cmdline string) (guard.Input, error) {
	if strings.TrimSpace(cwd) == "" {
		// If Getwd also fails, cwd stays empty: resolve finds no workspace, the
		// input is neutral, and the guard fails closed for remote mutations.
		cwd, _ = os.Getwd()
	}
	cfg, err := registry.Load()
	if err != nil {
		return guard.Input{}, err
	}
	rep := doctor.RunForCwd(cfg, doctor.Deps{
		Env:      os.LookupEnv,
		Cwd:      cwd,
		RunGit:   gitcfg.RunGit,
		RunGitIn: gitcfg.RunGitIn,
		LookPath: exec.LookPath,
	})
	in := guard.Input{Command: cmdline, Findings: rep.Codes(), ActualEmail: rep.ActualEmail}

	// An untrusted cwd still names a workspace: pass it through as non-neutral
	// so the guard's "untrusted" rule (finding + Neutral=false) can fire for
	// git, while non-git commands stay allowed.
	ws := rep.Result.Workspace
	if ws == nil {
		ws = rep.Result.Untrusted
	}
	if ws == nil {
		in.Neutral = true
		in.NeutralReason = rep.Result.Reason
		return in, nil
	}

	m := ws.Manifest
	in.Client = m.Client
	in.ExpectedEmail = strings.TrimSpace(m.Git.Email)
	in.CloudProvider = strings.ToLower(strings.TrimSpace(cloudString(m.Cloud, "provider")))
	in.Blocked = effectiveBlocked(ws)

	expectedGH := strings.TrimSpace(m.Git.User)
	if expectedGH == "" {
		expectedGH = strings.TrimSpace(m.Git.ExpectedUser)
	}
	in.ExpectedGHUser = expectedGH
	// Only look up the real gh login for a TRUSTED workspace: on the untrusted
	// path there is no env plan to point gh at the workspace config dir, so the
	// lookup would compare the ambient login against an untrusted manifest.
	// Untrusted workspaces are judged by the untrusted/neutral rules alone.
	if expectedGH != "" && rep.Result.Workspace != nil && guard.StartsWith(cmdline, "gh") {
		in.ActualGHUser = lookupGHUser(envplan.Desired(rep.Result.Workspace))
	}
	return in, nil
}

// effectiveBlocked is the union of the manifest's top-level, git and cloud
// blocked-command lists, in that order.
func effectiveBlocked(ws *resolve.Workspace) []string {
	m := ws.Manifest
	out := append([]string{}, m.BlockedCommands...)
	out = append(out, m.Git.BlockedCommands...)
	return append(out, cloudStrings(m.Cloud, "blocked_commands")...)
}

// cloudString reads a string value out of the loosely typed cloud block.
func cloudString(cloud map[string]any, key string) string {
	s, _ := cloud[key].(string)
	return s
}

// cloudStrings reads a list-of-strings value out of the loosely typed cloud
// block, ignoring anything that isn't a string.
func cloudStrings(cloud map[string]any, key string) []string {
	list, ok := cloud[key].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
