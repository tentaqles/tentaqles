package guard

import "strings"

// heredoc is one `<<WORD` found by scanHeredocs.
type heredoc struct {
	line       int // index of the line that opens it
	start, end int // byte offsets of the `<<WORD` marker in that line
	from, to   int // body lines: [from, to); to is the closing line when closed
	closed     bool
}

// scanHeredocs finds every heredoc in lines the way a shell reads them: the
// bodies of several markers on one line follow each other, in order, and a
// body is never searched for markers of its own. Offsets always refer to
// the line they were found in, so the result can be used to slice lines.
func scanHeredocs(lines []string) []heredoc {
	var out []heredoc
	for i := 0; i < len(lines); {
		next := i + 1
		line := lines[i]
		for _, loc := range heredocStart.FindAllStringSubmatchIndex(line, -1) {
			if loc[0] > 0 && (line[loc[0]-1] == '<' || (line[loc[0]-1] >= '0' && line[loc[0]-1] <= '9')) {
				continue // a `<<<` here-string or an arithmetic shift, not a heredoc
			}
			word := line[loc[4]:loc[5]]
			h := heredoc{line: i, start: loc[0], end: loc[1], from: next, to: len(lines)}
			for j := next; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == word {
					h.to, h.closed = j, true
					break
				}
			}
			out = append(out, h)
			if !h.closed {
				next = len(lines)
				break
			}
			next = h.to + 1
		}
		i = next
	}
	return out
}

func blankLines(lines []string, from, to int) {
	for k := from; k < to && k < len(lines); k++ {
		lines[k] = strings.Repeat(" ", len(lines[k]))
	}
}
