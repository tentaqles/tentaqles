package ml

// ml/env-lock (spec C §4.3): a changed dependency manifest comes with its
// changed lock, and requirements*.txt lines are pinned.

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
)

const (
	ruleLockStale   = "lock-stale"
	ruleLockMissing = "lock-missing"
	ruleUnpinned    = "unpinned"
	ruleNoHashes    = "no-hashes"
)

// envLockGlobs are the files ml/env-lock reads.
var envLockGlobs = []string{"pyproject.toml", "requirements*.in", "requirements*.txt", "environment.yml", "environment.yaml", "pixi.toml"}

var (
	reqInRe  = regexp.MustCompile(`^requirements.*\.in$`)
	reqTxtRe = regexp.MustCompile(`^requirements.*\.txt$`)
)

func runEnvLock(ctx context.Context, in *packs.Input) packs.Result {
	changed := changedSet(in)
	var fs []packs.Finding
	n := 0
	var skip skipCounter
	for _, p := range in.ChangedPaths() {
		base, dir := path.Base(p), path.Dir(p)
		if !lintMatchAny(envLockGlobs, base) || skip.stop(ctx, n, p) {
			continue
		}
		var locks []string
		switch {
		case base == "pyproject.toml":
			changedDeps, err := depsChanged(in, p)
			if err != nil {
				n++
				fs = append(fs, packs.Finding{File: p, Level: packs.LevelWarn, Rule: "unreadable", Message: "cannot read: " + shortErr(err)})
				continue
			}
			if !changedDeps {
				continue
			}
			locks = []string{"uv.lock", "poetry.lock", "pdm.lock", "pixi.lock"}
		case base == "pixi.toml":
			locks = []string{"pixi.lock"}
		case reqInRe.MatchString(base):
			locks = []string{strings.TrimSuffix(base, ".in") + ".txt"}
		case base == "environment.yml" || base == "environment.yaml":
			locks = []string{"conda-lock.yml", "conda-lock.yaml", "pixi.lock"}
		case reqTxtRe.MatchString(base):
			n++
			fs = append(fs, pinnedReqs(in, p)...)
			continue
		default:
			continue
		}
		n++
		var have []string
		fresh := false
		for _, l := range locks {
			lp := joinDir(dir, l)
			if existsNew(in, lp) {
				have = append(have, lp)
				if changed[lp] {
					fresh = true
				}
			}
		}
		switch {
		case len(have) == 0:
			fs = append(fs, packs.Finding{File: p, Level: packs.LevelError, Rule: ruleLockMissing,
				Message: fmt.Sprintf("%s changed and has no lock next to it (%s)", base, strings.Join(locks, ", "))})
		case !fresh:
			fs = append(fs, packs.Finding{File: p, Level: packs.LevelError, Rule: ruleLockStale,
				Message: fmt.Sprintf("%s dependencies changed but %s did not; re-lock and commit it", base, strings.Join(have, ", "))})
		}
	}
	fs = append(fs, skip.finding(n, "manifests")...)
	return packs.Result{Status: packs.OK, Detail: "environment lock checked for " + lintPlural(n, "manifest"), Findings: fs}
}

// depsChanged reports whether the dependency tables of a pyproject.toml
// differ from the old side (a new file with dependencies counts). A new side
// that cannot be read is an error, never "unchanged" (fail closed).
func depsChanged(in *packs.Input, p string) (bool, error) {
	nb, err := in.ReadNew(p, 4<<20)
	if err != nil {
		return false, err
	}
	nd := depsSection(string(nb))
	ob, err := in.ReadOld(p)
	if err != nil {
		return nd != "", nil
	}
	return nd != depsSection(string(ob)), nil
}

var tomlHeaderRe = regexp.MustCompile(`^\[\[?\s*([^\]]+?)\s*\]\]?\s*(#.*)?$`)

