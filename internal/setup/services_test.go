package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

func servicesCompany() Company {
	return Company{
		Name: "acme", GitName: "tester", GitEmail: "tester@acme.test", Identities: []string{"claude"}, PermissionMode: "default",
		Tracker: &manifest.Tracker{Provider: "jira", Scope: map[string]string{"org": "acme", "project": "PLAT"}, Auth: "keychain"},
		Chat:    &manifest.Service{Provider: "slack"},
		Email:   &manifest.Service{Provider: "none"},
	}
}

func TestValidate_RejectsBadServiceBlocks(t *testing.T) {
	cat := testCatalog(t)
	cases := map[string]func(c *Company){
		"unknown tracker": func(c *Company) { c.Tracker.Provider = "trello" },
		"env without var": func(c *Company) { c.Tracker.Auth = "env" },
		"unknown chat":    func(c *Company) { c.Chat.Provider = "irc" },
		"unknown email":   func(c *Company) { c.Email.Provider = "pigeon" },
	}
	for name, mutate := range cases {
		c := servicesCompany()
		tr := *c.Tracker
		c.Tracker = &tr
		ch, em := *c.Chat, *c.Email
		c.Chat, c.Email = &ch, &em
		mutate(&c)
		p := &SetupPlan{Base: "x", Companies: []Company{c}}
		err := p.Validate(cat)
		if err == nil {
			t.Errorf("%s: expected an error", name)
			continue
		}
		if !strings.Contains(err.Error(), `company "acme"`) {
			t.Errorf("%s: error should name the company: %v", name, err)
		}
	}
	ok := &SetupPlan{Base: "x", Companies: []Company{servicesCompany()}}
	if err := ok.Validate(cat); err != nil {
		t.Fatalf("valid services rejected: %v", err)
	}
}

// A token pasted where a name belongs (the env variable, a scope value, a lane) is refused
// before anything is written, and the error names the field without echoing the value.
func TestValidate_RejectsTokenShapedServiceValues(t *testing.T) {
	cat := testCatalog(t)
	pat := "gh" + "p_" + strings.Repeat("a1B2", 9) // a GitHub PAT shape, built so no scanner trips on the source
	cases := map[string]struct {
		mutate func(c *Company)
		field  string
	}{
		"env":         {func(c *Company) { c.Tracker.Auth, c.Tracker.Env = "env", pat }, "tracker.env"},
		"scope value": {func(c *Company) { c.Tracker.Scope = map[string]string{"org": "Bearer abc", "project": "PLAT"} }, "tracker.scope.org"},
		"scope key":   {func(c *Company) { c.Tracker.Scope = map[string]string{"org": "acme", "project": "PLAT", pat: "x"} }, "tracker.scope"},
		"lane value":  {func(c *Company) { c.Tracker.Lanes = map[string]string{"Doing": pat} }, "tracker.lanes.Doing"},
		"lane key":    {func(c *Company) { c.Tracker.Lanes = map[string]string{pat: "active"} }, "tracker.lanes"},
		"assigned":    {func(c *Company) { c.Tracker.AssignedTo = pat }, "tracker.assigned_to"},
	}
	for name, tc := range cases {
		c := servicesCompany()
		tr := *c.Tracker
		c.Tracker = &tr
		tc.mutate(&c)
		err := (&SetupPlan{Base: "x", Companies: []Company{c}}).Validate(cat)
		if err == nil {
			t.Errorf("%s: expected an error", name)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, tc.field) || !strings.Contains(msg, "secret") {
			t.Errorf("%s: error should name %s and say it looks like a secret: %v", name, tc.field, msg)
		}
		if strings.Contains(msg, pat) || strings.Contains(msg, "Bearer abc") {
			t.Errorf("%s: error echoes the value: %v", name, msg)
		}
	}
}

func TestPreview_ReportsServices(t *testing.T) {
	isolateHome(t)
	base := testutil.TempDir(t)
	p := &SetupPlan{Base: base, Companies: []Company{servicesCompany(), {Name: "plain", GitName: "tester", GitEmail: "tester@acme.test"}}}
	changes, err := Preview(p, tempProfiles(t))
	if err != nil {
		t.Fatal(err)
	}
	var got []Change
	for _, c := range changes {
		if c.Kind == "services" {
			got = append(got, c)
		}
	}
	if len(got) != 1 || got[0].Target != "acme" {
		t.Fatalf("want one services change for acme, got %+v", got)
	}
	for _, want := range []string{"tracker jira", "chat slack", "email none"} {
		if !strings.Contains(got[0].Detail, want) {
			t.Errorf("detail %q is missing %q", got[0].Detail, want)
		}
	}
}

func TestPreview_ExistingWorkspaceSaysServicesAreNotAdded(t *testing.T) {
	isolateHome(t)
	base := testutil.TempDir(t)
	if err := os.MkdirAll(filepath.Join(base, "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "acme", manifest.FileName), []byte("name: acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := Preview(&SetupPlan{Base: base, Companies: []Company{servicesCompany()}}, tempProfiles(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Kind == "workspace-skip" && c.Target == "acme" {
			if !strings.Contains(c.Detail, "services") {
				t.Fatalf("skip detail should say the services are not added: %q", c.Detail)
			}
			return
		}
	}
	t.Fatalf("no workspace-skip for acme: %+v", changes)
}

func TestApply_WritesServiceBlocks(t *testing.T) {
	isolateHome(t)
	cat := testCatalog(t)
	base := testutil.TempDir(t)
	p := &SetupPlan{Base: base, Trust: true, Companies: []Company{servicesCompany()}}
	report, err := Apply(p, cat, ApplyOptions{RunGit: fakeGit, Profiles: tempProfiles(t)})
	if err != nil {
		t.Fatalf("apply: %v; warnings=%v", err, report.Warnings)
	}
	m, err := manifest.Load(filepath.Join(base, "acme", manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if m.Tracker == nil || m.Tracker.Provider != "jira" || m.Tracker.Org() != "acme" || m.Tracker.Project() != "PLAT" || m.Tracker.Auth != "keychain" {
		t.Fatalf("tracker block: %+v", m.Tracker)
	}
	if m.Chat == nil || m.Chat.Provider != "slack" || m.Email == nil || m.Email.Provider != "none" {
		t.Fatalf("chat/email blocks: %+v %+v", m.Chat, m.Email)
	}
	found := false
	for _, c := range report.Changes {
		found = found || (c.Kind == "services" && c.Target == "acme")
	}
	if !found {
		t.Fatalf("report has no services change: %+v", report.Changes)
	}
}
