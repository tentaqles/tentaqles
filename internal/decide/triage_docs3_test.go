package decide

import (
	"context"
	"testing"
)

func triageDocs(t *testing.T, diff string, names []string) TriageResult {
	t.Helper()
	return TriageWith(context.Background(), client(secretsFlagged(t)), Policy{Backend: "typesafe"}, diff, names)
}

func TestTriage_MergeAndUnmergedDiffsAreNotDocsOnly(t *testing.T) {
	base := docDiff("docs/a.md", "secret scanning")
	for name, extra := range map[string]string{
		"cc":       "diff --cc docs/b.md\nindex 1,2..3\n--- a/docs/b.md\n+++ b/docs/b.md\n@@@ -1,1 -1,1 +1,2 @@@\n++secret scanning\n",
		"combined": "diff --combined docs/b.md\n@@@ -1 -1 +1 @@@\n++x\n",
		"unmerged": "* Unmerged path docs/b.md\n",
	} {
		if r := triageDocs(t, base+extra, nil); r.Risk != "high" {
			t.Errorf("%s must not be docs-only: %+v", name, r)
		}
	}
}

func TestTriage_NameListCrossCheck(t *testing.T) {
	diff := docDiff("docs/a.md", "secret scanning")
	if r := triageDocs(t, diff, []string{"docs/a.md"}); r.Risk != "low" {
		t.Fatalf("matching names stay low: %+v", r)
	}
	if r := triageDocs(t, diff, []string{"docs/a.md", "src/x.go"}); r.Risk != "high" {
		t.Fatalf("a name not parsed as a doc must keep the flag: %+v", r)
	}
}

func TestTriage_HighEntropyTokenInDocsStaysHigh(t *testing.T) {
	tok := "Xk9Zq3Lm7Pw2Rt5Yb8NcH1sJd4FgA6uVe0QoIzM"
	if len(tok) < 32 {
		t.Fatal("test token too short")
	}
	if r := triageDocs(t, docDiff("docs/a.md", "the value is "+tok), nil); r.Risk != "high" {
		t.Fatalf("random token in docs must stay high: %+v", r)
	}
}

func TestTriage_ProseAndURLPathStayLow(t *testing.T) {
	for _, line := range []string{
		"This change documents how secret scanning works in the pipeline.",
		"See https://example.com/docs/guides/getting-started-with-the-product-overview-page for details.",
		"A_VERY_LONG_CONSTANT_NAME_THAT_DESCRIBES_THE_SETTING_CLEARLY is documented here.",
	} {
		if r := triageDocs(t, docDiff("docs/a.md", line), nil); r.Risk != "low" {
			t.Errorf("%q must stay low: %+v", line, r)
		}
	}
}

func TestTriage_GitSHAInProseKeepsFlag(t *testing.T) {
	// Deliberate: a 40-char hex string is indistinguishable from a key, so the flag stays.
	sha := "8407cd25b0ed1e9a7f3c4d2b6e8109f5a3c7d4e2"
	if len(sha) != 40 {
		t.Fatal("sha length")
	}
	if r := triageDocs(t, docDiff("docs/a.md", "fixed in commit "+sha), nil); r.Risk != "high" {
		t.Fatalf("hex40 keeps the flag: %+v", r)
	}
}
