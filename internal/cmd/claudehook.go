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
	"sync"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// hookPayload is the subset of the Claude Code hook JSON tq reads. It never
// mutates anything: the hook only decides allow (exit 0) or block (exit 2).
type hookPayload struct {
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	// AgentID and AgentType are set by Claude Code when the call comes from
	// a subagent; the denial log names the caller from them.
	AgentID   string `json:"agent_id"`
	AgentType string `json:"agent_type"`
}

// hookPolicyDecision is policyDecision; tests replace it to make the
// analysis panic or stall.
var hookPolicyDecision = policyDecision

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
		if ok && dropKey(drop, k) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GH_CONFIG_DIR="+dir)
}

// dropKey matches env var names case-insensitively: Windows env names are
// case-insensitive, so an inherited "Gh_Token" must be dropped too.
func dropKey(drop map[string]bool, k string) bool {
	for name := range drop {
		if strings.EqualFold(name, k) {
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

// newSessionStartCmd prints the identity preamble Claude Code shows at the
// start of a session. It never fails: any internal error is reported on
// stdout and the command still exits 0, because a SessionStart hook must
// never block the session from starting.
func newSessionStartCmd() *cobra.Command {
	agent := hookAgentClaude
	c := &cobra.Command{
		Use:   "session-start",
		Short: "SessionStart hook: print the workspace identity preamble",
		RunE: func(c *cobra.Command, _ []string) error {
			defer trust.MemoScope()()
			// session-start must never block a session: an oversized payload
			// is reported as "could not resolve" and still exits 0; one that
			// does not parse resolves the process's own cwd.
			p, _, perr := readHookPayload(c.InOrStdin())
			if errors.Is(perr, errPayloadTooLarge) {
				fmt.Fprintf(c.OutOrStdout(), "Tentaqles: tq could not resolve this workspace (%s)\n", perr)
				return nil
			}
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
	c.Flags().StringVar(&agent, "agent", hookAgentClaude, "the agent calling the hook: claude or codex")
	return c
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

	identity := "Identity: " + ws.Name
	if expectedDir != "" {
		identity += " · CLAUDE_CONFIG_DIR=" + expectedDir
	}
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
	b.WriteString("Secrets: never read or print a .env file or the environment (Read/cat/type/Get-Content/grep .env, printenv, echo $TOKEN): tq blocks it. " +
		"To use a value, run the command through `tq dotenv run [--file <file>] -- <cmd>` and reference it as $NAME " +
		"(`tq dotenv run -- bash -c '...$NAME...'` when it needs shell expansion); values are masked in the output. " +
		"In code, read variables by name (os.Getenv, process.env.NAME) and never log them.\n")
	return b.String()
}

func newPreToolUseCmd() *cobra.Command {
	var asJSON bool
	agent := hookAgentClaude
	c := &cobra.Command{
		Use:   "pre-tool-use",
		Short: "Allow (exit 0) or deny (exit 2) the tool call in the PreToolUse payload",
		Long: `Reads a Claude Code PreToolUse payload on stdin and decides, in order
(the first category that matches denies; no match allows):

  1. identity: the manifest's blocked commands, then Bash/PowerShell
     commands through the workspace identity guard (git email, gh user,
     cloud CLI, trust) and the cross-client MCP check;
  2. secrets: every tool call (shell, file read, MCP) goes through tq's
     built-in secret rules and a secret scan of what a git commit would
     record;
  3. self-protection: a read, write, copy, move or delete of a protected
     path (tq's state folder, the workspace marker, the installed plugin,
     the tq binary) in any tool, and a human-only tq command in the shell.

There are two verdicts, allow and deny; tq never asks. Deny exits 2 with
BLOCKED [<category>/<rule>]: <reason> on stderr and one line in
~/.tentaqles/denials.jsonl. tq fails closed: a payload it cannot read, a
guard crash, a decision that takes longer than the hook deadline, a
registry or a workspace manifest it cannot read, and a commit whose secret
scan cannot finish are denied (BLOCKED [tq/guard-error], or
[secrets/tq/commit-secret] for the scan).`,
		RunE: func(c *cobra.Command, _ []string) error {
			if code := runPreToolUse(c.InOrStdin(), c.OutOrStdout(), c.ErrOrStderr(), asJSON, agent); code != 0 {
				exitFunc(code)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the decision as JSON")
	c.Flags().StringVar(&agent, "agent", hookAgentClaude, "the agent calling the hook: claude or codex")
	return c
}

// hookDeadline bounds one whole pre-tool-use decision. A harness that kills
// a hook at its own timeout (15 s for Claude in the plugin's hooks.json, 30 s
// for Codex, codexcfg.HookTimeoutSeconds) lets the call run, so tq denies
// first (PRD v2 §6.3, fail closed): the decision at 6 s, the process
// watchdog (hookWatchdog) at 7 s, tq_hook.sh's TERM at 7 s and KILL at 8 s,
// at least 5 s under the shortest harness timeout (Git Bash forks are slow).
// TestHookDeadlinesUnderHarnessTimeouts pins the order. It leaves room
// above the scan budget (at most maxScanBudget). Tests lower it.
var hookDeadline = 6 * time.Second

// hookRun is one pre-tool-use invocation: the flags and what is known about
// the call so far, so a fail-closed answer (a panic, the deadline) can still
// name the tool, the cwd and the identity in the denial log.
type hookRun struct {
	asJSON bool
	agent  string

	mu       sync.Mutex
	tool     string
	cwd      string
	command  string // the file path or the tool name; for a shell call, see shell
	shell    string // the shell line, redacted only when it is logged
	caller   string
	identity string
}

// runPreToolUse decides one call and returns the exit code. The decision
// runs in its own goroutine and answers into buffers; the caller's writers
// get the buffers when it finishes, or a deny when it panics or misses
// hookDeadline. There is no other way out: every path is allow or deny.
func runPreToolUse(in io.Reader, out, errw io.Writer, asJSON bool, agent string) int {
	run := &hookRun{asJSON: asJSON, agent: agent, caller: audit.CallerHarness}
	var bo, be bytes.Buffer
	done := make(chan int, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				bo.Reset()
				be.Reset()
				detail := fmt.Sprint(r)
				if len(detail) > 200 {
					detail = detail[:200] + "…"
				}
				fmt.Fprintln(&be, "tq: guard panic: "+detail)
				done <- run.failClosed(&bo, &be, "tq guard error ("+detail+")")
			}
		}()
		done <- run.decide(in, &bo, &be)
	}()
	timer := time.NewTimer(hookDeadline)
	defer timer.Stop()
	select {
	case code := <-done:
		_, _ = io.Copy(out, &bo)
		_, _ = io.Copy(errw, &be)
		return code
	case <-timer.C:
		return run.failClosed(out, errw, fmt.Sprintf("the guard did not decide within %s", hookDeadline))
	}
}

// decide is the analysis: payload, identity guard, secret rules,
// self-protection. It writes its answer to out/errw and returns the code.
func (run *hookRun) decide(in io.Reader, out, errw io.Writer) int {
	// One verification of the integrity record per call, however often
	// resolve and doctor ask (trust.MemoScope).
	defer trust.MemoScope()()
	// One scan deadline from hook entry: the gh/git identity lookups below
	// count against it (hooks must never stall).
	scanDeadline := time.Now().Add(hookScanBudget())
	p, cmdline, perr := readHookPayload(in)
	if perr != nil {
		return run.failClosed(out, errw, "the hook payload cannot be read ("+perr.Error()+")")
	}
	cwd := strings.TrimSpace(p.Cwd)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	run.mu.Lock()
	run.tool, run.cwd = p.ToolName, cwd
	if p.AgentID != "" || p.AgentType != "" {
		run.caller = audit.CallerSubagent
	}
	run.mu.Unlock()
	if p.ToolName == "" {
		return run.failClosed(out, errw, "the hook payload names no tool")
	}
	if why := payloadFieldError(p); why != "" {
		return run.failClosed(out, errw, "the hook payload cannot be judged ("+why+")")
	}
	call := toolCallFrom(p, cwd)
	run.mu.Lock()
	run.command = logCommand(call)
	if call.IsShell() {
		run.shell = call.Command
	}
	run.mu.Unlock()

	var ws *resolve.Workspace
	var cfg *registry.Config
	if cmdline != "" {
		in, gws, gcfg, res, err := gatherGuardInput(cwd, cmdline)
		if err != nil {
			return run.failClosed(out, errw, "tq cannot read its registry ("+err.Error()+")")
		}
		run.setIdentity(res)
		if res.Unreadable {
			return run.failClosed(out, errw, unreadableReason(res))
		}
		ws, cfg = gws, gcfg
		if d := guard.Decide(in); d.Block {
			return run.deny(out, errw, guard.Category, d.Rule, d.Reason+scopeNote(ws), "", audit.LayerHook)
		}
	} else {
		res, gcfg, err := resolveCwd(cwd)
		if err != nil {
			return run.failClosed(out, errw, "tq cannot read its registry ("+err.Error()+")")
		}
		run.setIdentity(res)
		if res.Unreadable {
			return run.failClosed(out, errw, unreadableReason(res))
		}
		ws, cfg = res.Workspace, gcfg
	}

	// subject is the one path/token the denial names for the log.
	subject := ""
	d := hookPolicyDecision(cwd, ws, cfg, call, scanDeadline)
	// A Codex apply_patch names its files only in the patch text: the file
	// rules run on every file it writes, and a patch tq cannot read, or one
	// too big to judge in the budget, is denied. The rule set and the
	// protected path set are built once.
	if isApplyPatch(call) || (run.agent == hookAgentCodex && isFileWriteTool(call.Tool) && call.Path == "") {
		files, err := parseApplyPatch(call.Command)
		if err != nil {
			return run.failClosed(out, errw, err.Error())
		}
		if len(files) > maxPatchFiles {
			return run.failClosed(out, errw, fmt.Sprintf("the patch writes %d files, more than the %d tq judges in one call: split the patch", len(files), maxPatchFiles))
		}
		set := buildPolicy()
		prot := protectedSet(ws, cfg)
		for _, fc := range patchCalls(files, cwd) {
			if time.Now().After(scanDeadline) {
				return run.failClosed(out, errw, "the patch took longer than the hook budget to judge: split the patch")
			}
			fd := policyDecisionWith(set, prot, cwd, ws, cfg, fc, scanDeadline)
			if fd.Action == policy.Deny && d.Action != policy.Deny {
				subject = fc.Path // the first file the patch is denied on
			}
			d = policy.Merge(d, fd)
		}
	}
	if d.Action == policy.Deny {
		r := d.Matched[0]
		if subject == "" {
			subject = r.Subject
		}
		layer := audit.LayerHook
		if r.ID == policy.ProtectedPathID {
			layer = audit.LayerFilesystem
		}
		reason := d.Reason()
		if r.Category == policy.CategoryIdentity {
			reason += scopeNote(ws)
		}
		return run.deny(out, errw, denyKind(d), r.ID, reason, denialSubject(call, subject), layer)
	}
	if run.asJSON {
		emitDecision(out, errw, true, false, "", "", "")
	}
	return 0
}

// setIdentity records the identity the cwd maps to for the log: the trusted
// workspace, else the untrusted one, else none.
func (run *hookRun) setIdentity(res resolve.Result) {
	name := hookWSName(res.Workspace)
	if name == "" {
		name = hookWSName(res.Untrusted)
	}
	if name == "" {
		name = res.Name
	}
	run.mu.Lock()
	run.identity = name
	run.mu.Unlock()
}

// deny answers a deny of category/rule: the log line, the Codex deny JSON
// under --agent codex, the --json object or the reason on stderr, exit 2.
func (run *hookRun) deny(out, errw io.Writer, category, rule, reason, subject, layer string) int {
	run.mu.Lock()
	d := audit.Denial{
		Category: category, Layer: layer, Caller: run.caller, Harness: run.agent,
		Tool: run.tool, Command: run.loggedCommand(subject), Subject: subject,
		Rule: rule, Reason: reason, Identity: run.identity, PID: os.Getpid(), Cwd: run.cwd,
	}
	run.mu.Unlock()
	// A log that cannot be written changes nothing: the deny stands.
	_ = audit.AppendDenial(d)
	if run.agent == hookAgentCodex && !run.asJSON {
		writeCodexDeny(out, reason)
	}
	emitDecision(out, errw, run.asJSON, true, category, rule, reason)
	return 2
}

// loggedCommand is the command the log records: the shell line through
// audit.RedactCommand, keeping the words of the subject the deny matched;
// otherwise the path or the tool name. Called with run.mu held.
func (run *hookRun) loggedCommand(subject string) string {
	if run.shell != "" {
		return audit.RedactCommand(run.shell, subject)
	}
	return run.command
}

// guardErrorID names the fail-closed deny of a call the guard cannot judge
// (PRD v2 §6.3): an unreadable payload, a panic, the deadline, a registry
// or manifest tq cannot read, a patch it cannot parse.
const guardErrorID = "tq/guard-error"

// failClosed denies a call the guard cannot judge.
func (run *hookRun) failClosed(out, errw io.Writer, why string) int {
	reason := "BLOCKED [" + guardErrorID + "]: " + why + "; tq denies a call it cannot judge. Report this to the developer instead of retrying; `tq doctor` shows what is wrong"
	return run.deny(out, errw, guardErrorID, guardErrorID, reason, "", audit.LayerHook)
}

// emitDecision reports a decision: the --json object on stdout when asked
// for (block, the category, the rule id and the reason), and for blocks the
// reason on stderr (Claude Code shows stderr of an exit-2 hook to the
// model).
func emitDecision(out, errw io.Writer, asJSON, block bool, category, rule, reason string) {
	if asJSON {
		_ = json.NewEncoder(out).Encode(struct {
			Block    bool   `json:"block"`
			Category string `json:"category,omitempty"`
			Rule     string `json:"rule"`
			Reason   string `json:"reason"`
		}{block, category, rule, reason})
	}
	if block {
		fmt.Fprintln(errw, reason)
	}
}

// maxHookPayload bounds stdin: the hook payload is small, and a runaway
// producer must not make tq allocate without limit.
const maxHookPayload = 1 << 20

// errPayloadTooLarge is returned when stdin exceeds maxHookPayload. Truncating
// silently would make the JSON unparseable and therefore "allow"; an oversized
// payload is a protocol violation and pre-tool-use fails closed on it.
var errPayloadTooLarge = errors.New("hook payload exceeds 1 MiB")

// readHookPayload decodes the hook JSON and extracts the shell command
// (Bash or PowerShell; "" for any other tool). A payload tq cannot read
// (a read error, more than maxHookPayload, JSON that does not parse into
// the payload object) is an error, and pre-tool-use denies it (PRD v2 §6.3,
// fail closed): tq never allows a call it did not read.
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
		// encoding/json would read invalid UTF-8 as U+FFFD, judging a
		// different command than the one the harness runs.
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
// gh login. It also returns the resolution, so the hook can deny when the
// cwd names a workspace whose manifest tq cannot read; a registry it
// cannot read is the error.
func gatherGuardInput(cwd, cmdline string) (guard.Input, *resolve.Workspace, *registry.Config, resolve.Result, error) {
	if strings.TrimSpace(cwd) == "" {
		// If Getwd also fails, cwd stays empty: resolve finds no workspace, the
		// input is neutral, and the guard fails closed for remote mutations.
		cwd, _ = os.Getwd()
	}
	cfg, err := registry.Load()
	if err != nil {
		return guard.Input{}, nil, nil, resolve.Result{}, err
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
		return in, nil, cfg, rep.Result, nil
	}

	m := ws.Manifest
	in.Client = m.Client
	in.ExpectedEmail = strings.TrimSpace(m.Git.Email)
	in.CloudProvider = manifest.CloudProviderName(cloudString(m.Cloud, "provider"))
	// An integrity mismatch is at least as strict as a workspace never
	// allowed: doctor reports untrusted for it too, so the guard refuses
	// every mutating git; and a remote mutation (git push, gh, a cloud CLI)
	// is judged as from no workspace at all, refused with the reason, which
	// names the first changed file and tq hooks verify. The blocked-command
	// and wrong-cloud rules of the manifest still run first.
	if rep.Result.Workspace == nil && rep.Result.Mismatch && guard.IsRemoteMutation(cmdline) {
		in.Neutral = true
		in.NeutralReason = rep.Result.Reason
	}
	// cloud.also widens what the guard allows, so only a TRUSTED manifest may
	// set it: an edited (untrusted) one must not unlock another cloud CLI.
	if rep.Result.Workspace != nil {
		for _, x := range cloudStrings(m.Cloud, "also") {
			in.ExtraClouds = append(in.ExtraClouds, manifest.CloudProviderName(x))
		}
	}
	in.Blocked = effectiveBlocked(ws)
	// guard: v2 on a trusted manifest: the provider scope judges cloud CLIs (ADR-0006).
	in.Scoped = rep.Result.Workspace != nil && m.GuardV2()

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
	// Only a TRUSTED workspace is handed on: the cross-client MCP check
	// compares bundles, and an untrusted manifest must not take part.
	return in, rep.Result.Workspace, cfg, rep.Result, nil
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
