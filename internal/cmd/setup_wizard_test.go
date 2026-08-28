package cmd

import (
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/providers"
)

func TestBuildProviderOptions_GroupsAndLabels(t *testing.T) {
	cat := providers.MustLoad()
	opts := buildProviderOptions(cat)
	if len(opts) != len(cat.All()) {
		t.Fatalf("expected %d options, got %d", len(cat.All()), len(opts))
	}

	// Options must be sorted by category then name, and labeled
	// "<Name> (<category>)" with the provider id as the value.
	lastCategory := ""
	lastName := ""
	for _, o := range opts {
		p, ok := cat.Get(o.Value)
		if !ok {
			t.Fatalf("option value %q is not a known provider id", o.Value)
		}
		wantLabel := p.Name + " (" + p.Category + ")"
		if o.Key != wantLabel {
			t.Errorf("option label = %q, want %q", o.Key, wantLabel)
		}
		if p.Category < lastCategory {
			t.Errorf("categories out of order: %q came after %q", p.Category, lastCategory)
		}
		if p.Category == lastCategory && p.Name < lastName {
			t.Errorf("names out of order within category %q: %q came after %q", p.Category, p.Name, lastName)
		}
		lastCategory = p.Category
		lastName = p.Name
	}
}

func TestPlanFromAnswers(t *testing.T) {
	a := wizardAnswers{
		Base: "C:/repos",
		Companies: []companyAnswers{
			{
				Name:           "acme",
				DisplayName:    "Acme Corp",
				Color:          "blue",
				GitName:        "Jane Doe",
				GitEmail:       "jane@acme.com",
				GitUser:        "janedoe",
				Identities:     []string{"claude", "gh"},
				PermissionMode: "acceptEdits",
			},
		},
		Hooks: []string{"bash", "pwsh"},
	}

	plan := planFromAnswers(a)

	if plan.Base != "C:/repos" {
		t.Errorf("Base = %q, want %q", plan.Base, "C:/repos")
	}
	if !plan.Trust {
		t.Error("expected Trust to default to true")
	}
	if len(plan.Hooks) != 2 || plan.Hooks[0] != "bash" || plan.Hooks[1] != "pwsh" {
		t.Errorf("Hooks = %v, want [bash pwsh]", plan.Hooks)
	}
	if len(plan.Companies) != 1 {
		t.Fatalf("expected 1 company, got %d", len(plan.Companies))
	}
	c := plan.Companies[0]
	if c.Name != "acme" || c.DisplayName != "Acme Corp" || c.Color != "blue" ||
		c.GitName != "Jane Doe" || c.GitEmail != "jane@acme.com" || c.GitUser != "janedoe" ||
		c.PermissionMode != "acceptEdits" {
		t.Errorf("unexpected company fields: %+v", c)
	}
	if len(c.Identities) != 2 || c.Identities[0] != "claude" || c.Identities[1] != "gh" {
		t.Errorf("Identities = %v, want [claude gh]", c.Identities)
	}
}

func TestPlanFromAnswers_NoCompanies(t *testing.T) {
	plan := planFromAnswers(wizardAnswers{Base: "~/work"})
	if plan.Base != "~/work" {
		t.Errorf("Base = %q", plan.Base)
	}
	if len(plan.Companies) != 0 {
		t.Errorf("expected no companies, got %d", len(plan.Companies))
	}
}
