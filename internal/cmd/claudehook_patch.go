package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// patchFile is one file an apply_patch payload writes, as the file rules
// see it: Tool is "Write" for an added, deleted or moved-to file and "Edit"
// for an updated one; Content is the added lines.
type patchFile struct {
	Tool, Path, Content string
}

// errBadPatch is a patch the hook cannot read. The hook denies it: a file
// list it cannot see is a write it cannot judge (guards fail closed).
var errBadPatch = errors.New("tq could not read the file list of this apply_patch payload")

// isApplyPatch reports whether call is a Codex apply_patch edit: the tool
// is apply_patch, or an edit tool with no path whose command is a patch
// (Codex sends the patch text in tool_input.command).
func isApplyPatch(call policy.ToolCall) bool {
	if call.Tool == "apply_patch" {
		return true
	}
	switch call.Tool {
	case "Edit", "Write", "MultiEdit":
		return call.Path == "" && strings.HasPrefix(strings.TrimSpace(call.Command), "*** Begin Patch")
	}
	return false
}

// parseApplyPatch reads the file headers of a Codex patch (*** Add File:,
// *** Update File:, *** Delete File: and *** Move to:). Anything it does not
// recognise is an error.
func parseApplyPatch(text string) ([]patchFile, error) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "*** Begin Patch" || strings.TrimSpace(lines[len(lines)-1]) != "*** End Patch" {
		return nil, errBadPatch
	}
	var files []patchFile
	cur := -1   // index in files of the file whose hunk lines follow
	moved := -1 // index of a Move to: target that shares cur's content
	for _, ln := range lines[1 : len(lines)-1] {
		if strings.HasPrefix(ln, "*** ") {
			head := strings.TrimRight(ln, " \t")
			var tool, p string
			switch {
			case strings.HasPrefix(head, "*** Add File:"):
				tool, p = "Write", strings.TrimPrefix(head, "*** Add File:")
			case strings.HasPrefix(head, "*** Update File:"):
				tool, p = "Edit", strings.TrimPrefix(head, "*** Update File:")
			case strings.HasPrefix(head, "*** Delete File:"):
				tool, p = "Write", strings.TrimPrefix(head, "*** Delete File:")
			case strings.HasPrefix(head, "*** Move to:"):
				if cur < 0 || files[cur].Tool != "Edit" || moved >= 0 {
					return nil, errBadPatch
				}
				tool, p = "Write", strings.TrimPrefix(head, "*** Move to:")
			case head == "*** End of File":
				continue
			default:
				return nil, errBadPatch
			}
			p = strings.TrimSpace(p)
			if p == "" {
				return nil, errBadPatch
			}
			files = append(files, patchFile{Tool: tool, Path: p})
			if strings.HasPrefix(head, "*** Move to:") {
				moved = len(files) - 1
				files[moved].Content = files[cur].Content
				continue
			}
			cur, moved = len(files)-1, -1
			if strings.HasPrefix(head, "*** Delete File:") {
				cur = -1
			}
			continue
		}
		if cur < 0 {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			return nil, errBadPatch
		}
		if strings.HasPrefix(ln, "+") {
			for _, i := range []int{cur, moved} {
				if i < 0 {
					continue
				}
				if files[i].Content != "" {
					files[i].Content += "\n"
				}
				files[i].Content += ln[1:]
			}
		}
	}
	if len(files) == 0 {
		return nil, errBadPatch
	}
	return files, nil
}

// maxPatchFiles is how many files of one apply_patch the hook judges; a
// bigger patch is denied (split it), so the hook stays within its budget.
const maxPatchFiles = 256

// patchCalls turns the files of a patch into the tool calls the file and
// path rules judge, one per path. A relative path is resolved against the
// session cwd (Codex's paths are relative to it) and every path is cleaned,
// as Claude's absolute file_path would be, so the rules see the real place.
func patchCalls(files []patchFile, cwd string) []policy.ToolCall {
	out := make([]policy.ToolCall, 0, len(files))
	for _, f := range files {
		abs := filepath.FromSlash(f.Path)
		if !filepath.IsAbs(abs) && !winAbsRe.MatchString(abs) && cwd != "" {
			abs = filepath.Join(cwd, abs)
		}
		abs = filepath.Clean(abs)
		c := policy.ToolCall{Tool: f.Tool, Path: abs, Content: f.Content}
		if _, err := os.Stat(abs); err == nil {
			c.Exists = true
		}
		out = append(out, c)
	}
	return out
}
