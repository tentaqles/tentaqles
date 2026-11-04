package setupapi

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func servicesPlan(base string) Plan {
	return Plan{
		Base:  base,
		Trust: true,
		Companies: []Company{{
			Name: "acme", GitName: "tester", GitEmail: "tester@acme.test", Identities: []string{"claude"}, PermissionMode: "default",
			Chat:  "teams",
			Email: "",
		}},
	}
}

func TestServiceCatalog_MatchesTheManifestLists(t *testing.T) {
	c := ServiceCatalog()
	if !slices.Contains(c.Chat, "slack") || !slices.Contains(c.Email, "gmail") {
		t.Fatalf("catalog: %+v", c)
	}
}

func TestValidatePlan_RejectsAnUnknownChat(t *testing.T) {
	isolateHome(t)
	p := servicesPlan("x")
	p.Companies[0].Chat = "irc"
	if err := ValidatePlan(p); err == nil || !strings.Contains(err.Error(), "chat") {
		t.Fatalf("want a chat error, got %v", err)
	}
}

func TestApply_WritesTheServiceBlocks(t *testing.T) {
	home := isolateHome(t)
	SetTestProfiles(tempProfiles(t))
	defer SetTestProfiles(nil)
	prev := RunGit
	RunGit = fakeGit
	defer func() { RunGit = prev }()

	base := filepath.Join(home, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	p := servicesPlan(base)
	changes, err := Preview(p)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(changes, func(c Change) bool { return c.Kind == "services" && strings.Contains(c.Detail, "chat teams") }) {
		t.Fatalf("preview has no services change: %+v", changes)
	}
	if _, err := Apply(p); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(base, "acme", ".tentaqles.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"chat:", "provider: teams"} {
		if !strings.Contains(text, want) {
			t.Errorf("manifest is missing %q:\n%s", want, text)
		}
	}
	// A skipped email block is left out entirely.
	if strings.Contains(text, "\nemail:") {
		t.Errorf("manifest should drop the skipped email:\n%s", text)
	}
}
