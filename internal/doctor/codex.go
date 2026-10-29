package doctor

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Owned by WS4 (spec E §12, §17).

func init() {
	Register(Check{Name: "codex", Workspace: codexWorkspaceCheck})
}

var (
	hookStateRe      = regexp.MustCompile(`^\s*\[\s*hooks\.state\.\"([^"]*)\"\s*\]\s*$`)
	trustedHashRe    = regexp.MustCompile(`^\s*trusted_hash\s*=\s*"[^"]+"`)
	featuresTableRe  = regexp.MustCompile(`^\s*\[\s*features\s*\]\s*$`)
	hooksDisabledRe  = regexp.MustCompile(`^\s*hooks\s*=\s*false\b`)
	anyTableHeaderRe = regexp.MustCompile(`^\s*\[`)
)

// codexWorkspaceCheck looks at tq's Codex hook in a workspace that isolates
// a codex identity:
//
//   - codex-hooks-missing: hooks.json has no tq PreToolUse entry (Codex runs
//     without tq's guard);
//   - codex-hooks-disabled: config.toml sets [features] hooks = false;
//   - codex-hooks-untrusted: config.toml has no [hooks.state."…"] record with
//     a trusted_hash for tq's handler. Codex silently skips an untrusted hook.
//     The record format is not in the Codex docs (A8): it follows Codex 0.13x
//     as third-party tools describe it, so the message says how to check.
//
// It reads files only (no subprocess).
func codexWorkspaceCheck(w *resolve.Workspace, d Deps, add AddFunc) {
	if w == nil || w.Manifest == nil || !slices.Contains(w.Manifest.IdentityNames(), "codex") {
		return
	}
	read := d.ReadFile
	if read == nil {
		read = os.ReadFile
	}
	dir := paths.IdentityDir(w.Name, "codex")
	hp := filepath.Join(dir, codexcfg.HooksFile)
	b, err := read(hp)
	g, h, ok := 0, 0, false
	if err == nil {
		g, h, ok = codexcfg.HookIndex(b, "PreToolUse")
	}
	if !ok {
		add("warn", "codex-hooks-missing", w.Name, "tq's Codex hook is not in "+hp+": Codex runs without tq's guard in this workspace", "tq settings render "+w.Name)
		return
	}
	cfg, _ := read(filepath.Join(dir, "config.toml"))
	lines := strings.Split(strings.ReplaceAll(string(cfg), "\r\n", "\n"), "\n")
	if hooksDisabled(lines) {
		add("warn", "codex-hooks-disabled", w.Name, "the codex identity's config.toml sets [features] hooks = false: tq's guard never runs in Codex", "remove hooks = false from "+filepath.Join(dir, "config.toml"))
		return
	}
	if !hookTrusted(lines, g, h) {
		add("warn", "codex-hooks-untrusted", w.Name,
			"no Codex trust record found for tq's PreToolUse hook: Codex skips a hook until you trust it, and again after it changes",
			"start codex in "+w.Name+" (tq run "+w.Name+" -- codex) and trust tq's hooks in /hooks; tq never passes --dangerously-bypass-hook-trust")
	}
}

func hooksDisabled(lines []string) bool {
	in := false
	for _, l := range lines {
		switch {
		case featuresTableRe.MatchString(l):
			in = true
		case anyTableHeaderRe.MatchString(l):
			in = false
		case in && hooksDisabledRe.MatchString(l):
			return true
		}
	}
	return false
}

// hookTrusted reports whether a [hooks.state."<…>hooks.json:pre_tool_use:G:H"]
// table carries a trusted_hash.
func hookTrusted(lines []string, g, h int) bool {
	suffixes := []string{
		"hooks.json:pre_tool_use:" + strconv.Itoa(g) + ":" + strconv.Itoa(h),
		"hooks.json:PreToolUse:" + strconv.Itoa(g) + ":" + strconv.Itoa(h),
	}
	in := false
	for _, l := range lines {
		if m := hookStateRe.FindStringSubmatch(l); m != nil {
			in = false
			for _, s := range suffixes {
				if strings.HasSuffix(m[1], s) {
					in = true
				}
			}
			continue
		}
		if anyTableHeaderRe.MatchString(l) {
			in = false
			continue
		}
		if in && trustedHashRe.MatchString(l) {
			return true
		}
	}
	return false
}
