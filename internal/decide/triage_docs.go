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

// fileSec is one file of a unified diff. ok is false when its path cannot
// be read unambiguously (quoted name, missing or odd headers).
type fileSec struct {
	path  string
	ok    bool
	added []string
}

// diffPath reads the path of a "--- a/x" / "+++ b/x" header value.
func diffPath(v, prefix string) string {
	if i := strings.IndexByte(v, '\t'); i >= 0 {
		v = v[:i]
	}
	if !strings.HasPrefix(v, prefix) || len(v) == len(prefix) {
		return "" // quoted, /dev/null or malformed
	}
	return v[len(prefix):]
}

// parseDiff splits a unified diff into files. Paths come from the ---/+++
// and rename headers, never from the "diff --git" line (a name may hold
// " b/"). Added lines are only collected inside hunks, so a line whose
// content starts with "++" is still seen.
func parseDiff(diff string) []fileSec {
	var out []fileSec
	var cur *fileSec
	var minus, plus, renameTo string
	var inHunk bool
	flush := func() {
		if cur == nil {
			return
		}
		switch {
		case renameTo != "":
			if !strings.HasPrefix(renameTo, "\"") {
				cur.path, cur.ok = strings.TrimSpace(renameTo), true
			}
		case plus != "":
			cur.path, cur.ok = plus, true
		case minus != "":
			cur.path, cur.ok = minus, true // deleted file (+++ /dev/null)
		}
		out = append(out, *cur)
	}
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "diff --git ") {
			flush()
			cur = &fileSec{}
			minus, plus, renameTo, inHunk = "", "", "", false
			continue
		}
		if cur == nil {
			continue
		}
		if inHunk {
			if strings.HasPrefix(l, "+") {
				cur.added = append(cur.added, l[1:])
			}
			continue
		}
		switch {
		case strings.HasPrefix(l, "@@"):
			inHunk = true
		case strings.HasPrefix(l, "--- "):
			minus = diffPath(l[4:], "a/")
		case strings.HasPrefix(l, "+++ "):
			plus = diffPath(l[4:], "b/")
		case strings.HasPrefix(l, "rename to "):
			renameTo = l[len("rename to "):]
		}
	}
	flush()
	return out
}

// docsOnlyNoSecrets is true when the diff touches only documentation files
// and none of its added lines is secret-shaped.
func docsOnlyNoSecrets(diff string) bool {
	files := parseDiff(diff)
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if !f.ok || !isDocPath(f.path) {
			return false
		}
		for _, l := range f.added {
			if secrets.ScanLine(l) != "" {
				return false
			}
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
