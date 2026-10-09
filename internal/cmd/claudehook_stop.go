package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/secrets"
)

// newStopCmd is the Stop hook: the completion-evidence check ("Belay").
// It never fails and never exits non-zero; any error means "do nothing".
func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop hook: ask Jev whether a claimed verification is backed by the transcript",
		RunE: func(c *cobra.Command, _ []string) error {
			stopCheck(c.InOrStdin(), c.OutOrStdout())
			return nil
		},
	}
}

// newPromptSubmitCmd is the UserPromptSubmit hook: the skill picker. It
// never blocks a prompt.
func newPromptSubmitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prompt-submit",
		Short: "UserPromptSubmit hook: log (and in enforce mode hint) the skill Jev thinks fits",
		RunE: func(c *cobra.Command, _ []string) error {
			promptSubmit(c.InOrStdin(), c.OutOrStdout())
			return nil
		},
	}
}

// jevWorkspaceFor resolves the trusted workspace for cwd when its policy
// turns Jev on, or nil.
func jevWorkspaceFor(cwd string) *resolve.Workspace {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	ws, _ := resolveTrusted(cwd)
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Decision.Enabled() {
		return nil
	}
	return ws
}

var safeSessionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// stopMarker is the once-per-session marker. A session id that is not a
// plain token (or is missing) is hashed, so it can never escape the dir.
func stopMarker(sessionID, transcript string) string {
	id := strings.TrimSpace(sessionID)
	if !safeSessionID.MatchString(id) {
		sum := sha256.Sum256([]byte(id + "\n" + transcript))
		id = hex.EncodeToString(sum[:16])
	}
	return filepath.Join(decideDir(), "stop", id)
}

func stopCheck(r io.Reader, w io.Writer) {
	p, _, err := readHookPayload(r)
	if err != nil || p.StopHookActive || strings.TrimSpace(p.TranscriptPath) == "" {
		return
	}
	ws := jevWorkspaceFor(p.Cwd)
	if ws == nil {
		return
	}
	pol := ws.Manifest.Decision
	marker := stopMarker(p.SessionID, p.TranscriptPath)
	if pol.Enforcing() {
		if _, err := os.Stat(marker); err == nil {
			return // already sent back once this session
		}
	}
	st, err := decide.ReadStopState(p.TranscriptPath)
	if err != nil || !st.HasEdits() || strings.TrimSpace(st.Final) == "" {
		return
	}
	br := jevBreaker()
	if br.Open() {
		return
	}
	cl, err := jevClientFor(ws, "stop", decide.HookTimeout)
	if err != nil {
		return
	}
	v := decide.CheckStop(context.Background(), cl, pol, st)
	if v.Err != nil {
		br.Trip()
	} else {
		br.Reset()
	}
	j := decide.Judgment{Workspace: ws.Name, Kind: "stop", Mode: policyMode(pol),
		P:     map[string]float64{},
		Extra: map[string]string{"edits": fmt.Sprint(len(st.Edited)), "commands": fmt.Sprint(len(st.Commands))}}
	if v.Err != nil {
		j.Error = v.Err.Error()
	} else {
		j.P["claims"] = v.Claims
		j.P["evidence"] = v.Evidence
		if v.Would {
			j.Would = map[string]string{"stop": "block"}
		}
	}
	if v.Apply {
		// The marker is what makes this once per session: without it, do
		// not block at all.
		if os.MkdirAll(filepath.Dir(marker), 0o700) != nil || os.WriteFile(marker, []byte(ws.Name+"\n"), 0o600) != nil {
			v.Apply = false
		}
	}
	if v.Apply {
		j.Applied = map[string]string{"stop": "block"}
	}
	judgmentLog().Write(j)
	if !v.Apply {
		return
	}
	reason := fmt.Sprintf("tq completion check: your final message says the work is verified (tests, build or lint pass), "+
		"but the transcript shows no successful verification command after the last edit. "+
		"Run the relevant check now and report its result, or say plainly that the change is unverified. "+
		"(Jev claims=%.2f evidence=%.2f; this check runs once per session and may be wrong.)", v.Claims, v.Evidence)
	_ = json.NewEncoder(w).Encode(map[string]string{"decision": "block", "reason": reason})
}

// claudeConfigDir is the active Claude Code config dir.
func claudeConfigDir() string {
	if d := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); d != "" {
		return d
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".claude")
	}
	return ""
}

func promptSubmit(r io.Reader, w io.Writer) {
	p, _, err := readHookPayload(r)
	prompt := strings.TrimSpace(p.Prompt)
	// A slash command already names what to run: nothing to pick.
	if err != nil || prompt == "" || strings.HasPrefix(prompt, "/") {
		return
	}
	ws := jevWorkspaceFor(p.Cwd)
	if ws == nil {
		return
	}
	pol := ws.Manifest.Decision
	br := jevBreaker()
	if br.Open() {
		return
	}
	cl, err := jevClientFor(ws, "skill", decide.HookTimeout)
	if err != nil {
		return
	}
	cwd := strings.TrimSpace(p.Cwd)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	skills := decide.SkillIndex(decideDir(), claudeConfigDir(), cwd)
	if len(skills) == 0 {
		return
	}
	pick := decide.PickSkill(context.Background(), cl, pol, prompt, skills)
	if pick.Err != nil {
		br.Trip()
	} else {
		br.Reset()
	}
	// Redact before truncating, so a cut can never split a secret past the
	// redactor.
	j := decide.Judgment{Workspace: ws.Name, Kind: "skill", Mode: policyMode(pol),
		Extra: map[string]string{"pick": pick.Pick, "confidence": fmt.Sprintf("%.2f", pick.Confidence),
			"applied": fmt.Sprint(pick.Apply), "skills": fmt.Sprint(len(skills)),
			"prompt": truncate(secrets.Redact(prompt), 80)}}
	if pick.Err != nil {
		j.Error = pick.Err.Error()
	}
	judgmentLog().Write(j)
	if !pick.Apply {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "UserPromptSubmit",
			"additionalContext": "Skill that may fit: " + pick.Pick,
		},
	})
}
