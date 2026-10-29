package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/setup"
	"github.com/tentaqles/tentaqles/internal/statusline"
)

// Owned by WS3 (spec E §10.4, §17).

// modeLineInput is what SessionStart knows when it renders the guard line.
type modeLineInput struct {
	WS         *resolve.Workspace // the trusted workspace; nil in a neutral folder
	Cwd        string
	Resolved   guardcfg.Resolved // the session's resolved mode (resolveMode)
	ClaudeMode string            // the payload's permission_mode ("" unknown)
	Agent      string            // hookAgentClaude | hookAgentCodex
	Now        time.Time
}

// modeLine is the SessionStart context line for the guard mode, e.g.
// "Guard: relaxed (workspace, granted) · 3 confirms and 1 block skipped in 24h
// · locked: identity, secrets, self-protection, privacy"; "" prints nothing.
// It replaces unattendedLine.
//
// In a workspace the line is always printed: it names the locked groups the
// agent cannot get past. In a neutral folder it is printed only when the
// global layer changes something.
func modeLine(in modeLineInput) string {
	res := in.Resolved
	mode := res.Mode
	if mode == "" {
		mode = guardcfg.ModeDefault
	}
	requested := res.Requested
	clampedMode := requested != "" && requested != mode
	if in.WS == nil && res.IsDefault() && !clampedMode && len(res.Ignored) == 0 && len(res.Clamped) == 0 {
		return ""
	}

	paren := ""
	if clampedMode {
		target := "--global"
		if in.WS != nil {
			target = in.WS.Name
		}
		paren = fmt.Sprintf("%s requested, not granted: tq allow --mode %s %s", requested, target, requested)
	} else {
		var tail []string
		if s := modeSourceLabel(res.ModeSource, in.WS); s != "" {
			tail = append(tail, s)
		}
		if res.Ceiling != nil && res.LooserThanDefault() {
			tail = append(tail, "granted")
		}
		head := ""
		if mode == guardcfg.ModeCustom {
			head = customSummary(res)
		}
		switch {
		case head != "" && len(tail) > 0:
			paren = head + "; " + strings.Join(tail, ", ")
		case head != "":
			paren = head
		default:
			paren = strings.Join(tail, ", ")
		}
	}
	line := "Guard: " + mode
	if paren != "" {
		line += " (" + paren + ")"
	}
	parts := []string{line}
	if in.WS != nil && res.LooserThanDefault() {
		now := in.Now
		if now.IsZero() {
			now = time.Now()
		}
		parts = append(parts, skippedCounts(in.WS.Name, now)+" skipped in 24h")
	}
	if len(res.Ignored) > 0 {
		parts = append(parts, "repo file tried to loosen: "+strings.Join(res.Ignored, ", "))
	}
	if len(res.Clamped) > 0 {
		parts = append(parts, "clamped to the grant: "+strings.Join(res.Clamped, ", "))
	}
	parts = append(parts, "locked: "+lockedList())
	switch in.ClaudeMode {
	case "", "bypassPermissions":
	case "auto":
		parts = append(parts, "Claude mode: auto (adds classifier prompts tq cannot configure)")
	default:
		parts = append(parts, "Claude mode: "+in.ClaudeMode+" (Claude also prompts for calls tq does not cover)")
	}
	if in.Agent == hookAgentCodex {
		var m *manifest.Manifest
		if in.WS != nil {
			m = in.WS.Manifest
		}
		parts = append(parts, "asks become blocks in Codex: "+plural(len(codexAskIDs(res, m)), "rule"))
	}
	return strings.Join(parts, " · ")
}

// lockedList names the locked groups: "identity, secrets, self-protection, privacy".
func lockedList() string {
	var s []string
	for _, c := range guardcfg.LockedCategories() {
		s = append(s, string(c))
	}
	return strings.Join(s, ", ")
}

// modeSourceLabel names the layer that set the mode for a person; "" for the
// built-in default.
func modeSourceLabel(src string, ws *resolve.Workspace) string {
	switch {
	case src == "" || src == "builtin":
		return ""
	case src == "global":
		return "global"
	case src == "launch":
		return "launch override"
	case src == "repo":
		return "repo file"
	case strings.HasPrefix(src, "project:"):
		return "project " + strings.TrimPrefix(src, "project:")
	case ws != nil && src == ws.Name:
		return "workspace"
	}
	return src
}

// customSummary is "relaxed + db-destructive ask" (up to three changes, then
// a count): a custom table against its base.
func customSummary(res guardcfg.Resolved) string {
	base := res.Base
	if !guardcfg.ValidBase(base) {
		base = guardcfg.ModeDefault
	}
	diff := setup.CustomDiff(res)
	switch {
	case len(diff) == 0:
		return base
	case len(diff) <= 3:
		return base + " + " + strings.Join(diff, ", ")
	}
	return fmt.Sprintf("%s + %d changes", base, len(diff))
}

// codexAskIDs lists the known rule ids whose effective action under res is
// ask. Codex hooks cannot ask, so the Codex adapter turns these into blocks
// (spec E §12), except shell calls that match a prompt prefix rule.
func codexAskIDs(res guardcfg.Resolved, m *manifest.Manifest) []string {
	seen := map[string]bool{}
	var out []string
	for _, ri := range guardcfg.Known(m, guardcfg.File{}) {
		r := policy.Rule{ID: ri.ID, Action: ri.Action}
		if !seen[ri.ID] && res.Effective(&r) == policy.Ask {
			seen[ri.ID] = true
			out = append(out, ri.ID)
		}
	}
	sort.Strings(out)
	return out
}

// statuslineModeFacts fills the status line's guard segment (spec E §10.4):
// "off" under TQ_MODE=off, else the resolved mode and the 24h skip count.
// It reads local files only, like the rest of the status line.
func statuslineModeFacts(f *statusline.Facts, ws *resolve.Workspace, cwd string) {
	if guardcfg.IsOff(launchMode()) {
		f.Mode = statusline.ModeOff
		return
	}
	res := resolveMode(ws, cwd, "")
	f.Mode = res.Mode
	if f.Mode == "" {
		f.Mode = guardcfg.ModeDefault
	}
	f.Looser = res.LooserThanDefault()
	if ws != nil {
		f.Skipped = statusline.Skipped(ws.Name, time.Now())
	}
}
