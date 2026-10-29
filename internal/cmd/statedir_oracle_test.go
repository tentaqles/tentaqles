package cmd

// The pre-optimization state-dir matcher (commit 030b92a: regexps, no
// prefilters), kept as the oracle for the optimized protectedMention and
// rootMatcher (R5-2). rawDots, normShell, stateInfo and the other shared
// helpers are the production ones: a semantic change there applies to both.

import (
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	oracleTokenRe   = regexp.MustCompile(`[^\s;&|()<>,=]+`)
	oracleSegSplit  = regexp.MustCompile(`&&|\|\||[;&|\n]`)
	oracleWordSplit = regexp.MustCompile(`[\s()<>=,]+`)
	oracleRootMu    sync.Mutex
	oracleRootCache = map[string]*regexp.Regexp{}
)

func oracleRootRegex(forms []string) *regexp.Regexp {
	key := strings.Join(forms, "\x00")
	oracleRootMu.Lock()
	defer oracleRootMu.Unlock()
	if re, ok := oracleRootCache[key]; ok {
		return re
	}
	re := computeRootRegexOracle(forms)
	oracleRootCache[key] = re
	return re
}

func expandInsideOracle(n string) string {
	si := stateInfo()
	vars := si.vars
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for _, k := range names {
		v := vars[k]
		for _, sp := range []string{"${" + k + "}", "$env:" + k, "%" + k + "%", "$" + k} {
			n = strings.ReplaceAll(n, sp, v)
		}
		// The bare name (os.environ[X], process.env.X): quotes and brackets are
		// already folded away, so no left boundary is required.
		n = si.bare[k].ReplaceAllString(n, v+"${1}")
	}
	return n
}

func computeRootRegexOracle(forms []string) *regexp.Regexp {
	forms = append([]string(nil), forms...)
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	alts := []string{`tentaq~[0-9]+`}
	for _, f := range forms {
		if f != "" && f != "tentaq~" {
			alts = append(alts, regexp.QuoteMeta(f))
		}
	}
	return regexp.MustCompile("(" + strings.Join(alts, "|") + ")")
}

func proseFreeOracle(raw string) string {
	raw = msgArg.ReplaceAllString(raw, " ")
	var out []string
	for _, seg := range proseSplit.Split(raw, -1) {
		if echoSeg.MatchString(seg) && !strings.ContainsAny(seg, ">|") {
			continue
		}
		out = append(out, seg)
	}
	return strings.Join(out, " ; ")
}

// foldPaths folds a command for path matching: inside variables expanded,
// string concatenation (+) dropped, and `..` resolved lexically in every
// path-like token. It also reports a root mention followed by `..`.
func foldPathsOracle(n string, re *regexp.Regexp) (string, bool) {
	n = strings.ReplaceAll(expandInsideOracle(n), "+", "")
	dotdot := false
	n = oracleTokenRe.ReplaceAllStringFunc(n, func(tok string) string {
		if !strings.Contains(tok, "/") {
			return tok
		}
		if loc := re.FindStringIndex(tok); loc != nil && strings.Contains(tok[loc[1]:], "/..") {
			dotdot = true
		}
		if strings.Contains(tok, "..") {
			lead := ""
			if strings.HasPrefix(tok, "~") {
				lead, tok = "~", tok[1:]
			}
			c := path.Clean(tok)
			return lead + c
		}
		return tok
	})
	return n, dotdot
}

// rawDots is the command with only quotes folded, for the .. check (the

func protectedMentionOracle(raw string) bool {
	si := stateInfo()
	re := oracleRootRegex(si.forms)
	n, dotdot := foldPathsOracle(normShell(proseFreeOracle(raw)), re)
	// Fail closed: a root spelling and a `..` path in one command (a
	// variable set to a path under the root, then `/../packs`).
	if dotdot || (re.MatchString(n) && dotdotRe.MatchString(rawDots(raw))) {
		return true
	}
	bare := false
	mentioned := false
	for _, l := range re.FindAllStringIndex(n, -1) {
		mentioned = true
		rest := n[l[1]:]
		if strings.HasPrefix(rest, "/") {
			seg := firstSeg(rest[1:])
			if seg == "" {
				bare = true
				continue
			}
			if si.isProt(seg) || strings.ContainsAny(seg, "*?{") {
				return true
			}
			continue
		}
		// An argv or join list: joinpath(.tentaqles, packs, ...).
		if strings.HasPrefix(rest, ",") && si.isProt(firstSeg(strings.TrimLeft(rest, ", "))) {
			return true
		}
		bare = true
	}
	if mentioned && linkVerb.MatchString(n) {
		return true
	}
	if bare {
		for _, tok := range oracleTokenRe.FindAllString(n, -1) {
			if !strings.Contains(tok, "/") {
				continue
			}
			for _, s := range strings.Split(tok, "/") {
				if si.isProt(s) {
					return true
				}
			}
		}
	}
	// Cumulative cd: rel is the virtual cwd under the root, known while the
	// cds stay under it.
	known, rel := false, ""
	for _, seg := range oracleSegSplit.Split(n, -1) {
		if m := cdRe.FindStringSubmatch(seg); m != nil {
			t := strings.TrimSuffix(m[2], "/")
			if loc := re.FindStringIndex(t); loc != nil && loc[0] == 0 {
				known, rel = true, strings.Trim(path.Clean("/"+t[loc[1]:]), "/")
			} else if known && !path.IsAbs(t) && !strings.HasPrefix(t, "~") && !strings.HasPrefix(t, "$") {
				c := path.Clean(rel + "/" + t)
				if c == ".." || strings.HasPrefix(c, "../") {
					known = false
				} else {
					rel = strings.Trim(c, "./")
					if c == "." {
						rel = ""
					}
				}
			} else {
				known = false
			}
			if known && si.isProt(firstSeg(rel)) {
				return true
			}
			continue
		}
		if !known {
			continue
		}
		for _, w := range oracleWordSplit.Split(seg, -1) {
			w = strings.TrimPrefix(w, "./")
			if w == "" || strings.HasPrefix(w, "-") || strings.Contains(w, "://") || path.IsAbs(w) || strings.HasPrefix(w, "~") || strings.HasPrefix(w, "$") {
				continue
			}
			full := strings.TrimPrefix(path.Clean(rel+"/"+w), "/")
			if si.isProt(firstSeg(full)) {
				return true
			}
		}
	}
	return false
}
