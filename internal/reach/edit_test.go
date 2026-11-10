package reach

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

// A manifest as reach writes it, plus what a developer added by hand: a comment, a stack block, an
// identity the form does not own (vercel through scope:), and a cloud key of their own.
const handEdited = `# tentaqles workspace manifest — names only, never secrets
schema: tentaqles-client-v2
client: acme
git:
    name: Me
    email: me@acme.test
    provider: github
identities:
    az: {}
    claude: {}
    gh: {}
    slack: {}
cloud:
    provider: azure
    tenant_hint: acme-prod # kept by hand
# the stack this client runs
stack: [go, postgres]
email:
    provider: gmail
scope: [jira, slack, snowflake, vercel]
browsers:
    - name: edge
      profile: Profile 3
other_providers: [In-house]
`

func parse(t *testing.T, raw []byte) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse(raw, "test")
	if err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	return m
}

func TestAnswersFrom(t *testing.T) {
	a := AnswersFrom(parse(t, []byte(handEdited)))
	if a.Email != "me@acme.test" || a.Git != "github" || !slices.Equal(a.Clouds, []string{"azure"}) ||
		!slices.Equal(a.Team, []string{"slack"}) || !slices.Equal(a.Data, []string{"snowflake"}) || a.Task != "jira" ||
		a.EmailProvider != "gmail" || len(a.Browsers) != 1 || !slices.Equal(a.Other, []string{"In-house"}) {
		t.Fatalf("%+v", a)
	}
}

func TestEdit_NoChangeIsTheSameBytes(t *testing.T) {
	a := AnswersFrom(parse(t, []byte(handEdited)))
	out, changes, added, removed, err := Edit([]byte(handEdited), a, AnswersFrom(parse(t, []byte(handEdited))))
	if err != nil || len(changes) != 0 || len(added) != 0 || len(removed) != 0 || !bytes.Equal(out, []byte(handEdited)) {
		t.Fatalf("err=%v changes=%v added=%v removed=%v", err, changes, added, removed)
	}
}

// Providers added and removed: the scope and the identities follow; what the form does not own stays.
func TestEdit_Providers(t *testing.T) {
	before := AnswersFrom(parse(t, []byte(handEdited)))
	after := AnswersFrom(parse(t, []byte(handEdited)))
	after.Clouds = []string{"azure", "aws"}
	after.Team = []string{"discord"}
	after.Data = nil
	after.Task = "linear"
	out, changes, added, removed, err := Edit([]byte(handEdited), before, after)
	if err != nil {
		t.Fatal(err)
	}
	m := parse(t, out)
	scope := m.Scope()
	for _, id := range []string{"aws", "az", "claude", "discord", "gh", "gmail", "linear", "vercel"} {
		if !slices.Contains(scope, id) {
			t.Errorf("%s not in scope %v", id, scope)
		}
	}
	for _, id := range []string{"slack", "snowflake", "jira"} {
		if slices.Contains(scope, id) {
			t.Errorf("%s still in scope %v", id, scope)
		}
	}
	if !slices.Contains(added, "aws") || !slices.Contains(removed, "slack") {
		t.Errorf("added %v removed %v", added, removed)
	}
	if _, ok := m.Identities["slack"]; ok {
		t.Error("the removed provider's identity is still listed")
	}
	if m.Cloud["provider"] != "azure" || m.Cloud["tenant_hint"] != "acme-prod" {
		t.Errorf("cloud: %v", m.Cloud)
	}
	s := string(out)
	for _, kept := range []string{"# tentaqles workspace manifest", "# the stack this client runs", "# kept by hand", "postgres", "In-house", "Profile 3"} {
		if !strings.Contains(s, kept) {
			t.Errorf("%q was lost:\n%s", kept, s)
		}
	}
	if len(changes) < 3 {
		t.Errorf("changes: %v", changes)
	}
}

