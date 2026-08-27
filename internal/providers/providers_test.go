package providers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate_Rules(t *testing.T) {
	cases := []struct {
		name    string
		p       Provider
		wantErr bool
	}{
		{"bad id", Provider{ID: "Bad_ID", Name: "x", Category: "other"}, true},
		{"bad category", Provider{ID: "good", Name: "x", Category: "nope"}, true},
		{"env without {dir} ok", Provider{ID: "good", Name: "x", Category: "other", Identity: Identity{Env: map[string]string{"X": "static"}}}, false},
		{"env with dotdot rejected", Provider{ID: "good", Name: "x", Category: "other", Identity: Identity{Env: map[string]string{"X": "{dir}/../escape"}}}, true},
		{"cli without command rejected", Provider{ID: "good", Name: "x", Category: "other", CLI: &CLI{}}, true},
		{"valid minimal", Provider{ID: "good", Name: "x", Category: "other"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestVars_ExpandsDir(t *testing.T) {
	p := Provider{ID: "x", Name: "x", Category: "other", Identity: Identity{Env: map[string]string{
		"FOO": "{dir}/config",
	}}}
	dir := filepath.Join("some", "dir")
	got := p.Vars(dir)
	want := filepath.Join(dir, "config")
	if got["FOO"] != want {
		t.Fatalf("Vars = %q, want %q", got["FOO"], want)
	}
}

func TestLoad_EmbeddedParsesAndUnique(t *testing.T) {
	c, err := loadEmbedded()
	if err != nil {
		t.Fatalf("loadEmbedded: %v", err)
	}
	if len(c.byID) == 0 {
		t.Fatalf("expected at least one embedded provider")
	}
	for id, p := range c.byID {
		if err := p.Validate(); err != nil {
			t.Errorf("provider %q: Validate() = %v", id, err)
		}
		if p.ID != id {
			t.Errorf("provider key %q != p.ID %q", id, p.ID)
		}
	}
}

func TestLoad_UserOverridesEmbedded(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TQ_HOME", tmp)

	base, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ids := base.IDs()
	if len(ids) == 0 {
		t.Skip("no embedded providers to override yet")
	}
	targetID := ids[0]

	userDir := UserDir()
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "id: " + targetID + "\nname: \"Custom Name\"\ncategory: other\n"
	if err := os.WriteFile(filepath.Join(userDir, targetID+".yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := c.Get(targetID)
	if !ok {
		t.Fatalf("Get(%q) not found", targetID)
	}
	if got.Name != "Custom Name" {
		t.Fatalf("Name = %q, want %q", got.Name, "Custom Name")
	}
	if got.Source == "embedded" {
		t.Fatalf("Source should not be embedded after override, got %q", got.Source)
	}
}

func TestLoad_UserInvalidReturnsError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TQ_HOME", tmp)

	userDir := UserDir()
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// filename does not match id -> Validate error
	content := "id: mismatched\nname: x\ncategory: other\n"
	if err := os.WriteFile(filepath.Join(userDir, "wrongname.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := Load(); err == nil {
		t.Fatalf("expected error for mismatched filename/id")
	}
}

func TestWriteUser_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TQ_HOME", tmp)

	p := Provider{ID: "myprovider", Name: "My Provider", Category: "other", Identity: Identity{Env: map[string]string{"MY_HOME": "{dir}"}}}
	path, err := WriteUser(p)
	if err != nil {
		t.Fatalf("WriteUser: %v", err)
	}
	if filepath.Dir(path) != UserDir() {
		t.Fatalf("path dir = %q, want %q", filepath.Dir(path), UserDir())
	}

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := c.Get("myprovider")
	if !ok {
		t.Fatalf("Get(myprovider) not found")
	}
	if got.Name != "My Provider" {
		t.Fatalf("Name = %q", got.Name)
	}
}

func TestMustLoad_Cached(t *testing.T) {
	c1 := MustLoad()
	c2 := MustLoad()
	if c1 != c2 {
		t.Fatalf("expected MustLoad to return the same cached *Catalog pointer, got %p and %p", c1, c2)
	}
}

func TestValidate_RejectsBadEnvKeys(t *testing.T) {
	for _, key := range []string{"X;id", "", "FOO BAR", "1X", "X;curl x|sh", "X=Y"} {
		p := Provider{
			ID:       "widget",
			Name:     "Widget",
			Category: "other",
			Identity: Identity{Env: map[string]string{key: "{dir}"}},
		}
		if err := p.Validate(); err == nil {
			t.Errorf("expected env key %q to be rejected", key)
		}
	}
	for _, key := range []string{"CLAUDE_CONFIG_DIR", "_X", "TQ_WS", "TQ_WS_ROOT", "__TQ_STATE", "A1"} {
		p := Provider{
			ID:       "widget",
			Name:     "Widget",
			Category: "other",
			Identity: Identity{Env: map[string]string{key: "{dir}"}},
		}
		if err := p.Validate(); err != nil {
			t.Errorf("expected env key %q to be accepted, got %v", key, err)
		}
	}
}

func TestValidate_RejectsControlCharsInValues(t *testing.T) {
	for _, val := range []string{"{dir}\n", "{dir}\r", "a\x00b", "a\x7fb", "\tx"} {
		p := Provider{
			ID:       "widget",
			Name:     "Widget",
			Category: "other",
			Identity: Identity{Env: map[string]string{"WIDGET_HOME": val}},
		}
		if err := p.Validate(); err == nil {
			t.Errorf("expected value %q to be rejected", val)
		}
	}
}
