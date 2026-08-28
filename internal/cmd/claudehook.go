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

// newSessionStartCmd prints the identity preamble Claude Code shows at the
// start of a session. It never fails: any internal error is reported on
// stdout and the command still exits 0, because a SessionStart hook must
// never block the session from starting.
func newSessionStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "session-start",
		Short: "SessionStart hook: print the workspace identity preamble",
		RunE: func(c *cobra.Command, _ []string) error {
			p, _ := readHookPayload(c.InOrStdin())
			cwd := strings.TrimSpace(p.Cwd)
			if cwd == "" {
				cwd, _ = os.Getwd()
			}
			cfg, err := registry.Load()
			if err != nil {
				fmt.Fprintf(c.OutOrStdout(), "Tentaqles: tq could not resolve this workspace (%s)\n", err)
				return nil
			}
			rep := doctor.RunForCwd(cfg, doctor.Deps{
				Env:      os.LookupEnv,
				Cwd:      cwd,
				RunGit:   gitcfg.RunGit,
				RunGitIn: gitcfg.RunGitIn,
				LookPath: exec.LookPath,
			})
			expectedDir := ""
			if rep.Result.Workspace != nil {
				expectedDir = envplan.Desired(rep.Result.Workspace)["CLAUDE_CONFIG_DIR"]
			}
			fmt.Fprint(c.OutOrStdout(), renderSessionStart(rep, expectedDir))
			return nil
		},
	}
}

// renderSessionStart is the pure formatter behind session-start: given the
// cwd doctor report and the expected CLAUDE_CONFIG_DIR for a resolved
// workspace, it renders the exact preamble text (see task-4-brief.md).
func renderSessionStart(rep doctor.CwdReport, expectedDir string) string {
	res := rep.Result
	ws := res.Workspace
	if ws == nil {
		ws = res.Untrusted
	}
	if ws == nil {
		var b strings.Builder
		fmt.Fprintf(&b, "Client: none (neutral cwd: %s)\n\n", res.Reason)
		b.WriteString("Rules: remote git, gh and cloud CLI commands are blocked here until you cd into a trusted workspace (tq allow <name>).\n")
		return b.String()
	}

	m := ws.Manifest
	display := m.DisplayName
	if display == "" {
		display = m.Client
	}
	lang := m.Language
	if lang == "" {
		lang = "en"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Client: %s (%s)\n", display, lang)

	gitHost := m.Git.Host
	if gitHost == "" {
		gitHost = m.Git.Provider
	}
	if gitHost != "" || m.Git.User != "" || m.Git.Email != "" {
		fmt.Fprintf(&b, "Git: %s as %s (%s)\n", gitHost, m.Git.User, m.Git.Email)
	}

	cloudProvider := cloudString(m.Cloud, "provider")
	cloudSub := cloudString(m.Cloud, "subscription_name")
	if cloudProvider != "" || cloudSub != "" {
		fmt.Fprintf(&b, "Cloud: %s (%s subscription)\n", cloudProvider, cloudSub)
	}

	permMode := m.Claude.PermissionMode
	if permMode == "" {
		permMode = "default"
	}
	identity := "Identity: " + ws.Name
	if expectedDir != "" {
		identity += " · CLAUDE_CONFIG_DIR=" + expectedDir
	}
	identity += " · permission_mode=" + permMode
	b.WriteString(identity + "\n")

	b.WriteString("\ntq doctor:\n")
	if len(rep.Findings) == 0 {
		b.WriteString("- [ok] all checks passed\n")
	} else {
		for _, f := range rep.Findings {
			line := fmt.Sprintf("- [%s] %s: %s", f.Level, f.Code, f.Msg)
			if f.Fix != "" {
				line += " (→ " + f.Fix + ")"
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\nRules: tq blocks git/gh/cloud commands on identity drift (exit 2). Run `tq doctor` for details.\n")
	return b.String()
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
