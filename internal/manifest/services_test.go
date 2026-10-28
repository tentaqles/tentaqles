package manifest

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"gopkg.in/yaml.v3"
)

// isolateHome keeps the catalog lookups (data and agent ids) away from the real home.
func isolateHome(t *testing.T) {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// TestTrackerProvidersAreCatalogIDs keeps the fixed tracker list from drifting away from the
// provider catalog: every tracker id is a catalog entry (github's CLI entry is "gh").
func TestTrackerProvidersAreCatalogIDs(t *testing.T) {
	isolateHome(t)
	cat := providers.MustLoad()
	catalogID := map[string]string{"github": "gh"}
	for _, p := range TrackerProviders {
		id := p
		if alias, ok := catalogID[p]; ok {
			id = alias
		}
		if _, ok := cat.Get(id); !ok {
			t.Errorf("tracker %q has no catalog entry %q", p, id)
		}
	}
}

const servicesYAML = `
schema: tentaqles-client-v2
client: acme
tracker:
  provider: azure-devops
  scope:
    org: acme
    project: Platform
    board: Platform Team
    area: Web
  assigned_to: me
  auth: env
  env: ACME_ADO_TOKEN
  lanes:
    "In Review": review
    "QA": review
chat:  { provider: slack }
email: { provider: gmail }
data_platform: { provider: snowflake }
agents: [claude, codex]
`

func TestLoadServiceBlocks(t *testing.T) {
	isolateHome(t)
	m, err := Load(write(t, servicesYAML))
	if err != nil {
		t.Fatal(err)
	}
	tr := m.Tracker
	if tr == nil || tr.Provider != "azure-devops" || tr.Org() != "acme" || tr.Project() != "Platform" || tr.Board() != "Platform Team" ||
		tr.Scope["area"] != "Web" || tr.AssignedTo != "me" || tr.Auth != "env" || tr.Env != "ACME_ADO_TOKEN" || tr.Lanes["QA"] != "review" {
		t.Fatalf("tracker %+v", tr)
	}
	if tr.ScopeKey() != "area=Web;board=Platform Team;org=acme;project=Platform" {
		t.Fatalf("scope key %q", tr.ScopeKey())
	}
	if m.Chat.Provider != "slack" || m.Email.Provider != "gmail" || m.DataPlatform.Provider != "snowflake" ||
		!reflect.DeepEqual(m.Agents, []string{"claude", "codex"}) {
		t.Fatalf("%+v %+v %+v %v", m.Chat, m.Email, m.DataPlatform, m.Agents)
	}
	if !m.HasTracker() {
		t.Fatal("HasTracker")
	}
}

func TestServiceBlocksAreOptional(t *testing.T) {
	isolateHome(t)
	m, err := Load(write(t, "schema: tentaqles-client-v2\nclient: acme\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Tracker != nil || m.Chat != nil || m.Email != nil || m.DataPlatform != nil || m.Agents != nil || m.HasTracker() {
		t.Fatalf("%+v", m)
	}
	m, err = Load(write(t, "schema: tentaqles-client-v2\nclient: acme\nchat: { provider: none }\nemail: { provider: none }\ndata_platform: { provider: none }\ntracker: { provider: github, scope: { org: acme, board: 3 } }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Tracker.Board() != "3" || m.Tracker.AuthOrDefault() != "cli" || m.Tracker.AssignedToOrDefault() != "me" {
		t.Fatalf("tracker defaults %+v", m.Tracker)
	}
}

func TestServiceBlocksValidate(t *testing.T) {
	isolateHome(t)
	base := "schema: tentaqles-client-v2\nclient: acme\n"
	for name, body := range map[string]string{
		"unknown tracker":      "tracker: { provider: trello }",
		"tracker no provider":  "tracker: { scope: { org: acme } }",
		"bad auth":             "tracker: { provider: jira, auth: password }",
		"env auth without var": "tracker: { provider: jira, auth: env }",
		"bad env name":         "tracker: { provider: jira, auth: env, env: 'A B' }",
		"bad assigned_to":      "tracker: { provider: jira, assigned_to: tester }",
		"empty lane":           "tracker: { provider: jira, lanes: { QA: '' } }",
		"empty scope key":      "tracker: { provider: jira, scope: { '': x } }",
		"unknown chat":         "chat: { provider: discord }",
		"chat no provider":     "chat: {}",
		"unknown email":        "email: { provider: yahoo }",
		"unknown data":         "data_platform: { provider: excel }",
		"non-data data":        "data_platform: { provider: claude }",
		"unknown agent":        "agents: [claude, hal]",
		"non-agent agent":      "agents: [snowflake]",
		"duplicate agent":      "agents: [claude, claude]",
	} {
		_, err := Load(write(t, base+body+"\n"))
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The error names the field and the known values.
	_, err := Load(write(t, base+"tracker: { provider: trello }\n"))
	if err == nil || !strings.Contains(err.Error(), "tracker.provider") || !strings.Contains(err.Error(), "azure-devops") {
		t.Fatalf("error text: %v", err)
	}
}

// TestServiceBlocksMarshalRoundTrip: the manifest writer (yaml.Marshal of a Manifest) keeps every
// block and drops absent ones.
func TestServiceBlocksMarshalRoundTrip(t *testing.T) {
	isolateHome(t)
	m, err := Load(write(t, servicesYAML))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := Load(write(t, string(raw)))
	if err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	if !reflect.DeepEqual(m.Tracker, m2.Tracker) || !reflect.DeepEqual(m.Chat, m2.Chat) || !reflect.DeepEqual(m.Email, m2.Email) ||
		!reflect.DeepEqual(m.DataPlatform, m2.DataPlatform) || !reflect.DeepEqual(m.Agents, m2.Agents) {
		t.Fatalf("round trip lost data:\n%s", raw)
	}
	empty, _ := yaml.Marshal(&Manifest{Schema: "tentaqles-client-v2", Client: "acme"})
	for _, k := range []string{"tracker:", "chat:", "email:", "data_platform:", "agents:"} {
		if strings.Contains("\n"+string(empty), "\n"+k) { // a top-level key, not git.email
			t.Fatalf("absent block %s written:\n%s", k, empty)
		}
	}
}
