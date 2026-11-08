package reach

import (
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

func current() *Answers {
	return &Answers{Email: "me@acme.test", Git: "github", Clouds: []string{"azure"}, Team: []string{"slack"},
		Task: "jira", EmailProvider: "gmail", Browsers: []manifest.Browser{{Name: "edge", Profile: "Default"}}}
}

func TestChange_SingleValueCategories(t *testing.T) {
	cur := current()
	n, drop, err := Change(cur, true, CatGit, []string{"ado"})
	if err != nil || n.Git != "azure-devops" || drop != nil {
		t.Fatalf("add git ado: %+v %v %v", n, drop, err)
	}
	if cur.Git != "github" {
		t.Fatal("Change changed its input")
	}
	if n, _, err := Change(cur, false, CatGit, nil); err != nil || n.Git != "" {
		t.Fatalf("remove git: %+v %v", n, err)
	}
	if _, _, err := Change(cur, false, CatGit, []string{"gitlab"}); err == nil || !strings.Contains(err.Error(), "git is github") {
		t.Fatalf("remove git gitlab: %v", err)
	}
	if n, _, err := Change(cur, true, CatMail, []string{"microsoft"}); err != nil || n.EmailProvider != "outlook" {
		t.Fatalf("add mail microsoft: %+v %v", n, err)
	}
	if n, _, err := Change(cur, true, CatEmail, []string{"dev@acme.test"}); err != nil || n.Email != "dev@acme.test" {
		t.Fatalf("add email: %+v %v", n, err)
	}
	if _, _, err := Change(cur, false, CatEmail, nil); err == nil {
		t.Fatal("removing the email was accepted")
	}
	if _, _, err := Change(cur, true, CatGit, []string{"svn"}); err == nil || !strings.Contains(err.Error(), "github") {
		t.Fatalf("an unknown git host: %v", err)
	}
}

func TestChange_ListCategories(t *testing.T) {
	cur := current()
	n, _, err := Change(cur, true, CatCloud, []string{"aws", "gcloud", "azure"})
	if err != nil || !slices.Equal(n.Clouds, []string{"azure", "aws", "gcp"}) {
		t.Fatalf("add cloud: %v %v", n.Clouds, err)
	}
	// Removing the primary promotes the next cloud.
	n, _, err = Change(n, false, CatCloud, []string{"az"})
	if err != nil || !slices.Equal(n.Clouds, []string{"aws", "gcp"}) {
		t.Fatalf("remove the primary cloud: %v %v", n.Clouds, err)
	}
	if _, _, err := Change(cur, false, CatTeam, []string{"discord"}); err == nil {
		t.Fatal("removing a team tool the client lacks was accepted")
	}
	if _, _, err := Change(cur, true, CatData, nil); err == nil {
		t.Fatal("add data with no value was accepted")
	}
}

func TestChange_BrowserAndOther(t *testing.T) {
	cur := current()
	n, _, err := Change(cur, true, CatBrowser, []string{"chrome", "Profile 2"})
	if err != nil || len(n.Browsers) != 2 || n.Browsers[1] != (manifest.Browser{Name: "chrome", Profile: "Profile 2"}) {
		t.Fatalf("add browser: %+v %v", n.Browsers, err)
	}
	n, _, err = Change(n, true, CatBrowser, []string{"edge", "Work"})
	if err != nil || n.Browsers[0].Profile != "Work" {
		t.Fatalf("change a browser's profile: %+v %v", n.Browsers, err)
	}
	if n, _, err = Change(n, false, CatBrowser, []string{"chrome"}); err != nil || len(n.Browsers) != 1 {
		t.Fatalf("remove browser: %+v %v", n.Browsers, err)
	}
	if n, _, err = Change(cur, true, CatOther, []string{"In-house CRM"}); err != nil || !slices.Equal(n.Other, []string{"In-house CRM"}) {
		t.Fatalf("add other: %v %v", n.Other, err)
	}
	if _, _, err := Change(cur, true, CatBrowser, []string{"chrome", "ghp_" + strings.Repeat("a", 30)}); err == nil {
		t.Fatal("a secret-shaped profile was accepted")
	}
}

// A bare value finds its category; a bare id no answer holds is handed to Edit to drop.
func TestChange_BareValues(t *testing.T) {
	cur := current()
	if n, drop, err := Change(cur, true, "aws", nil); err != nil || !slices.Contains(n.Clouds, "aws") || drop != nil {
		t.Fatalf("add aws: %+v %v %v", n, drop, err)
	}
	if n, drop, err := Change(cur, false, "slack", nil); err != nil || len(n.Team) != 0 || drop != nil {
		t.Fatalf("remove slack: %+v %v %v", n, drop, err)
	}
	if n, drop, err := Change(cur, false, "gh", nil); err != nil || n.Git != "" || drop != nil {
		t.Fatalf("remove gh while github is the git host: %+v %v %v", n, drop, err)
	}
	cur.Git = "azure-devops"
	if n, drop, err := Change(cur, false, "gh", nil); err != nil || n.Git != "azure-devops" || !slices.Equal(drop, []string{"gh"}) {
		t.Fatalf("remove gh no answer holds: %+v %v %v", n, drop, err)
	}
	if _, _, err := Change(cur, true, "fortran", nil); err == nil {
		t.Fatal("an unknown provider was added")
	}
	if _, _, err := Change(cur, true, "bogus", []string{"x"}); err == nil || !strings.Contains(err.Error(), "unknown category") {
		t.Fatalf("an unknown category: %v", err)
	}
}

// A client on Azure DevOps with gh listed by hand: tentaqles remove gh drops it, and the rest of the
// file stays.
const handManifest = `schema: tentaqles-client-v2
client: yduqs
git:
    provider: azure-devops
    email: me@yduqs.example
identities:
    claude: {}
    gh: {}
cloud:
    provider: azure
    expected: DEV-BI # kept by hand
`

func TestEdit_DropsAHandListedIdentity(t *testing.T) {
	before := AnswersFrom(parse(t, []byte(handManifest)))
	next, drop, err := Change(before, false, "gh", nil)
	if err != nil {
		t.Fatal(err)
	}
	out, changes, _, removed, err := Edit([]byte(handManifest), before, next, drop...)
	if err != nil {
		t.Fatal(err)
	}
	m := parse(t, out)
	if slices.Contains(m.Scope(), "gh") {
		t.Fatalf("gh still in scope: %v", m.Scope())
	}
	for _, id := range []string{"claude", "az", "azure-devops"} {
		if !slices.Contains(m.Scope(), id) {
			t.Errorf("%s left the scope: %v", id, m.Scope())
		}
	}
	if !slices.Equal(removed, []string{"gh"}) || !strings.Contains(strings.Join(changes, "|"), "removed gh") {
		t.Errorf("removed %v changes %v", removed, changes)
	}
	if !strings.Contains(string(out), "# kept by hand") || !strings.Contains(string(out), "DEV-BI") {
		t.Errorf("a hand-written line was lost:\n%s", out)
	}
	// Dropping what the client does not hold is an error, and nothing is built.
	if _, _, _, _, err := Edit(out, AnswersFrom(m), AnswersFrom(m), "gh"); err == nil {
		t.Fatal("dropping gh twice was accepted")
	}
}

// Moving the git host from GitHub to Azure DevOps takes gh away and adds azure-devops.
func TestEdit_GitHostChange(t *testing.T) {
	src := strings.Replace(handManifest, "provider: azure-devops", "provider: github", 1)
	before := AnswersFrom(parse(t, []byte(src)))
	next, _, err := Change(before, true, CatGit, []string{"azure-devops"})
	if err != nil {
		t.Fatal(err)
	}
	out, _, _, removed, err := Edit([]byte(src), before, next)
	if err != nil {
		t.Fatal(err)
	}
	if m := parse(t, out); slices.Contains(m.Scope(), "gh") || !slices.Contains(removed, "gh") || !slices.Contains(m.Scope(), "azure-devops") {
		t.Fatalf("scope %v removed %v", m.Scope(), removed)
	}
}