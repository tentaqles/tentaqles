package providers

import (
	"slices"
	"sort"
	"testing"
)

// GenericTool is the catalog's structural rule: a provider whose CLI
// command its match lists do not name. It is exactly the generic tools the catalog test lists.
func TestGenericTool(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range c.All() {
		if p.GenericTool() {
			got = append(got, p.ID)
		}
	}
	sort.Strings(got)
	want := slices.Clone(genericTools)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("generic tools = %v, want %v", got, want)
	}
}

func TestAmbiguousExecutable(t *testing.T) {
	for _, n := range []string{"cn", "tea", "snow", "render", "vc", "fly", "atlas", "copilot", "goose", "Render.EXE"} {
		if !AmbiguousExecutable(n) {
			t.Errorf("%s is not ambiguous", n)
		}
	}
	for _, n := range []string{"aws", "az", "gcloud", "flyctl", "vercel", "snowsql", "gh", "aws_completer"} {
		if AmbiguousExecutable(n) {
			t.Errorf("%s is ambiguous", n)
		}
	}
}