// depTable reports whether a TOML table holds dependencies.
func depTable(t string) bool {
	switch {
	case t == "project.optional-dependencies", t == "dependency-groups", t == "tool.uv", t == "tool.uv.sources",
		t == "tool.poetry.dependencies", t == "tool.poetry.dev-dependencies", t == "tool.pixi.dependencies",
		t == "tool.pixi.pypi-dependencies", t == "build-system":
		return true
	case strings.HasPrefix(t, "tool.poetry.group.") && strings.HasSuffix(t, ".dependencies"):
		return true
	}
	return false
}

var projectDepKeyRe = regexp.MustCompile(`^(dependencies|requires-python)\s*=`)

// depsSection extracts, normalised, the dependency parts of a pyproject.toml
// without a TOML parser: the dependency tables, and `dependencies` and
// `requires-python` of [project].
func depsSection(src string) string {
	var b strings.Builder
	table := ""
	inArray := 0
	for _, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		l := strings.TrimSpace(stripTomlComment(raw))
		if l == "" {
			continue
		}
		if inArray == 0 {
			if m := tomlHeaderRe.FindStringSubmatch(l); m != nil {
				table = strings.ReplaceAll(m[1], " ", "")
				continue
			}
		}
		keep := depTable(table)
		if table == "project" {
			if inArray > 0 || projectDepKeyRe.MatchString(l) {
				keep = true
				inArray += strings.Count(l, "[") - strings.Count(l, "]")
				if inArray < 0 {
					inArray = 0
				}
			}
		}
		if keep {
			b.WriteString(table)
			b.WriteByte('|')
			b.WriteString(strings.ReplaceAll(l, " ", ""))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// stripTomlComment drops a # comment that is not inside a string.
func stripTomlComment(l string) string {
	q := byte(0)
	for i := 0; i < len(l); i++ {
		c := l[i]
		switch {
		case q != 0 && c == q:
			q = 0
		case q == 0 && (c == '"' || c == '\''):
			q = c
		case q == 0 && c == '#':
			return l[:i]
		}
	}
	return l
}

// pinnedReqs checks a requirements*.txt: every requirement pinned with ==
// (error otherwise) and hashes present (warn otherwise).
func pinnedReqs(in *packs.Input, p string) []packs.Finding {
	b, err := in.ReadNew(p, 4<<20)
	if err != nil {
		return []packs.Finding{{File: p, Level: packs.LevelWarn, Rule: "unreadable", Message: "cannot read: " + shortErr(err)}}
	}
	var out []packs.Finding
	hashes, reqs := false, 0
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		start := i + 1
		l := lines[i]
		for strings.HasSuffix(strings.TrimRight(l, " \t"), "\\") && i+1 < len(lines) {
			i++
			l = strings.TrimSuffix(strings.TrimRight(l, " \t"), "\\") + " " + lines[i]
		}
		if j := strings.Index(l, " #"); j >= 0 {
			l = l[:j]
		}
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.Contains(l, "--hash=") {
			hashes = true
		}
		if strings.HasPrefix(l, "-") {
			continue // -r, -c, -e, --index-url and other options
		}
		// The pin is in the requirement, never in its environment marker:
		// `numpy; python_version == "3.10"` is unpinned.
		req := l
		if k := strings.Index(req, ";"); k >= 0 {
			req = req[:k]
		}
		req = strings.TrimSpace(req)
		if req == "" {
			continue
		}
		reqs++
		spec := strings.Fields(req)[0]
		if strings.Contains(req, "==") || strings.Contains(req, " @ ") || strings.Contains(spec, "://") {
			continue
		}
		name := spec
		if k := strings.IndexAny(name, "<>=!~;[ "); k > 0 {
			name = name[:k]
		}
		out = append(out, packs.Finding{File: p, Line: start, Level: packs.LevelError, Rule: ruleUnpinned,
			Message: fmt.Sprintf("requirement %q is not pinned with ==", name)})
	}
	if reqs > 0 && !hashes {
		out = append(out, packs.Finding{File: p, Line: 1, Level: packs.LevelWarn, Rule: ruleNoHashes,
			Message: "no --hash entries; generate the file with pip-compile --generate-hashes (or uv pip compile --generate-hashes)"})
	}
	return out
}