func TestEdit_EmailAndBrowsers(t *testing.T) {
	before := AnswersFrom(parse(t, []byte(handEdited)))
	after := AnswersFrom(parse(t, []byte(handEdited)))
	after.Email = "dev@acme.test"
	after.Browsers = []manifest.Browser{{Name: "chrome", Profile: "Work"}}
	out, changes, _, _, err := Edit([]byte(handEdited), before, after)
	if err != nil {
		t.Fatal(err)
	}
	m := parse(t, out)
	if m.Git.Email != "dev@acme.test" || m.Git.Name != "Me" || len(m.Browsers) != 1 || m.Browsers[0] != (manifest.Browser{Name: "chrome", Profile: "Work"}) {
		t.Fatalf("%+v %+v", m.Git, m.Browsers)
	}
	if !slices.Equal(m.Scope(), parse(t, []byte(handEdited)).Scope()) {
		t.Errorf("the scope changed: %v", m.Scope())
	}
	if len(changes) != 2 {
		t.Errorf("changes: %v", changes)
	}
}

// Every cloud removed: the cloud identities go, and the block keeps only the developer's own keys.
func TestEdit_RemoveClouds(t *testing.T) {
	before := AnswersFrom(parse(t, []byte(handEdited)))
	after := AnswersFrom(parse(t, []byte(handEdited)))
	after.Clouds, after.Git, after.EmailProvider, after.Browsers, after.Other = nil, "", "", nil, nil
	out, _, _, removed, err := Edit([]byte(handEdited), before, after)
	if err != nil {
		t.Fatal(err)
	}
	m := parse(t, out)
	for _, id := range []string{"az", "gh", "gmail"} {
		if slices.Contains(m.Scope(), id) {
			t.Errorf("%s still in scope %v", id, m.Scope())
		}
	}
	if !slices.Contains(removed, "az") || !slices.Contains(removed, "gh") {
		t.Errorf("removed %v", removed)
	}
	if _, ok := m.Cloud["provider"]; ok || m.Cloud["tenant_hint"] != "acme-prod" {
		t.Errorf("cloud: %v", m.Cloud)
	}
	if len(m.Browsers) != 0 || len(m.OtherProviders) != 0 || m.Git.Provider != "" {
		t.Errorf("%+v %v %q", m.Browsers, m.OtherProviders, m.Git.Provider)
	}
}

// A bad answer is refused before anything is built.
func TestEdit_RefusesBadAnswers(t *testing.T) {
	before := AnswersFrom(parse(t, []byte(handEdited)))
	after := AnswersFrom(parse(t, []byte(handEdited)))
	after.Email = "not an email"
	if _, _, _, _, err := Edit([]byte(handEdited), before, after); err == nil {
		t.Fatal("a bad email was accepted")
	}
	after.Email = before.Email
	after.Browsers = []manifest.Browser{{Name: "chrome", Profile: "ghp_" + strings.Repeat("a", 30)}}
	if _, _, _, _, err := Edit([]byte(handEdited), before, after); err == nil {
		t.Fatal("a secret-shaped profile was accepted")
	}
}

// A lobe key is not reach's to change: reach.Edit keeps its mapping byte for byte, here while the form
// changes the email and so re-encodes the manifest.
func TestEdit_KeepsTheLobeKey(t *testing.T) {
	const lobeBlock = "lobe:\n    capture: \"off\"\n    model: off\n"
	raw := []byte(handEdited + lobeBlock)
	before := AnswersFrom(parse(t, raw))
	after := AnswersFrom(parse(t, raw))
	after.Email = "new@acme.test"
	out, changes, _, _, err := Edit(raw, before, after)
	if err != nil || len(changes) == 0 {
		t.Fatalf("err=%v changes=%v", err, changes)
	}
	if !strings.HasSuffix(string(out), lobeBlock) {
		t.Fatalf("lobe mapping changed:\n%s", out)
	}
	if m := parse(t, out); m.Lobe.Kind == 0 {
		t.Fatal("the lobe node was lost")
	}
}
