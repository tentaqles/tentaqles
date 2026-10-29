package ml

import (
	"context"
	"reflect"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

func codeInput(t *testing.T, files map[string]string) *packs.Input {
	t.Helper()
	root := testutil.TempDir(t)
	writeFiles(t, root, files)
	in := mlInput(t, root, guardConfig, "")
	in.Base = "HEAD"
	for p := range files {
		in.Changed = append(in.Changed, packs.ChangedFile{Path: p, Status: 'M'})
	}
	return in.ForPack("ml")
}

func TestPromotionInCodeGolden(t *testing.T) {
	cases := []struct {
		file          string
		errors, warns []string
	}{
		{"promote-code/positive.py", []string{"promotion-in-code", "promotion-in-code", "promotion-in-code", "promotion-in-code"}, []string{"promotion-in-code"}},
		{"promote-code/negative.py", []string{}, []string{}},
	}
	for _, c := range cases {
		in := codeInput(t, map[string]string{"src/deploy/register.py": readFixture(t, c.file)})
		if !promotionInCodeApplies(in) {
			t.Fatalf("%s: applies", c.file)
		}
		r := runPromotionInCode(context.Background(), in)
		if got := rules(r.Findings, packs.LevelError); !reflect.DeepEqual(got, c.errors) {
			t.Fatalf("%s errors %v (%+v)", c.file, got, r.Findings)
		}
		if got := rules(r.Findings, packs.LevelWarn); !reflect.DeepEqual(got, c.warns) {
			t.Fatalf("%s warns %v (%+v)", c.file, got, r.Findings)
		}
	}
}

func TestPromotionInCodeLines(t *testing.T) {
	in := codeInput(t, map[string]string{"src/deploy/register.py": readFixture(t, "promote-code/positive.py")})
	r := runPromotionInCode(context.Background(), in)
	var lines []int
	for _, f := range r.Findings {
		lines = append(lines, f.Line)
	}
	if !reflect.DeepEqual(lines, []int{5, 6, 7, 8, 13}) {
		t.Fatalf("lines %v", lines)
	}
}

func TestPromotionInCodeExemptAndTests(t *testing.T) {
	src := readFixture(t, "promote-code/positive.py")
	in := codeInput(t, map[string]string{"scripts/promote.py": src})
	if r := runPromotionInCode(context.Background(), in); len(r.Findings) != 0 {
		t.Fatalf("the declared promotion script is exempt: %+v", r.Findings)
	}
	in = codeInput(t, map[string]string{"tests/test_register.py": src})
	r := runPromotionInCode(context.Background(), in)
	if len(rules(r.Findings, packs.LevelError)) != 0 || len(rules(r.Findings, packs.LevelWarn)) == 0 {
		t.Fatalf("tests only warn: %+v", r.Findings)
	}
}

func TestPromotionInCodeNotebook(t *testing.T) {
	in := codeInput(t, map[string]string{"notebooks/deploy.ipynb": readFixture(t, "promote-code/deploy.ipynb")})
	r := runPromotionInCode(context.Background(), in)
	if got := rules(r.Findings, packs.LevelError); !reflect.DeepEqual(got, []string{"promotion-in-code"}) {
		t.Fatalf("%+v", r.Findings)
	}
}
