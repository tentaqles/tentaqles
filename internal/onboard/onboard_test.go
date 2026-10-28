package onboard

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

func writeFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "answers.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const full = `
name: acme
display_name: Acme
git:
  name: Tester
  email: tester@acme.test
  provider: github
  user: tester
cloud:
  provider: azure
  also: [aws]
data_platform: { provider: snowflake }
agents: [claude, codex]
tracker:
  provider: azure-devops
  scope: { org: acme, project: Platform, board: Platform Team }
  auth: cli
chat: { provider: slack }
email: { provider: gmail }
`

func TestLoad_FullAnswers(t *testing.T) {
	a, err := Load(writeFile(t, full))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if a.Name != "acme" || a.Git.Email != "tester@acme.test" || a.Cloud.Provider != "azure" || a.Tracker.Org() != "acme" {
		t.Fatalf("parsed %+v", a)
	}
}

func TestLoad_RefusesUnknownKeysAndSecrets(t *testing.T) {
	if _, err := Load(writeFile(t, "name: acme\nbogus: 1\n")); err == nil {
		t.Error("an unknown key must be refused (a typo would silently skip a block)")
	}
	if _, err := Load(writeFile(t, "name: acme\ngit: { email: a@b.test }\ntracker: { provider: jira, env: ghp_abcdefghijklmnopqrstuvwxyz0123 }\n")); err == nil { // tq:allow-secret (fake fixture)
		t.Error("a secret-shaped value must be refused")
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]string{
		"no name":         "git: { email: a@b.test }\n",
		"bad name":        "name: Acme!\ngit: { email: a@b.test }\n",
		"no email":        "name: acme\n",
		"bad git":         "name: acme\ngit: { email: a@b.test, provider: svn }\n",
		"bad cloud":       "name: acme\ngit: { email: a@b.test }\ncloud: { provider: mars }\n",
		"also no primary": "name: acme\ngit: { email: a@b.test }\ncloud: { also: [aws] }\n",
		"also dup":        "name: acme\ngit: { email: a@b.test }\ncloud: { provider: aws, also: [aws] }\n",
		"bad tracker":     "name: acme\ngit: { email: a@b.test }\ntracker: { provider: trello }\n",
		"env no var":      "name: acme\ngit: { email: a@b.test }\ntracker: { provider: jira, auth: env }\n",
		"bad chat":        "name: acme\ngit: { email: a@b.test }\nchat: { provider: irc }\n",
		"bad agent":       "name: acme\ngit: { email: a@b.test }\nagents: [hal]\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			a, err := Load(writeFile(t, body))
			if err == nil {
				err = a.Validate()
			}
			if err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

// Skipped blocks stay out of the manifest; a minimal answer set is valid.
func TestAddOptions_SkippedBlocks(t *testing.T) {
	a, err := Load(writeFile(t, "name: acme\ngit: { email: tester@acme.test }\n"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := a.AddOptions()
	if err != nil {
		t.Fatal(err)
	}
	if o.Tracker != nil || o.Chat != nil || o.Email != nil || o.DataPlatform != nil || o.Cloud != nil || len(o.Agents) != 0 {
		t.Fatalf("skipped blocks leaked: %+v", o)
	}
	if !reflect.DeepEqual(o.Identities, []string{"claude", "gh"}) {
		t.Fatalf("identities = %v, want the tq add default", o.Identities)
	}
	if o.Trust {
		t.Fatal("onboard never trusts: tq allow is the human's")
	}
}

func TestAddOptions_DerivesIdentities(t *testing.T) {
	a, err := Load(writeFile(t, full))
	if err != nil {
		t.Fatal(err)
	}
	o, err := a.AddOptions()
	if err != nil {
		t.Fatal(err)
	}
	// claude+codex (agents), gh (git), az+aws (cloud), snowflake (data), azure-devops (tracker cli).
	want := []string{"aws", "az", "azure-devops", "claude", "codex", "gh", "snowflake"}
	if !reflect.DeepEqual(o.Identities, want) {
		t.Fatalf("identities = %v, want %v", o.Identities, want)
	}
	if o.Cloud["provider"] != "azure" || !reflect.DeepEqual(o.Cloud["also"], []string{"aws"}) {
		t.Fatalf("cloud = %v", o.Cloud)
	}
	if o.Tracker.Provider != "azure-devops" || o.Chat.Provider != "slack" || o.Email.Provider != "gmail" || o.DataPlatform.Provider != "snowflake" {
		t.Fatalf("blocks = %+v", o)
	}
}

// A tracker or git host without an isolatable CLI adds no identity; agents without one either.
func TestAddOptions_SkipsProvidersWithoutIdentity(t *testing.T) {
	a, err := Load(writeFile(t, "name: acme\ngit: { email: a@b.test, provider: bitbucket }\nagents: [cursor]\ntracker: { provider: jira, auth: keychain }\n"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := a.AddOptions()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(o.Identities, []string{"claude"}) {
		t.Fatalf("identities = %v", o.Identities)
	}
}

func TestAddOptions_ExplicitIdentitiesWin(t *testing.T) {
	a, _ := Load(writeFile(t, "name: acme\ngit: { email: a@b.test }\nidentities: [claude]\ncloud: { provider: aws }\n"))
	o, err := a.AddOptions()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(o.Identities, []string{"claude"}) {
		t.Fatalf("identities = %v", o.Identities)
	}
}

func TestNextSteps(t *testing.T) {
	a, _ := Load(writeFile(t, full))
	m := a.Manifest()
	lines := strings.Join(NextSteps("acme", m), "\n")
	for _, want := range []string{
		"tq allow acme",
		"tq login acme claude",
		"tq login acme az",
		"azure-devops shares the az login",
		"uses the az CLI login above (tq login acme az)",
		"slack",
		"gmail",
		"/tentaqles:onboard",
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("next steps miss %q:\n%s", want, lines)
		}
	}
	if strings.Index(lines, "tq allow acme") > strings.Index(lines, "tq login acme claude") {
		t.Error("tq allow comes first: login refuses an untrusted workspace")
	}
}

// With az and azure-devops both listed, one login line is printed (az's); the tracker step must
// name that line, not the azure-devops one that was folded into it.
func TestNextSteps_TrackerNamesThePrintedSharedLogin(t *testing.T) {
	m := &manifest.Manifest{Client: "acme",
		Identities: map[string]manifest.Identity{"az": {}, "azure-devops": {}},
		Tracker:    &manifest.Tracker{Provider: "azure-devops", Auth: "cli"}}
	steps := NextSteps("acme", m)
	lines := strings.Join(steps, "\n")
	var printed []string
	for _, l := range steps {
		if strings.HasPrefix(l, "Log in: ") {
			printed = append(printed, strings.TrimPrefix(l, "Log in: "))
		}
	}
	if len(printed) != 1 {
		t.Fatalf("one shared login line, got %v:\n%s", printed, lines)
	}
	var tracker string
	for _, l := range steps {
		if strings.HasPrefix(l, "Tracker ") {
			tracker = l
		}
	}
	if !strings.Contains(tracker, "("+printed[0]+")") {
		t.Fatalf("tracker step %q must name the printed login %q", tracker, printed[0])
	}
}

func TestNextSteps_TrackerAuthSources(t *testing.T) {
	cases := map[string]struct {
		tr   *manifest.Tracker
		want string
	}{
		"jira cli":      {&manifest.Tracker{Provider: "jira", Auth: "cli"}, "acli jira auth login"},
		"linear cli":    {&manifest.Tracker{Provider: "linear"}, "tq secret set acme linear"},
		"keychain":      {&manifest.Tracker{Provider: "asana", Auth: "keychain"}, "tq secret set acme asana"},
		"env":           {&manifest.Tracker{Provider: "jira", Auth: "env", Env: "ACME_JIRA_TOKEN"}, "ACME_JIRA_TOKEN"},
		"mcp":           {&manifest.Tracker{Provider: "linear", Auth: "mcp"}, "/tentaqles:sync"},
		"github cli":    {&manifest.Tracker{Provider: "github"}, "tq login acme gh"},
		"ado cli login": {&manifest.Tracker{Provider: "azure-devops"}, "tq login acme azure-devops"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m := &manifest.Manifest{Client: "acme", Tracker: c.tr}
			got := strings.Join(NextSteps("acme", m), "\n")
			if !strings.Contains(got, c.want) {
				t.Fatalf("want %q in:\n%s", c.want, got)
			}
		})
	}
}

func TestSuggestEnvName(t *testing.T) {
	if got := SuggestEnvName("acme-co", "azure-devops"); got != "ACME_CO_AZURE_DEVOPS_TOKEN" {
		t.Fatalf("got %q", got)
	}
}
