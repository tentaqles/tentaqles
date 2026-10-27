package decide

import (
	"context"
	"strings"
	"testing"
)

func TestTriage_DocsPlusPlusLineIsScanned(t *testing.T) {
	c := client(secretsFlagged(t))
	diff := docDiff("docs/setup.md", "++AKIA"+strings.Repeat("ABCD1234", 2))
	if r := Triage(context.Background(), c, Policy{Backend: "typesafe"}, diff); r.Risk != "high" || strings.Join(r.Flags, ",") != "secrets" {
		t.Fatalf("++ line must be scanned: %+v", r)
	}
}

func TestTriage_PathWithSpaceBSlashIsNotDocs(t *testing.T) {
	c := client(secretsFlagged(t))
	pol := Policy{Backend: "typesafe"}
	code := "diff --git a/x b/y.md b/x b/y.go\n--- a/x b/y.md\n+++ b/x b/y.go\n@@ -1 +1,2 @@\n+var token string\n"
	if r := Triage(context.Background(), c, pol, code); r.Risk != "high" {
		t.Fatalf("code path containing ' b/' must not be suppressed: %+v", r)
	}
	dir := "diff --git a/x b/dir/f.go b/x b/dir/f.go\n--- a/x b/dir/f.go\n+++ b/x b/dir/f.go\n@@ -1 +1,2 @@\n+var token string\n"
	if r := Triage(context.Background(), c, pol, dir); r.Risk != "high" {
		t.Fatalf("dir 'x b' code file must not be suppressed: %+v", r)
	}
}

func TestTriage_QuotedOrMissingHeaderIsNotDocs(t *testing.T) {
	c := client(secretsFlagged(t))
	pol := Policy{Backend: "typesafe"}
	quoted := "diff --git \"a/d\\303\\251.md\" \"b/d\\303\\251.md\"\n--- \"a/d\\303\\251.md\"\n+++ \"b/d\\303\\251.md\"\n@@ -1 +1,2 @@\n+secret scanning\n"
	if r := Triage(context.Background(), c, pol, quoted); r.Risk != "high" {
		t.Fatalf("quoted path must not count as docs: %+v", r)
	}
	missing := "diff --git a/a.md b/a.md\nold mode 100644\nnew mode 100755\n"
	if r := Triage(context.Background(), c, pol, missing); r.Risk != "high" {
		t.Fatalf("missing header must not count as docs: %+v", r)
	}
}
