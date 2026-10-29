package pgtap

import (
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Failure is one failed pgTAP test: the test file and its test number (0
// for a file-level failure such as a bad plan or a script error).
type Failure struct {
	File string
	Test int
	Desc string
}

// TAPResult is what ParseTAP read from pg_prove's (TAP::Harness) output.
type TAPResult struct {
	Failures     []Failure
	Files, Tests int
	// Pass is the harness's "Result: PASS".
	Pass bool
}

var (
	// "file.sql .. " (prove pads the dots), optionally followed by a result.
	headerRe  = regexp.MustCompile(`^(\S.*?\.(?:sql|pg|test)) \.{2,}(?: |$)`)
	notOkRe   = regexp.MustCompile(`^not ok (\d+)(?:\s*-?\s*(.*))?$`)
	summaryRe = regexp.MustCompile(`^(\S.*?) \(Wstat: \d+ Tests: \d+ Failed: \d+\)\s*$`)
	failedRe  = regexp.MustCompile(`^\s+Failed tests?:\s+(.*)$`)
	parseRe   = regexp.MustCompile(`^\s+Parse errors:\s+(.*)$`)
	nonZeroRe = regexp.MustCompile(`^\s+Non-zero exit status:\s+(\d+)`)
	filesRe   = regexp.MustCompile(`^Files=(\d+), Tests=(\d+)`)
)

// ParseTAP reads pg_prove output: the "not ok N - description" lines under
// each file's header (pg_prove --failures prints them), and the Test Summary
// Report (failed test numbers, parse errors, a non-zero exit). mapPath maps
// a reported path to the repository's test file (nil keeps it).
func ParseTAP(out []byte, mapPath func(string) string) TAPResult {
	if mapPath == nil {
		mapPath = func(s string) string { return s }
	}
	var res TAPResult
	type key struct {
		file string
		n    int
	}
	seen := map[key]int{} // index into res.Failures
	add := func(file string, n int, desc string) {
		file = mapPath(file)
		k := key{file, n}
		if i, ok := seen[k]; ok {
			if res.Failures[i].Desc == "" {
				res.Failures[i].Desc = desc
			}
			return
		}
		seen[k] = len(res.Failures)
		res.Failures = append(res.Failures, Failure{File: file, Test: n, Desc: desc})
	}
	cur, summary := "", ""
	inSummary := false
	fileLevel := map[string]bool{}
	for _, raw := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		l := strings.TrimRight(raw, " \t\r")
		switch {
		case strings.HasPrefix(l, "Test Summary Report"):
			inSummary = true
		case filesRe.MatchString(l):
			m := filesRe.FindStringSubmatch(l)
			res.Files, _ = strconv.Atoi(m[1])
			res.Tests, _ = strconv.Atoi(m[2])
			inSummary = false
		case strings.HasPrefix(l, "Result: "):
			res.Pass = strings.TrimSpace(strings.TrimPrefix(l, "Result: ")) == "PASS"
		case inSummary && summaryRe.MatchString(l):
			summary = summaryRe.FindStringSubmatch(l)[1]
		case inSummary && summary != "" && failedRe.MatchString(l):
			for _, n := range numbers(failedRe.FindStringSubmatch(l)[1]) {
				add(summary, n, "")
			}
		case inSummary && summary != "" && parseRe.MatchString(l):
			desc := strings.TrimSpace(parseRe.FindStringSubmatch(l)[1])
			add(summary, 0, desc)
			res.Failures[seen[key{mapPath(summary), 0}]].Desc = desc // a parse error says more than the exit status
			fileLevel[mapPath(summary)] = true
		case inSummary && summary != "" && nonZeroRe.MatchString(l):
			if !fileLevel[mapPath(summary)] {
				add(summary, 0, "the test script exited "+nonZeroRe.FindStringSubmatch(l)[1])
				fileLevel[mapPath(summary)] = true
			}
		case !inSummary && headerRe.MatchString(l):
			cur = headerRe.FindStringSubmatch(l)[1]
		case !inSummary && cur != "" && notOkRe.MatchString(l):
			m := notOkRe.FindStringSubmatch(l)
			n, _ := strconv.Atoi(m[1])
			desc := m[2]
			if i := strings.Index(desc, " # "); i >= 0 && strings.Contains(strings.ToUpper(desc[i:]), "TODO") {
				continue // a TODO test is expected to fail
			}
			add(cur, n, strings.TrimSpace(desc))
		}
	}
	sort.SliceStable(res.Failures, func(i, j int) bool {
		a, b := res.Failures[i], res.Failures[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Test < b.Test
	})
	return res
}

// numbers expands "2, 5, 7-9".
func numbers(s string) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		a, b, rng := strings.Cut(part, "-")
		x, err := strconv.Atoi(strings.TrimSpace(a))
		if err != nil {
			continue
		}
		y := x
		if rng {
			if y, err = strconv.Atoi(strings.TrimSpace(b)); err != nil || y < x || y-x > 10000 {
				y = x
			}
		}
		for n := x; n <= y; n++ {
			out = append(out, n)
		}
	}
	return out
}

// MapPaths maps a path reported by the harness to one of the local test
// files: an exact match, else the local file whose path is the longest
// suffix match (supabase test db reports container paths such as
// /tmp/supabase/tests/…), else the unique base-name match, else the base
// name under dir.
func MapPaths(local []string, dir string) func(string) string {
	return func(p string) string {
		p = strings.ReplaceAll(p, `\`, "/")
		best := ""
		for _, l := range local {
			if l == p {
				return l
			}
			// Compare from the end, segment by segment.
			ls, ps := strings.Split(l, "/"), strings.Split(p, "/")
			n := 0
			for n < len(ls) && n < len(ps) && ls[len(ls)-1-n] == ps[len(ps)-1-n] {
				n++
			}
			if n >= 2 && (best == "" || n > common(best, p)) {
				best = l
			}
		}
		if best != "" {
			return best
		}
		var byBase []string
		for _, l := range local {
			if path.Base(l) == path.Base(p) {
				byBase = append(byBase, l)
			}
		}
		if len(byBase) == 1 {
			return byBase[0]
		}
		return path.Join(dir, path.Base(p))
	}
}

func common(a, b string) int {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	n := 0
	for n < len(as) && n < len(bs) && as[len(as)-1-n] == bs[len(bs)-1-n] {
		n++
	}
	return n
}
