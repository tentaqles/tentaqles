package decide

import (
	"math"
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/secrets"
)

// The docs-only rule: the secrets question is answered by Jev from keywords,
// so a design doc that merely says "secret scanning" or "token" raises it.
// When every changed file is documentation and no added line holds a
// secret-shaped value, that flag is dropped. Code, config and workflow files,
// mixed diffs and truncated diffs keep today's behaviour.

var docExts = map[string]bool{".md": true, ".rst": true}

// isDocPath reports whether a repo-relative path is documentation:
// *.md and *.rst anywhere, or anything under the top-level docs/ directory
// (so docs/notes.txt counts; requirements.txt and CMakeLists.txt do not).
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
// content starts with "++" is still seen. odd is true when the diff holds
// a section this parser does not understand (a combined or conflict diff
// such as "diff --cc", or an "* Unmerged path" line).
func parseDiff(diff string) (files []fileSec, odd bool) {
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
		files = append(files, *cur)
	}
	for _, l := range strings.Split(diff, "\n") {
		// Hunk body lines start with ' ', '+', '-' or '\', never 'd' or '*'.
		if strings.HasPrefix(l, "* Unmerged path") || (strings.HasPrefix(l, "diff ") && !strings.HasPrefix(l, "diff --git ")) {
			odd = true
		}
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
	return files, odd
}

// docsOnlyNoSecrets is true when the diff touches only documentation files
// and none of its added lines is secret-shaped. When names (the changed-file
// list) is non-nil, every name must also have been parsed as a file here.
func docsOnlyNoSecrets(diff string, names []string) bool {
	files, odd := parseDiff(diff)
	if odd || len(files) == 0 {
		return false
	}
	if names != nil {
		parsed := map[string]bool{}
		for _, f := range files {
			if f.ok {
				parsed[f.path] = true
			}
		}
		for _, n := range names {
			if !parsed[n] {
				return false
			}
		}
	}
	for _, f := range files {
		if !f.ok || !isDocPath(f.path) {
			return false
		}
		for _, l := range f.added {
			if secrets.ScanLine(l) != "" || highEntropyToken(l) {
				return false
			}
		}
	}
	return true
}

// dropDocsSecrets removes the secrets flag from res when docsOnlyNoSecrets
// holds for diff, and recomputes the risk. Never lowers an errored or
// truncated result.
func dropDocsSecrets(res *TriageResult, diff string, names []string) {
	if res.Error != "" || res.Truncated || !docsOnlyNoSecrets(diff, names) {
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

var tokenRe = regexp.MustCompile(`[A-Za-z0-9+/=_-]{32,}`)
var alnumRe = regexp.MustCompile(`[A-Za-z0-9]+`)
var hexRe = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// highEntropyToken reports a bare token that looks like a credential: at
// least 32 chars of [A-Za-z0-9+/=_-] with Shannon entropy >= 4.0 bits/char
// (mixing two character classes), or any hex-only run of >= 32 chars. A
// commit SHA or an md5 in prose therefore keeps the flag, on purpose: it
// cannot be told from a key.
// Local to the docs-only rule; secrets.ScanLine is untouched.
func highEntropyToken(line string) bool {
	// Any hex-only run of 32+ chars (an md5, a 32-hex API key, a SHA), however
	// it is glued to separators (key=<hex>, "<hex>", ?k=<hex>), keeps the flag.
	for _, r := range alnumRe.FindAllString(line, -1) {
		if len(r) >= 32 && hexRe.MatchString(r) {
			return true
		}
	}
	for _, t := range tokenRe.FindAllString(line, -1) {
		h := entropy(t)
		// Hyphenated lowercase words (URL slugs) reach 4 bits too; a random
		// token mixes at least two of upper, lower and digits.
		if h >= 4.0 && classes(t) >= 2 {
			return true
		}
	}
	return false
}

func classes(s string) int {
	var up, lo, dg bool
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			up = true
		case r >= 'a' && r <= 'z':
			lo = true
		case r >= '0' && r <= '9':
			dg = true
		}
	}
	n := 0
	for _, b := range []bool{up, lo, dg} {
		if b {
			n++
		}
	}
	return n
}

func entropy(s string) float64 {
	freq := map[rune]float64{}
	n := 0.0
	for _, r := range s {
		freq[r]++
		n++
	}
	h := 0.0
	for _, c := range freq {
		p := c / n
		h -= p * math.Log2(p)
	}
	return h
}
