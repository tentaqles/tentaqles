package decide

import (
	"path"
	"strings"

	"github.com/tentaqles/tentaqles/internal/secrets"
)

// The docs-only rule: the secrets question is answered by Jev from keywords,
// so a design doc that merely says "secret scanning" or "token" raises it.
// When every changed file is documentation and no added line holds a
// secret-shaped value, that flag is dropped. Code, config and workflow files,
// mixed diffs and truncated diffs keep today's behaviour.

var docExts = map[string]bool{".md": true, ".rst": true, ".txt": true}

// isDocPath reports whether a repo-relative path is documentation:
// *.md, *.rst, *.txt, or anything under a docs/ directory at the repo root.
func isDocPath(p string) bool {
	p = strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "./")
	if docExts[strings.ToLower(path.Ext(p))] {
		return true
	}
	return strings.HasPrefix(p, "docs/")
}

// diffFiles returns the b-side path of each "diff --git" header.
func diffFiles(diff string) []string {
	var out []string
	for _, l := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(l, "diff --git ") {
			continue
		}
		if i := strings.LastIndex(l, " b/"); i >= 0 {
			out = append(out, strings.TrimSpace(l[i+3:]))
		}
	}
	return out
}

// docsOnlyNoSecrets is true when the diff touches only documentation files
// and none of its added lines is secret-shaped.
func docsOnlyNoSecrets(diff string) bool {
	files := diffFiles(diff)
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if !isDocPath(f) {
			return false
		}
	}
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") && secrets.ScanLine(l[1:]) != "" {
			return false
		}
	}
	return true
}

// dropDocsSecrets removes the secrets flag from res when docsOnlyNoSecrets
// holds for diff, and recomputes the risk. Never lowers an errored or
// truncated result.
func dropDocsSecrets(res *TriageResult, diff string) {
	if res.Error != "" || res.Truncated || !docsOnlyNoSecrets(diff) {
		return
	}
	kept := res.Flags[:0:0]
	for _, f := range res.Flags {
		if f != "secrets" {
			kept = append(kept, f)
		}
	}
	if len(kept) == len(res.Flags) {
		return
	}
	res.Flags = kept
	if len(kept) == 0 {
		res.Risk = "low"
	}
}
