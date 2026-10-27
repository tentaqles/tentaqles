package decide

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// secretsFlagged answers "yes" to the secrets question only.
func secretsFlagged(t *testing.T) *fake {
	return newFake(t, func(w http.ResponseWriter, body map[string]any) {
		ans := map[string]any{}
		for id := range body["questions"].(map[string]any) {
			p := 0.1
			if id == "secrets" {
				p = 0.9
			}
			ans[id] = map[string]any{"noul": p}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": ans})
	})
}

func docDiff(path string, lines ...string) string {
	return "diff --git a/" + path + " b/" + path + "\n--- a/" + path + "\n+++ b/" + path + "\n@@ -1 +1,2 @@\n+" + strings.Join(lines, "\n+") + "\n"
}

func TestTriage_DocsOnlyKeywordIsNotSecrets(t *testing.T) {
	pol := Policy{Backend: "typesafe"}
	c := client(secretsFlagged(t))
	diff := docDiff("docs/prd.md", "Risk: secret scanning is not wired up yet.") +
		docDiff("docs/threat-model.md", "A leaked token is the main threat.") +
		docDiff("README.txt", "token handling")
	if r := Triage(context.Background(), c, pol, diff); r.Risk != "low" || len(r.Flags) != 0 {
		t.Fatalf("docs-only keyword mention must be low: %+v", r)
	}
}

func TestTriage_DocsWithKeyPatternStaysHigh(t *testing.T) {
	pol := Policy{Backend: "typesafe"}
	c := client(secretsFlagged(t))
	key := "AKIA" + strings.Repeat("ABCD1234", 2)
	diff := docDiff("docs/setup.md", "aws_access_key_id = "+key)
	if r := Triage(context.Background(), c, pol, diff); r.Risk != "high" || strings.Join(r.Flags, ",") != "secrets" {
		t.Fatalf("key pattern in docs must stay high (secrets): %+v", r)
	}
}

func TestTriage_CodeKeepsSecretsFlag(t *testing.T) {
	pol := Policy{Backend: "typesafe"}
	c := client(secretsFlagged(t))
	diff := docDiff("internal/x/x.go", "var token string")
	if r := Triage(context.Background(), c, pol, diff); r.Risk != "high" || strings.Join(r.Flags, ",") != "secrets" {
		t.Fatalf("go file must keep secrets flag: %+v", r)
	}
	mixed := docDiff("docs/a.md", "secret scanning") + diff
	if r := Triage(context.Background(), c, pol, mixed); r.Risk != "high" {
		t.Fatalf("mixed diff must keep secrets flag: %+v", r)
	}
}

func TestIsDocPath(t *testing.T) {
	for p, want := range map[string]bool{"a.md": true, "x/b.RST": true, "c.txt": true, "docs/d.yaml": true,
		"docs/sub/e.png": true, "main.go": false, ".github/workflows/ci.yml": false, "mydocs/x.go": false} {
		if got := isDocPath(p); got != want {
			t.Errorf("isDocPath(%q) = %v", p, got)
		}
	}
}
