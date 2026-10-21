package commitscan

import (
	"regexp"
	"strconv"
	"strings"
)

var hunkHeaderRe = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// ParseDiffAdded walks a unified diff (git diff, or git log -p) and returns
// every added line with its file and its line number in the new file.
//
// It is hunk-aware so that content cannot forge structure: a file header is
// accepted only as the "+++ " line that directly follows a "--- " line inside
// a "diff --git" block, and inside a hunk the header's line counts decide
// what is content, so an added line that itself starts with "++ " (a content
// line "+++ x") stays content of the real file.
func ParseDiffAdded(diff string) []AddedLine {
	var out []AddedLine
	file := ""
	sawDiff, sawMinus := false, false
	oldLeft, newLeft, line := 0, 0, 0
	for _, l := range strings.Split(diff, "\n") {
		if oldLeft > 0 || newLeft > 0 {
			switch {
			case strings.HasPrefix(l, "+"):
				if newLeft > 0 {
					newLeft--
					if file != "/dev/null" {
						out = append(out, AddedLine{File: file, Line: line, Text: strings.TrimRight(l[1:], "\r")})
					}
					line++
				}
			case strings.HasPrefix(l, "-"):
				if oldLeft > 0 {
					oldLeft--
				}
			case strings.HasPrefix(l, " "):
				if oldLeft > 0 {
					oldLeft--
				}
				if newLeft > 0 {
					newLeft--
					line++
				}
			}
			continue
		}
		switch {
		case strings.HasPrefix(l, "diff --git "):
			sawDiff, sawMinus = true, false
			file = ""
		case sawDiff && strings.HasPrefix(l, "--- "):
			sawMinus = true
		case sawDiff && sawMinus && strings.HasPrefix(l, "+++ "):
			name := strings.TrimSpace(strings.TrimRight(strings.TrimPrefix(l, "+++ "), "\r"))
			name = strings.Trim(name, `"`)
			if name != "/dev/null" {
				name = strings.TrimPrefix(name, "b/")
			}
			file = name
			sawMinus = false
		case strings.HasPrefix(l, "@@"):
			m := hunkHeaderRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			oldLeft = countOrOne(m[1])
			newLeft = countOrOne(m[3])
			line, _ = strconv.Atoi(m[2])
		}
	}
	return out
}

func countOrOne(s string) int {
	if s == "" {
		return 1
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 1
	}
	return n
}
