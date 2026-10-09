package decide

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Explore finds where a question is answered in a codebase, in two stages:
//
//  1. a cheap, deterministic keyword pre-filter (the caller greps for the
//     terms ExploreTerms picks and hands the hits to BuildSpans), which
//     yields ~40-line candidate spans ranked by keyword hits;
//  2. one batched Jev call (split only to stay under the request cap) that
//     asks, per span, "is this relevant to the question?".
//
// Jev only re-ranks the candidates stage 1 found; it never adds one. When
// Jev is unavailable the keyword ranking is the answer.

// SpanLines is the size of a candidate span around a hit.
const SpanLines = 40

// maxSpanLines caps a span grown by merging overlapping windows.
const maxSpanLines = 60

// maxSpanText caps the text of one span sent to Jev, so a file with very
// long lines cannot crowd every other span out of a batch.
const maxSpanText = 4000

// exploreBatchBudget is the request size one batch aims for, leaving room
// under MaxStateBytes for the framing and JSON escaping growth.
const exploreBatchBudget = MaxStateBytes - 12_000

// Hit is one matching line from the pre-filter.
type Hit struct {
	Path string // slash-separated, relative to the search root
	Line int    // 1-based
	Text string
}

// Span is one candidate: a line range in a file.
type Span struct {
	ID    string   `json:"id"`
	Path  string   `json:"path"`
	Start int      `json:"start"`
	End   int      `json:"end"`
	Terms []string `json:"terms"`
	Hits  int      `json:"hits"`
	// KeywordScore ranks spans in stage 1 and in the fallback.
	KeywordScore float64 `json:"keyword_score"`
	// Score is Jev's relevance probability, or KeywordScore normalized to
	// [0,1] in the fallback.
	Score float64 `json:"score"`
	Text  string  `json:"-"`
}

var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an and are as at be been but by can could do does did done for from
		get gets got has have how i if in into is it its me my of on or our should so some such than that
		the their them then there these they this those to up us use used uses using was we were what when
		where which while who whom why will with would you your yours about after all also any because before
		between both each every find found here just like make makes many more most much need needs new not
		now only other over own same see show shows tell thing things through under very via want wants way
		code file files function functions method methods class classes work works handle handles happen
		happens implemented implement implementation defined define where's what's does's`) {
		stopwords[w] = true
	}
}

var termRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.\-]*[A-Za-z0-9_]|[A-Za-z]{3,}`)

// ExploreTerms picks the search terms in a question: identifiers and words
// of three or more letters, minus stopwords, longest first, capped at max.
// A camelCase or snake_case identifier also contributes its parts, so
// "loadManifest" finds "load_manifest" and "manifest".
func ExploreTerms(question string, max int) []string {
	if max <= 0 {
		max = 8
	}
	seen := map[string]bool{}
	var primary, parts []string
	add := func(list *[]string, t string) {
		t = strings.ToLower(strings.Trim(t, ".-_"))
		if len(t) < 3 || stopwords[t] || seen[t] {
			return
		}
		seen[t] = true
		*list = append(*list, t)
	}
	for _, tok := range termRe.FindAllString(question, -1) {
		add(&primary, tok)
		for _, p := range splitIdent(tok) {
			add(&parts, p)
		}
	}
	sort.SliceStable(primary, func(i, j int) bool { return len(primary[i]) > len(primary[j]) })
	out := append(primary, parts...)
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// splitIdent splits camelCase, snake_case, kebab-case and dotted names.
func splitIdent(s string) []string {
	var out []string
	for _, chunk := range strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' || r == '.' }) {
		start := 0
		rs := []rune(chunk)
		for i := 1; i < len(rs); i++ {
			if unicode.IsUpper(rs[i]) && (unicode.IsLower(rs[i-1]) || (i+1 < len(rs) && unicode.IsLower(rs[i+1]))) {
				out = append(out, string(rs[start:i]))
				start = i
			}
		}
		out = append(out, string(rs[start:]))
	}
	if len(out) == 1 {
		return nil // nothing new beyond the token itself
	}
	return out
}

// BuildSpans groups hits into SpanLines-sized windows per file, merges
// windows that overlap, scores each by distinct terms and hit count, and
// returns the best `limit` spans in keyword order. lineCount reports how
// many lines a file has (0 when unknown) so a window never runs past EOF.
func BuildSpans(hits []Hit, terms []string, limit int, lineCount func(path string) int) []Span {
	if limit <= 0 {
		limit = 30
	}
	byFile := map[string][]Hit{}
	for _, h := range hits {
		byFile[h.Path] = append(byFile[h.Path], h)
	}
	var spans []Span
	half := SpanLines / 2
	for p, hs := range byFile {
		sort.Slice(hs, func(i, j int) bool { return hs[i].Line < hs[j].Line })
		n := 0
		if lineCount != nil {
			n = lineCount(p)
		}
		var cur *Span
		termSet := map[string]bool{}
		flush := func() {
			if cur == nil {
				return
			}
			for t := range termSet {
				cur.Terms = append(cur.Terms, t)
			}
			sort.Strings(cur.Terms)
			cur.KeywordScore = keywordScore(cur, terms)
			spans = append(spans, *cur)
			cur, termSet = nil, map[string]bool{}
		}
		for _, h := range hs {
			start, end := h.Line-half, h.Line+half-1
			if start < 1 {
				end += 1 - start
				start = 1
			}
			if n > 0 && end > n {
				end = n
			}
			if cur != nil && start <= cur.End+1 && h.Line-cur.Start < maxSpanLines {
				// Overlapping window: extend, but never past maxSpanLines,
				// so a file dense with hits yields several spans rather than
				// one span covering the whole file.
				if end > cur.Start+maxSpanLines-1 {
					end = cur.Start + maxSpanLines - 1
				}
				if end > cur.End {
					cur.End = end
				}
			} else {
				if cur != nil && start <= cur.End {
					start = cur.End + 1 // keep spans disjoint
				}
				flush()
				cur = &Span{Path: p, Start: start, End: end}
			}
			cur.Hits++
			low := strings.ToLower(h.Text)
			for _, t := range terms {
				if strings.Contains(low, t) {
					termSet[t] = true
				}
			}
		}
		flush()
	}
	sortKeyword(spans)
	if len(spans) > limit {
		spans = spans[:limit]
	}
	for i := range spans {
		spans[i].ID = fmt.Sprintf("s%d", i+1)
	}
	return spans
}

// keywordScore: distinct terms dominate, then hit density, then a small
// bonus for terms in the path (a file named after the concept).
func keywordScore(s *Span, terms []string) float64 {
	hits := s.Hits
	if hits > 10 {
		hits = 10
	}
	score := float64(len(s.Terms))*3 + float64(hits)*0.5
	base := strings.ToLower(path.Base(s.Path))
	for _, t := range terms {
		if strings.Contains(base, t) {
			score += 2
		}
	}
	// "Where is X" usually means the implementation: tests rank just
	// below an implementation span with the same hits.
	if isTestPath(s.Path) {
		score -= 1
	}
	return score
}

func isTestPath(p string) bool {
	low := strings.ToLower(p)
	base := path.Base(low)
	return strings.Contains(base, "_test.") || strings.HasPrefix(base, "test_") || strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.") || strings.HasPrefix(low, "tests/") || strings.Contains(low, "/tests/")
}

func sortKeyword(spans []Span) {
	sort.SliceStable(spans, func(i, j int) bool {
		a, b := spans[i], spans[j]
		if a.KeywordScore != b.KeywordScore {
			return a.KeywordScore > b.KeywordScore
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Start < b.Start
	})
}

// sortScore orders by Score, then keyword order.
func sortScore(spans []Span) {
	sort.SliceStable(spans, func(i, j int) bool {
		a, b := spans[i], spans[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.KeywordScore != b.KeywordScore {
			return a.KeywordScore > b.KeywordScore
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Start < b.Start
	})
}

// ExploreResult is the ranked outcome.
type ExploreResult struct {
	Question string   `json:"question"`
	Terms    []string `json:"terms"`
	// Mode is "jev" when Jev ranked the spans, "keyword" otherwise.
	Mode       string `json:"mode"`
	Fallback   string `json:"fallback,omitempty"`
	Candidates int    `json:"candidates"`
	Batches    int    `json:"batches,omitempty"`
	// SkippedSubtrees counts subtrees the keyword stage left out because an
	// ignore file governing them could not be parsed (fail closed).
	SkippedSubtrees int    `json:"skipped_subtrees,omitempty"`
	Results         []Span `json:"results"`
}

func exploreQuestion(id, question string) Question {
	return Noul(fmt.Sprintf("Is the code span data.spans.%s relevant to answering this question: %s", id, question))
}

type spanState struct {
	Path  string `json:"path"`
	Lines string `json:"lines"`
	Text  string `json:"text"`
}

func clipSpan(text string) string {
	if len(text) <= maxSpanText {
		return text
	}
	cut := maxSpanText
	for cut > 0 && !utf8Start(text[cut]) {
		cut--
	}
	return text[:cut] + "\n[span truncated]"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// ExploreBatches packs spans into groups whose request stays under the
// size cap. Each span costs its text, its metadata and its question.
func ExploreBatches(question string, spans []Span) [][]Span {
	base := len(question) + len(untrustedNote) + 200
	var out [][]Span
	var cur []Span
	size := base
	for _, s := range spans {
		st, _ := json.Marshal(spanState{Path: s.Path, Lines: "000000-000000", Text: clipSpan(s.Text)})
		q, _ := json.Marshal(exploreQuestion(s.ID, question))
		cost := len(st) + len(q) + 2*len(s.ID) + 16
		if len(cur) > 0 && size+cost > exploreBatchBudget {
			out = append(out, cur)
			cur, size = nil, base
		}
		cur = append(cur, s)
		size += cost
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// exploreParallel caps concurrent Jev requests for one explore.
const exploreParallel = 3

// RankSpans asks Jev about every span (batched) and returns them ordered by
// relevance. On any failure it returns the keyword ranking with Mode
// "keyword" and the reason in Fallback: scores from a partial set of
// batches would not be comparable with keyword scores.
func RankSpans(ctx context.Context, c *Client, question string, terms []string, spans []Span, top int) ExploreResult {
	res := ExploreResult{Question: question, Terms: terms, Candidates: len(spans), Mode: "keyword"}
	if top <= 0 {
		top = 5
	}
	finish := func() ExploreResult {
		if len(res.Results) > top {
			res.Results = res.Results[:top]
		}
		if res.Results == nil {
			res.Results = []Span{}
		}
		return res
	}
	keyword := func(reason string) ExploreResult {
		out := append([]Span(nil), spans...)
		sortKeyword(out)
		max := 0.0
		for _, s := range out {
			if s.KeywordScore > max {
				max = s.KeywordScore
			}
		}
		for i := range out {
			if max > 0 {
				out[i].Score = out[i].KeywordScore / max
			}
		}
		res.Mode, res.Fallback, res.Results = "keyword", reason, out
		return finish()
	}
	if len(spans) == 0 {
		return finish()
	}
	if c == nil {
		return keyword("jev unavailable")
	}
	batches := ExploreBatches(question, spans)
	res.Batches = len(batches)
	probs := make([]map[string]float64, len(batches))
	errs := make([]error, len(batches))
	sem := make(chan struct{}, exploreParallel)
	var wg sync.WaitGroup
	for i, b := range batches {
		wg.Add(1)
		go func(i int, b []Span) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			state := map[string]any{"question": question}
			ss := map[string]spanState{}
			qs := map[string]Question{}
			for _, s := range b {
				ss[s.ID] = spanState{Path: s.Path, Lines: fmt.Sprintf("%d-%d", s.Start, s.End), Text: clipSpan(s.Text)}
				qs[s.ID] = exploreQuestion(s.ID, question)
			}
			state["spans"] = ss
			resp, err := c.Ask(ctx, state, qs)
			if err != nil {
				errs[i] = err
				return
			}
			m := map[string]float64{}
			for _, s := range b {
				p, err := resp.NoulOf(s.ID)
				if err != nil {
					errs[i] = err
					return
				}
				m[s.ID] = p
			}
			probs[i] = m
		}(i, b)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return keyword("jev unavailable: " + err.Error())
		}
	}
	out := append([]Span(nil), spans...)
	for i := range out {
		for _, m := range probs {
			if p, ok := m[out[i].ID]; ok {
				out[i].Score = p
			}
		}
	}
	sortScore(out)
	res.Mode, res.Results = "jev", out
	return finish()
}
