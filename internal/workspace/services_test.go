package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// TestAdd_WritesServiceBlocks: the manifest writer emits the spec B §6 blocks it is given and
// the result loads back identically; blocks left out are not written.
func TestAdd_WritesServiceBlocks(t *testing.T) {
	isolateHome(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	cfg.Save()
	tr := &manifest.Tracker{Provider: "linear", Scope: map[string]string{"org": "acme"}, Auth: "env", Env: "ACME_LINEAR_TOKEN",
		Lanes: map[string]string{"In Review": "review"}}
	ws, err := Add(AddOptions{Base: base, Name: "acme", GitEmail: "tester@acme.test", RunGit: fakeGit, Trust: true,
		Tracker: tr, Chat: &manifest.Service{Provider: "slack"}, Email: &manifest.Service{Provider: "none"},
		DataPlatform: &manifest.Service{Provider: "snowflake"}, Agents: []string{"claude"}})
	if err != nil {
		t.Fatal(err)
	}
	m := ws.Manifest
	if !reflect.DeepEqual(m.Tracker, tr) || m.Chat.Provider != "slack" || m.Email.Provider != "none" ||
		m.DataPlatform.Provider != "snowflake" || !reflect.DeepEqual(m.Agents, []string{"claude"}) {
		t.Fatalf("%+v", m)
	}
	if _, err := Add(AddOptions{Base: base, Name: "bad", GitEmail: "tester@acme.test", RunGit: fakeGit,
		Tracker: &manifest.Tracker{Provider: "trello"}}); err == nil {
		t.Fatal("an invalid tracker was written")
	}
	if _, err := os.Stat(filepath.Join(base, "bad", manifest.FileName)); err == nil {
		t.Fatal("a manifest that does not load was left behind")
	}
	plain, err := Add(AddOptions{Base: base, Name: "plain", GitEmail: "tester@acme.test", RunGit: fakeGit})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(plain.ManifestPath)
	for _, k := range []string{"tracker:", "chat:", "email:", "data_platform:", "agents:"} {
		if strings.Contains("\n"+string(raw), "\n"+k) {
			t.Fatalf("absent block %s written:\n%s", k, raw)
		}
	}
}
