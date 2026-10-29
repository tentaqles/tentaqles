package data

import (
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// Inline suppressions of the dbt and SQL gates (built-in checks use these,
// not the baseline): a comment `tq:allow <rule>: <reason>` on the finding's
// line or the line above it. <rule> is the check id (data/sql-select-star)
// or the finding's rule (select-star). The reason is required. SQL takes it
// in a -- or {# #} comment, YAML in a # comment.
var reAllow = regexp.MustCompile(`tq:allow\s+([A-Za-z0-9/_.-]+)\s*:\s*\S`)

type suppressions map[int][]string

func parseSuppressions(src []byte) suppressions {
	s := suppressions{}
	if !strings.Contains(string(src), "tq:allow") {
		return s
	}
	for i, l := range strings.Split(string(src), "\n") {
		for _, m := range reAllow.FindAllStringSubmatch(l, -1) {
			s[i+1] = append(s[i+1], m[1])
		}
	}
	return s
}

func (s suppressions) allows(checkID string, f packs.Finding) bool {
	for _, line := range []int{f.Line, f.Line - 1} {
		for _, r := range s[line] {
			if r == checkID || r == f.Rule {
				return true
			}
		}
	}
	return false
}

// filterFindings drops the findings an inline suppression allows. files
// caches the suppressions per file; read loads a file's new side.
type suppressor struct {
	checkID string
	read    func(path string) []byte
	files   map[string]suppressions
}

func newSuppressor(in *packs.Input, checkID string) *suppressor {
	return &suppressor{checkID: checkID, files: map[string]suppressions{}, read: func(p string) []byte {
		b, err := in.ReadNew(p, maxFileBytes)
		if err != nil {
			return nil
		}
		return b
	}}
}

func (s *suppressor) filter(fs []packs.Finding) []packs.Finding {
	out := []packs.Finding{}
	for _, f := range fs {
		sup, ok := s.files[f.File]
		if !ok {
			sup = parseSuppressions(s.read(f.File))
			s.files[f.File] = sup
		}
		if !sup.allows(s.checkID, f) {
			out = append(out, f)
		}
	}
	return out
}
