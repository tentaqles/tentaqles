package decide

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestExploreTerms(t *testing.T) {
	cases := []struct {
		q    string
		want []string
	}{
		{"where does the breaker trip?", []string{"breaker", "trip"}},
		{"How is loadManifest called", []string{"loadmanifest", "called", "load", "manifest"}},
		{"what writes judgments.jsonl", []string{"judgments.jsonl", "writes", "judgments", "jsonl"}},
		{"is it the of to", nil},
	}
	for _, c := range cases {
		got := ExploreTerms(c.q, 8)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ExploreTerms(%q) = %v, want %v", c.q, got, c.want)
		}
	}
	if got := ExploreTerms("alpha bravo charlie delta echo foxtrot", 3); len(got) != 3 {
		t.Errorf("cap ignored: %v", got)
	}
}

func TestBuildSpans(t *testing.T) {
	terms := []string{"breaker", "trip"}
	lines := func(string) int { return 500 }
	cases := []struct {
		name  string
		hits  []Hit
		limit int
		want  [][3]any // path, start, end in keyword order
	}{
		{
			name: "one hit near the top is clamped to line 1",
			hits: []Hit{{"a.go", 3, "breaker"}},
			want: [][3]any{{"a.go", 1, 40}},
		},
		{
			name: "overlapping hits merge into one span",
			hits: []Hit{{"a.go", 100, "breaker"}, {"a.go", 110, "trip"}},
			want: [][3]any{{"a.go", 80, 129}},
		},
		{
			name: "a dense file splits at maxSpanLines and spans stay disjoint",
			hits: []Hit{{"a.go", 100, "breaker"}, {"a.go", 130, "breaker"}, {"a.go", 150, "breaker"}},
			want: [][3]any{{"a.go", 80, 139}, {"a.go", 140, 169}},
		},
		{
			name: "more distinct terms rank first; path breaks ties",
			hits: []Hit{{"z.go", 50, "breaker trip"}, {"b.go", 50, "breaker"}, {"a.go", 50, "breaker"}},
			want: [][3]any{{"z.go", 30, 69}, {"a.go", 30, 69}, {"b.go", 30, 69}},
		},
		{
			name:  "capped at limit",
			hits:  []Hit{{"a.go", 50, "breaker"}, {"b.go", 50, "breaker"}, {"c.go", 50, "breaker"}},
			limit: 2,
			want:  [][3]any{{"a.go", 30, 69}, {"b.go", 30, 69}},
		},
		{
			name: "tests rank below implementation with the same hits",
			hits: []Hit{{"a_test.go", 50, "breaker"}, {"b.go", 50, "breaker"}},
			want: [][3]any{{"b.go", 30, 69}, {"a_test.go", 30, 69}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := BuildSpans(c.hits, terms, c.limit, lines)
			var g [][3]any
			for i, s := range got {
				g = append(g, [3]any{s.Path, s.Start, s.End})
				if s.ID != "s"+string(rune('1'+i)) {
					t.Errorf("span %d id = %s", i, s.ID)
				}
			}
			if !reflect.DeepEqual(g, c.want) {
				t.Fatalf("got %v, want %v", g, c.want)
			}
		})
	}
	// A short file clamps the window at EOF.
	got := BuildSpans([]Hit{{"short.go", 5, "trip"}}, terms, 0, func(string) int { return 12 })
	if got[0].End != 12 {
		t.Fatalf("end = %d, want 12", got[0].End)
	}
}

func bigSpans(n, textLen int) []Span {
	out := make([]Span, n)
	for i := range out {
		out[i] = Span{ID: "s" + itoa(i+1), Path: "f" + itoa(i) + ".go", Start: 1, End: 40,
			KeywordScore: float64(n - i), Text: strings.Repeat("x", textLen)}
	}
	return out
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func TestExploreBatchesStayUnderCap(t *testing.T) {
	spans := bigSpans(30, 3900) // ~120 KB of span text: must split
	batches := ExploreBatches("where is the breaker?", spans)
	if len(batches) < 2 {
		t.Fatalf("want several batches, got %d", len(batches))
	}
	seen := 0
	for _, b := range batches {
		seen += len(b)
	}
	if seen != 30 {
		t.Fatalf("batches hold %d spans, want 30", seen)
	}
	if got := ExploreBatches("q", bigSpans(5, 200)); len(got) != 1 {
		t.Fatalf("small set split into %d batches", len(got))
	}
}

// spanPath reads the span's path out of a framed request.
func spanPaths(body map[string]any) map[string]string {
	out := map[string]string{}
	st, _ := body["state"].(map[string]any)
	data, _ := st["data"].(map[string]any)
	spans, _ := data["spans"].(map[string]any)
	for id, v := range spans {
		m, _ := v.(map[string]any)
		out[id], _ = m["path"].(string)
	}
	return out
}

func TestRankSpansBatchesAndOrders(t *testing.T) {
	// Jev likes later files: p rises with the file number, the reverse of
	// the keyword order, so the output proves Jev's ranking won.
	f := newFake(t, func(w http.ResponseWriter, body map[string]any) {
		ans := map[string]any{}
		for id, p := range spanPaths(body) {
			var n int
			_ = json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(p, "f"), ".go")), &n)
			ans[id] = map[string]any{"noul": float64(n) / 100}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": ans})
	})
	spans := bigSpans(30, 3900)
	res := RankSpans(context.Background(), client(f), "where is the breaker?", nil, spans, 5)
	if res.Mode != "jev" || res.Fallback != "" {
		t.Fatalf("mode=%s fallback=%q", res.Mode, res.Fallback)
	}
	if int(f.calls.Load()) != res.Batches || res.Batches < 2 {
		t.Fatalf("calls=%d batches=%d", f.calls.Load(), res.Batches)
	}
	var got []string
	for _, s := range res.Results {
		got = append(got, s.Path)
	}
	want := []string{"f29.go", "f28.go", "f27.go", "f26.go", "f25.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for _, raw := range []string{f.last.Load().(string)} {
		if len(raw) > MaxStateBytes {
			t.Fatalf("request of %d bytes exceeds the cap", len(raw))
		}
	}
}

func TestRankSpansFallsBackToKeywords(t *testing.T) {
	spans := []Span{
		{ID: "s1", Path: "a.go", KeywordScore: 4},
		{ID: "s2", Path: "b.go", KeywordScore: 8},
		{ID: "s3", Path: "c.go", KeywordScore: 2},
	}
	down := newFake(t, func(w http.ResponseWriter, _ map[string]any) { w.WriteHeader(503) })
	partial := newFake(t, func(w http.ResponseWriter, _ map[string]any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"s1": map[string]any{"noul": 0.9}}})
	})
	for name, cl := range map[string]*Client{"nil client": nil, "server down": client(down), "missing answer": client(partial)} {
		t.Run(name, func(t *testing.T) {
			res := RankSpans(context.Background(), cl, "q", nil, spans, 2)
			if res.Mode != "keyword" || res.Fallback == "" {
				t.Fatalf("mode=%s fallback=%q", res.Mode, res.Fallback)
			}
			if len(res.Results) != 2 || res.Results[0].Path != "b.go" || res.Results[1].Path != "a.go" {
				t.Fatalf("keyword order wrong: %+v", res.Results)
			}
			if res.Results[0].Score != 1 || res.Results[1].Score != 0.5 {
				t.Fatalf("scores not normalized: %v %v", res.Results[0].Score, res.Results[1].Score)
			}
		})
	}
}
