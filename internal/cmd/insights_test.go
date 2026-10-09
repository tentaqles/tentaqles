package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"7d":         now.Add(-7 * 24 * time.Hour),
		"30D":        now.Add(-30 * 24 * time.Hour),
		"12h":        now.Add(-12 * time.Hour),
		"2w":         now.Add(-14 * 24 * time.Hour),
		"2026-10-01": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
	for in, want := range cases {
		got, err := parseSince(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseSince(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "yesterday", "7", "2027-01-01", "-3d"} {
		if _, err := parseSince(bad, now); err == nil {
			t.Errorf("parseSince(%q) accepted", bad)
		}
	}
}

func TestInsightsCmdJSON(t *testing.T) {
	home := isolateHome(t)
	proj := filepath.Join(home, ".tentaqles", "identities", "acme", "claude", "projects", "C--work")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	lines := `{"type":"user","timestamp":"` + ts + `","message":{"role":"user","content":"hello there"}}` + "\n" +
		`{"type":"assistant","timestamp":"` + ts + `","message":{"id":"m1","model":"claude-sonnet-5-5","usage":{"input_tokens":1000000},"content":[]}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"insights", "--since", "7d", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Schema string `json:"schema"`
		Total  struct {
			Sessions int     `json:"sessions"`
			CostUSD  float64 `json:"cost_usd"`
		} `json:"total"`
		Worktrees *struct {
			Total int `json:"total"`
		} `json:"worktrees"`
	}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out.String())
	}
	if rep.Schema != "tq.insights/v1" || rep.Total.Sessions != 1 || rep.Total.CostUSD != 2 {
		t.Errorf("report = %+v", rep)
	}
	if rep.Worktrees == nil || rep.Worktrees.Total != 0 {
		t.Errorf("worktrees = %+v", rep.Worktrees)
	}
	if strings.Contains(out.String(), "hello there") {
		t.Error("JSON leaks prompt text")
	}

	out.Reset()
	root = NewRoot()
	root.SetOut(&out)
	root.SetArgs([]string{"insights", "--ws", "nobody", "--no-worktrees"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "30-day targets") || !strings.Contains(out.String(), "(not scanned)") {
		t.Errorf("text output:\n%s", out.String())
	}
}
