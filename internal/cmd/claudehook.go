package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// The Claude Code hook: a small identity-drift check. Before a shell command runs, it compares
// the command with the client the folder belongs to (git email, gh user, cloud CLI, the
// manifest's blocked commands) and denies a command that would run under the wrong identity,
// with the reason. Any other tool call is allowed. The check is a speed bump for shell text,
// not a sandbox.

// hookPayload is the subset of the Claude Code hook JSON tq reads.
type hookPayload struct {
	Cwd       string          `json:"cwd"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

// lookupGHUser resolves the gh login active for the client's environment. Tests replace it.
var lookupGHUser = func(env map[string]string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "gh", "api", "user", "--jq", ".login")
	c.Env = ghEnv(os.Environ(), env)
	out, err := c.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ghEnv builds the child environment for gh. When the client pins a GH_CONFIG_DIR, the ambient
// token and host variables are dropped too: gh prefers GH_TOKEN over the config dir, so an
// inherited token would report the shell's login instead of the client's.
func ghEnv(base []string, ws map[string]string) []string {
	dir := ws["GH_CONFIG_DIR"]
	if dir == "" {
		return base
	}
	drop := []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_CONFIG_DIR"}
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		k, _, ok := strings.Cut(kv, "=")
		if ok && containsFold(drop, k) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GH_CONFIG_DIR="+dir)
}

// containsFold matches names case-insensitively: Windows variable names are.
func containsFold(xs []string, s string) bool {
	for _, x := range xs {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

func newClaudeHookCmd() *cobra.Command {
	c := &cobra.Command{Use: "claude-hook", Short: "Claude Code hook adapter (reads the hook JSON on stdin)"}
	c.AddCommand(newPreToolUseCmd(), newSessionStartCmd())
	return c
}

// newSessionStartCmd prints the client's identity at the start of a session. It never fails: a
// SessionStart hook must never keep a session from starting.
func newSessionStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "session-start",
		Short: "SessionStart hook: print the client identity preamble",
		RunE: func(c *cobra.Command, _ []string) error {
			p, _, err := readHookPayload(c.InOrStdin())
			if errors.Is(err, errPayloadTooLarge) {
				fmt.Fprintf(c.OutOrStdout(), "Tentaqles: tq could not resolve this folder (%s)\n", err)
				return nil
			}
			cwd := strings.TrimSpace(p.Cwd)
			if cwd == "" {
				cwd, _ = os.Getwd()
			}
			cfg, err := registry.Load()
			if err != nil {
				fmt.Fprintf(c.OutOrStdout(), "Tentaqles: tq could not resolve this folder (%s)\n", err)
				return nil
			}
			rep := doctor.RunForCwd(cfg, doctorDeps(cwd))
			expectedDir := ""
			if rep.Result.Workspace != nil {
				expectedDir = envplan.Desired(rep.Result.Workspace)["CLAUDE_CONFIG_DIR"]
			}
			fmt.Fprint(c.OutOrStdout(), renderSessionStart(rep, expectedDir))
			return nil
		},
	}
}

func doctorDeps(cwd string) doctor.Deps {
	return doctor.Deps{Env: os.LookupEnv, Cwd: cwd, RunGit: gitcfg.RunGit, RunGitIn: gitcfg.RunGitIn, LookPath: exec.LookPath}
}

// renderSessionStart is the preamble: the client, its language, its git and cloud identity, the Claude config
// home, and what the hook checks.
func renderSessionStart(rep doctor.CwdReport, expectedDir string) string {
	ws := rep.Result.Workspace
	var b strings.Builder
	if ws == nil {
		fmt.Fprintf(&b, "Client: none (neutral folder: %s)\n\n", rep.Result.Reason)
		b.WriteString("Rules: remote git, gh and cloud CLI commands are blocked here; cd into a client folder first.\n")
		return b.String()
	}
	m := ws.Manifest
	fmt.Fprintf(&b, "Client: %s\n", m.Client)
	l := m.ClientLanguage()
	fmt.Fprintf(&b, "Language: %s (%s). Write every report, document, email and message for this client's people (clients and teammates) in %s.\n", l.Name, l.Tag, l.Name)
	if m.Git.Provider != "" || m.Git.User != "" || m.Git.Email != "" {
		line := "Git: " + m.Git.Provider
		if m.Git.User != "" {
			line += " as " + m.Git.User
		}
		if m.Git.Email != "" {
			line += " (" + m.Git.Email + ")"
		}
		b.WriteString(strings.TrimSpace(line) + "\n")
	}
	if p := cloudString(m.Cloud, "provider"); p != "" {
		clouds := append([]string{p}, cloudStrings(m.Cloud, "also")...)
		fmt.Fprintf(&b, "Cloud: %s\n", strings.Join(clouds, ", "))
	}
	identity := "Identity: " + ws.Name
	if expectedDir != "" {
		identity += " · CLAUDE_CONFIG_DIR=" + expectedDir
	}
	b.WriteString(identity + "\n")
	fmt.Fprintf(&b, "Tools: %s. Other providers' CLIs are not on this terminal's PATH.\n", strings.Join(m.Scope(), ", "))
	b.WriteString("\ntentaqles doctor:\n")
	if len(rep.Findings) == 0 {
		b.WriteString("- [ok] all checks passed\n")
	}
	for _, f := range rep.Findings {
		line := fmt.Sprintf("- [%s] %s: %s", f.Level, f.Code, f.Msg)
		if f.Fix != "" {
			line += " (→ " + f.Fix + ")"
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\nRules: tq blocks git, gh and cloud commands that would run under another identity (exit 2). Run `tentaqles doctor` for details.\n")
	return b.String()
}

func newPreToolUseCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "pre-tool-use",
		Short: "Allow (exit 0) or deny (exit 2) the shell command in the PreToolUse payload",
		Long: `Reads a Claude Code PreToolUse payload on stdin. A Bash or PowerShell command is checked
against the client the folder belongs to: the manifest's blocked commands, the git email, the
gh user and the cloud CLI. A command that would run under another identity, or a remote git, gh
or cloud command outside every client folder, is denied: exit 2 with
BLOCKED [identity/<rule>]: <reason> on stderr. Every other tool call is allowed.

A payload tq cannot read, a client manifest it cannot read, and a check that does not finish
within the hook deadline are denied as BLOCKED [tq/guard-error].`,
		RunE: func(c *cobra.Command, _ []string) error {
			if code := runPreToolUse(c.InOrStdin(), c.OutOrStdout(), c.ErrOrStderr(), asJSON); code != 0 {
				exitFunc(code)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the decision as JSON")
	return c
}

// hookDeadline bounds one decision, well under the plugin's 15 s hook timeout (a harness runs a
// call whose hook it killed). Tests lower it.
var hookDeadline = 6 * time.Second

// hookDecideTestHook runs at the start of every decision when a test sets it (a decision that
// spins, to prove the deadline and the watchdog end the process). Nil in tq.
var hookDecideTestHook func()

// hookGuardDecide is guard.Decide; tests replace it to make the check panic or stall.
var hookGuardDecide = guard.Decide

// guardErrorID names the deny of a call the check cannot judge.
const guardErrorID = "tq/guard-error"

// runPreToolUse decides one call and returns the exit code. The decision runs in its own
// goroutine; a panic or a missed deadline is a deny.
func runPreToolUse(in io.Reader, out, errw io.Writer, asJSON bool) int {
	var bo, be bytes.Buffer
	done := make(chan int, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				bo.Reset()
				be.Reset()
				done <- failClosed(&bo, &be, asJSON, fmt.Sprintf("tq guard error (%v)", r))
			}
		}()
		done <- decide(in, &bo, &be, asJSON)
	}()
	timer := time.NewTimer(hookDeadline)
	defer timer.Stop()
	select {
	case code := <-done:
		_, _ = io.Copy(out, &bo)
		_, _ = io.Copy(errw, &be)
		return code
	case <-timer.C:
		return failClosed(out, errw, asJSON, fmt.Sprintf("the check did not finish within %s", hookDeadline))
	}
}

func decide(in io.Reader, out, errw io.Writer, asJSON bool) int {
	if hookDecideTestHook != nil {
		hookDecideTestHook()
	}
	p, cmdline, err := readHookPayload(in)
	if err != nil {
		return failClosed(out, errw, asJSON, "the hook payload cannot be read ("+err.Error()+")")
	}
	if cmdline != "" {
		cwd := strings.TrimSpace(p.Cwd)
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		gin, res, err := gatherGuardInput(cwd, cmdline)
		if err != nil {
			return failClosed(out, errw, asJSON, "tq cannot read its registry ("+err.Error()+")")
		}
		if res.Unreadable {
			return failClosed(out, errw, asJSON, "the client manifest of this folder cannot be read ("+res.Reason+")")
		}
		if d := hookGuardDecide(gin); d.Block {
			emitDecision(out, errw, asJSON, true, d.Rule, d.Reason)
			return 2
		}
	}
	if asJSON {
		emitDecision(out, errw, true, false, "", "")
	}
	return 0
}

func failClosed(out, errw io.Writer, asJSON bool, why string) int {
	reason := "BLOCKED [" + guardErrorID + "]: " + why + "; tq denies a call it cannot judge. Report this to the developer instead of retrying; `tentaqles doctor` shows what is wrong"
	emitDecision(out, errw, asJSON, true, guardErrorID, reason)
	return 2
}

// emitDecision reports a decision: the --json object on stdout when asked for, and for a block
// the reason on stderr (Claude Code shows the stderr of an exit-2 hook to the model).
func emitDecision(out, errw io.Writer, asJSON, block bool, rule, reason string) {
	if asJSON {
		_ = json.NewEncoder(out).Encode(struct {
			Block  bool   `json:"block"`
			Rule   string `json:"rule"`
			Reason string `json:"reason"`
		}{block, rule, reason})
	}
	if block {
		fmt.Fprintln(errw, reason)
	}
}

// maxHookPayload bounds stdin: the payload is small, and a runaway producer must not make tq
// allocate without limit.
const maxHookPayload = 1 << 20

var errPayloadTooLarge = errors.New("hook payload exceeds 1 MiB")

// isShellTool reports the tools whose input is a shell command line.
func isShellTool(name string) bool { return name == "Bash" || name == "PowerShell" }

// readHookPayload decodes the hook JSON and extracts the shell command ("" for any other tool).
func readHookPayload(r io.Reader) (hookPayload, string, error) {
	var p hookPayload
	raw, err := io.ReadAll(io.LimitReader(r, maxHookPayload+1))
	if err != nil {
		return p, "", fmt.Errorf("reading stdin: %w", err)
	}
	if len(raw) > maxHookPayload {
		return p, "", errPayloadTooLarge
	}
	if !utf8.Valid(raw) {
		return p, "", errors.New("the payload is not valid UTF-8")
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, "", fmt.Errorf("malformed JSON: %w", err)
	}
	if !isShellTool(p.ToolName) {
		return p, "", nil
	}
	return p, toolInputCommand(p.ToolInput), nil
}

// toolInputCommand reads {"command":...} out of tool_input, which arrives as an object or as a
// JSON-encoded string holding that object.
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

// gatherGuardInput does the I/O the check needs: the folder's client, the cwd findings, the
// manifest facts and, only for a gh command, the actual gh login.
func gatherGuardInput(cwd, cmdline string) (guard.Input, resolve.Result, error) {
	cfg, err := registry.Load()
	if err != nil {
		return guard.Input{}, resolve.Result{}, err
	}
	rep := doctor.RunForCwd(cfg, doctorDeps(cwd))
	in := guard.Input{Command: cmdline, Findings: rep.Codes(), ActualEmail: rep.ActualEmail}
	ws := rep.Result.Workspace
	if ws == nil {
		in.Neutral, in.NeutralReason = true, rep.Result.Reason
		return in, rep.Result, nil
	}
	m := ws.Manifest
	in.Client = m.Client
	in.ExpectedEmail = strings.TrimSpace(m.Git.Email)
	in.CloudProvider = manifest.CloudProviderName(cloudString(m.Cloud, "provider"))
	for _, x := range cloudStrings(m.Cloud, "also") {
		in.ExtraClouds = append(in.ExtraClouds, manifest.CloudProviderName(x))
	}
	in.Blocked = effectiveBlocked(ws)
	// expected_user names the GitHub user only for a GitHub client: on another host it is that
	// host's account (an Azure DevOps email, say), which gh would never report.
	expectedGH := strings.TrimSpace(m.Git.User)
	if gp := strings.ToLower(strings.TrimSpace(m.Git.Provider)); expectedGH == "" && (gp == "" || gp == "github") {
		expectedGH = strings.TrimSpace(m.Git.ExpectedUser)
	}
	in.ExpectedGHUser = expectedGH
	if expectedGH != "" && guard.StartsWith(cmdline, "gh") {
		in.ActualGHUser = lookupGHUser(envplan.Desired(ws))
	}
	return in, rep.Result, nil
}

// effectiveBlocked is the union of the manifest's top-level, git and cloud blocked-command
// lists, in that order.
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

// cloudStrings reads a list of strings out of the loosely typed cloud block.
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
