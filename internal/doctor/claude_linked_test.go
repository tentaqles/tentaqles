package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// Two identities whose claude/plugins directories are links to one shared
// directory (seen on a real machine: dirtybird and uplabs both linked to
// ~/.claude/plugins). Every install from either identity rewrites the same
// installed_plugins.json, so "foreign path" is the wrong diagnosis; the link
// itself is the finding, once per identity.
func TestPluginForeignPath_LinkedPluginsDirIsReportedOnce(t *testing.T) {
	isolatedHome(t)
	shared := filepath.Join(paths.Home(), "shared-plugins")
	if err := os.MkdirAll(filepath.Join(shared, "cache", "m", "x", "1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		os.MkdirAll(paths.IdentityDir(id, "claude"), 0o755)
		mkLink(t, filepath.Join(paths.IdentityDir(id, "claude"), "plugins"), shared)
	}
	// Records written through identity a's path and through the shared dir's own path.
	viaA := filepath.ToSlash(filepath.Join(paths.IdentityDir("a", "claude"), "plugins", "cache", "m", "x", "1.0.0"))
	direct := filepath.ToSlash(filepath.Join(shared, "cache", "m", "x", "1.0.0"))
	doc := `{"version":2,"plugins":{"x@m":[{"version":"1.0.0","installPath":"` + viaA + `"}],"y@m":[{"version":"1.0.0","installPath":"` + direct + `"}]}}`
	if err := os.WriteFile(filepath.Join(shared, "installed_plugins.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	var got []Finding
	add := func(level, code, ws, msg, fix string) {
		got = append(got, Finding{Level: level, Code: code, Workspace: ws, Msg: msg, Fix: fix})
	}
	pluginForeignPath([]string{"a", "b"}, add)

	var linked, foreign []Finding
	for _, f := range got {
		switch f.Code {
		case "plugins-dir-linked":
			linked = append(linked, f)
		case "plugin-foreign-path":
			foreign = append(foreign, f)
		}
	}
	if len(foreign) != 0 {
		t.Fatalf("paths that resolve into the shared dir are not foreign: %+v", foreign)
	}
	if len(linked) != 2 {
		t.Fatalf("one finding per linked identity, got %+v", got)
	}
	for _, f := range linked {
		if f.Level != "warn" || f.Fix == "" || !strings.Contains(strings.ToLower(f.Msg), strings.ToLower(shared)) {
			t.Fatalf("%+v", f)
		}
	}
	// Each finding names the other identity sharing the directory.
	if !strings.Contains(linked[0].Msg, "b") || !strings.Contains(linked[1].Msg, "a") {
		t.Fatalf("findings must name the identities sharing the dir: %+v", linked)
	}
}

func TestPluginSkew_SharedFileCountedOnce(t *testing.T) {
	isolatedHome(t)
	shared := filepath.Join(paths.Home(), "shared-plugins")
	os.MkdirAll(shared, 0o755)
	for _, id := range []string{"a", "b"} {
		os.MkdirAll(paths.IdentityDir(id, "claude"), 0o755)
		mkLink(t, filepath.Join(paths.IdentityDir(id, "claude"), "plugins"), shared)
	}
	os.WriteFile(filepath.Join(shared, "installed_plugins.json"), []byte(`{"version":2,"plugins":{"x@m":[{"version":"1.0.0"}]}}`), 0o644)
	own := filepath.Join(paths.IdentityDir("c", "claude"), "plugins")
	os.MkdirAll(own, 0o755)
	os.WriteFile(filepath.Join(own, "installed_plugins.json"), []byte(`{"version":2,"plugins":{"x@m":[{"version":"2.0.0"}]}}`), 0o644)

	var got []Finding
	pluginSkew([]string{"a", "b", "c"}, func(level, code, ws, msg, fix string) {
		got = append(got, Finding{Code: code, Msg: msg})
	})

	if len(got) != 1 || got[0].Code != "plugin-version-skew" {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got[0].Msg, "a,b") {
		t.Fatalf("a shared file must be counted once, got %q", got[0].Msg)
	}
	if !strings.Contains(got[0].Msg, "1.0.0 (a)") || !strings.Contains(got[0].Msg, "2.0.0 (c)") {
		t.Fatalf("%q", got[0].Msg)
	}
}
