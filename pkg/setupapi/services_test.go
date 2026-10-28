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
			Tracker: &Tracker{Provider: "azure-devops", Scope: map[string]string{"org": "acme", "project": "Platform", "board": ""}, Auth: "cli",
				Lanes: map[string]string{"QA": "review"}},
			Chat:  "teams",
			Email: "",
		}},
	}
}

func TestServiceCatalog_MatchesTheManifestLists(t *testing.T) {
	c := ServiceCatalog()
	for _, want := range []string{"github", "azure-devops", "jira", "linear", "asana"} {
		if !slices.Contains(c.Trackers, want) {
			t.Errorf("trackers missing %s: %v", want, c.Trackers)
		}
	}
	if !slices.Contains(c.Chat, "slack") || !slices.Contains(c.Email, "gmail") || !slices.Contains(c.TrackerAuths, "keychain") ||
		!slices.Contains(c.Assignees, "anyone") {
		t.Fatalf("catalog: %+v", c)
	}
}

func TestValidatePlan_RejectsAnUnknownTracker(t *testing.T) {
	isolateHome(t)
	p := servicesPlan("x")
	p.Companies[0].Tracker.Provider = "trello"
	if err := ValidatePlan(p); err == nil || !strings.Contains(err.Error(), "tracker") {
		t.Fatalf("want a tracker error, got %v", err)
	}
	p = servicesPlan("x")
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
	for _, want := range []string{"tracker:", "provider: azure-devops", "org: acme", "project: Platform", "QA: review", "chat:", "provider: teams"} {
		if !strings.Contains(text, want) {
			t.Errorf("manifest is missing %q:\n%s", want, text)
		}
	}
	// An empty scope value is dropped, and a skipped email block is left out entirely.
	if strings.Contains(text, "board:") || strings.Contains(text, "\nemail:") {
		t.Errorf("manifest should drop the empty board and the skipped email:\n%s", text)
	}
}
