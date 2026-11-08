package reach

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/providers"
)

func TestValidateEmail(t *testing.T) {
	for _, good := range []string{"me@acme.com", "first.last+x@sub.acme.co.uk"} {
		if err := ValidateEmail(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"", "me", "me@", "@acme.com", "me@acme", "Me <me@acme.com>", "a b@acme.com", "me@acme.com\nx"} {
		if ValidateEmail(bad) == nil {
			t.Errorf("%q passed", bad)
		}
	}
}

func TestValidateName(t *testing.T) {
	for _, good := range []string{"In-house tool", "Profile 2", "Default", "acme.tool_v2"} {
		if err := ValidateName(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	secret := "ghp_" + strings.Repeat("a", 30)
	for _, bad := range []string{"", " ", "a;b", "a\nb", "../x", secret, strings.Repeat("x", 65)} {
		if ValidateName(bad) == nil {
			t.Errorf("%q passed", bad)
		}
	}
	if err := ValidateName(secret); !strings.Contains(err.Error(), "secret") {
		t.Errorf("a token gets the reason: %v", err)
	}
}

// Every option of the form must be a value the catalog or the manifest knows, or a client could
// pick something the guard cannot judge.
func TestOptionsAreKnown(t *testing.T) {
	cat := providers.MustLoad()
	for _, group := range [][]Option{TeamOptions, DataOptions, TaskOptions, EmailOptions} {
		for _, o := range group {
			if _, ok := cat.Get(o.Value); !ok {
				t.Errorf("%s (%s) is not in the catalog", o.Label, o.Value)
			}
		}
	}
	for _, o := range GitOptions {
		id, ok := gitCLI[o.Value]
		if !ok {
			t.Errorf("git %s has no CLI id", o.Value)
		}
		if _, ok := cat.Get(id); !ok {
			t.Errorf("git CLI %s is not in the catalog", id)
		}
	}
	for _, o := range CloudOptions {
		id, ok := cloudCLI[o.Value]
		if !ok {
			t.Errorf("cloud %s has no CLI id", o.Value)
		}
		if _, ok := cat.Get(id); !ok {
			t.Errorf("cloud CLI %s is not in the catalog", id)
		}
	}
}

func TestAddOptions(t *testing.T) {
	a := &Answers{
		Email: "me@acme.com", Git: "github", Clouds: []string{"azure", "aws"},
		Team: []string{"slack", "discord"}, Data: []string{"databricks", "colab"}, Task: "jira", EmailProvider: "gmail",
		Browsers: []manifest.Browser{{Name: "chrome", Profile: "Profile 2"}}, Other: []string{"In-house"},
	}
	o, err := a.AddOptions("/base", "acme", "Me")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"aws", "az", "claude", "databricks", "gh"}; !reflect.DeepEqual(o.Identities, want) {
		t.Errorf("identities = %v, want %v", o.Identities, want)
	}
	if want := []string{"colab", "databricks", "discord", "jira", "slack"}; !reflect.DeepEqual(o.ExtraScope, want) {
		t.Errorf("scope = %v, want %v", o.ExtraScope, want)
	}
	if o.Cloud["provider"] != "azure" || !reflect.DeepEqual(o.Cloud["also"], []string{"aws"}) {
		t.Errorf("cloud = %v", o.Cloud)
	}
	if o.Email == nil || o.Email.Provider != "gmail" || o.GitEmail != "me@acme.com" || o.GitProvider != "github" {
		t.Errorf("options = %+v", o)
	}
	if !reflect.DeepEqual(o.OtherProviders, []string{"In-house"}) || len(o.Browsers) != 1 {
		t.Errorf("other = %v browsers = %v", o.OtherProviders, o.Browsers)
	}
}

// Skipping everything but the email leaves a Claude-only identity.
func TestAddOptions_AllSkipped(t *testing.T) {
	o, err := (&Answers{Email: "me@acme.com"}).AddOptions("/base", "acme", "Me")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(o.Identities, []string{"claude"}) || o.Cloud != nil || o.Email != nil || len(o.ExtraScope) != 0 {
		t.Fatalf("options = %+v", o)
	}
}

func TestValidate_RefusesBadAnswers(t *testing.T) {
	good := "me@acme.com"
	for name, a := range map[string]*Answers{
		"email":   {Email: "nope"},
		"other":   {Email: good, Other: []string{"a;b"}},
		"browser": {Email: good, Browsers: []manifest.Browser{{Name: "safari", Profile: "x"}}},
		"profile": {Email: good, Browsers: []manifest.Browser{{Name: "chrome", Profile: "a/b"}}},
		"token":   {Email: good, Other: []string{"ghp_" + strings.Repeat("a", 30)}},
	} {
		if a.Validate() == nil {
			t.Errorf("%s passed", name)
		}
	}
}

func TestGitNameFor(t *testing.T) {
	if got := GitNameFor(" Renato ", "me@acme.com"); got != "Renato" {
		t.Errorf("global name: %q", got)
	}
	if got := GitNameFor("", "first.last@acme.com"); got != "first.last" {
		t.Errorf("from the email: %q", got)
	}
}
