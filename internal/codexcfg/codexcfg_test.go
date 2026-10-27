package codexcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

const user = "# my settings\nmodel = \"gpt-5\"\n\n[mcp_servers.docs]\ncommand = \"docs-mcp\"\n"

func write(t *testing.T, s string) string {
	t.Helper()
	dir := t.TempDir()
	if s != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func read(t *testing.T, dir string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(dir, "config.toml"))
	return string(b)
}

func readProfile(dir string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(dir, Profile+".config.toml"))
	return string(b), err == nil
}

func TestRenderTopLevelOnAndOff(t *testing.T) {
	dir := write(t, user)
	res, err := Render(dir, Want{Top: true, Sandbox: "workspace-write"}, false)
	if err != nil || !res.Changed {
		t.Fatalf("on: %+v %v", res, err)
	}
	got := read(t, dir)
	if !strings.HasPrefix(got, beginMarker+"\napproval_policy = \"never\"\nsandbox_mode = \"workspace-write\"\n"+endMarker+"\n") {
		t.Fatalf("managed block must lead the file:\n%s", got)
	}
	if !strings.HasSuffix(got, user) {
		t.Fatalf("user content changed:\n%s", got)
	}
	if res, _ := Render(dir, Want{Top: true, Sandbox: "workspace-write"}, false); res.Changed {
		t.Fatal("second render must be a no-op")
	}
	if _, err := Render(dir, Want{}, false); err != nil {
		t.Fatal(err)
	}
	if read(t, dir) != user {
		t.Fatalf("off must restore the user file exactly:\n%q", read(t, dir))
	}
}

// Codex layers $CODEX_HOME/<name>.config.toml for --profile <name>, so the
// projects-only setup is a file of its own and config.toml is never touched.
func TestRenderProfileFile(t *testing.T) {
	dir := write(t, user)
	res, err := Render(dir, Want{Profile: true, Sandbox: "read-only"}, false)
	if err != nil || !res.Changed {
		t.Fatalf("on: %+v %v", res, err)
	}
	if read(t, dir) != user {
		t.Fatalf("config.toml must be untouched:\n%q", read(t, dir))
	}
	got, ok := readProfile(dir)
	want := beginMarker + "\napproval_policy = \"never\"\nsandbox_mode = \"read-only\"\n" + endMarker + "\n"
	if !ok || got != want {
		t.Fatalf("profile file:\n%q", got)
	}
	if res, _ := Render(dir, Want{Profile: true, Sandbox: "read-only"}, false); res.Changed {
		t.Fatal("second render must be a no-op")
	}
	if _, err := Render(dir, Want{}, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := readProfile(dir); ok {
		t.Fatal("off must remove the profile file")
	}
	if read(t, dir) != user {
		t.Fatalf("off must leave config.toml exactly:\n%q", read(t, dir))
	}
}

func TestRenderSwitchTopToProfile(t *testing.T) {
	dir := write(t, user)
	_, _ = Render(dir, Want{Top: true, Sandbox: "workspace-write"}, false)
	if _, err := Render(dir, Want{Profile: true, Sandbox: "workspace-write"}, false); err != nil {
		t.Fatal(err)
	}
	if read(t, dir) != user {
		t.Fatalf("top block must be removed:\n%q", read(t, dir))
	}
	if _, ok := readProfile(dir); !ok {
		t.Fatal("profile file missing")
	}
}

func TestRenderKeepsUserContent(t *testing.T) {
	dir := write(t, "")
	if _, err := Render(dir, Want{}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("off on a missing file must not create it")
	}
	res, err := Render(dir, Want{Top: true, Sandbox: "workspace-write"}, true)
	if err != nil || !res.Changed {
		t.Fatalf("dry run: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("dry run must not write")
	}
	res, err = Render(dir, Want{Profile: true, Sandbox: "workspace-write"}, true)
	if err != nil || !res.Changed {
		t.Fatalf("profile dry run: %+v %v", res, err)
	}
	if _, ok := readProfile(dir); ok {
		t.Fatal("dry run must not write the profile file")
	}
}

func TestRenderConflict(t *testing.T) {
	dir := write(t, "approval_policy = \"on-request\"\n"+user)
	res, err := Render(dir, Want{Top: true, Sandbox: "workspace-write"}, false)
	if err != nil || res.Conflict == "" || res.Changed {
		t.Fatalf("want conflict without change: %+v %v", res, err)
	}
	dir = write(t, user)
	mine := "model = \"x\"\n"
	pp := filepath.Join(dir, Profile+".config.toml")
	if err := os.WriteFile(pp, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	if res, _ := Render(dir, Want{Profile: true, Sandbox: "workspace-write"}, false); res.Conflict == "" {
		t.Fatal("a user-owned tq-unattended profile file is a conflict")
	}
	if _, err := Render(dir, Want{}, false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(pp); string(b) != mine {
		t.Fatal("off must not delete a profile file tq does not own")
	}
}

func TestRenderUnterminatedBlockIsConflict(t *testing.T) {
	broken := beginMarker + "\napproval_policy = \"never\"\n" + user
	for _, w := range []Want{{Top: true, Sandbox: "workspace-write"}, {}} {
		dir := write(t, broken)
		res, err := Render(dir, w, false)
		if err != nil || res.Conflict == "" || !strings.Contains(res.Conflict, "no end marker") || res.Changed {
			t.Fatalf("want conflict for %+v: %+v %v", w, res, err)
		}
		if read(t, dir) != broken {
			t.Fatalf("file must be byte-identical:\n%q", read(t, dir))
		}
	}
}

func TestRenderProfileWithExtraContentIsConflict(t *testing.T) {
	owned := beginMarker + "\napproval_policy = \"never\"\nsandbox_mode = \"workspace-write\"\n" + endMarker + "\n"
	edited := owned + "model = \"mine\"\n"
	for _, w := range []Want{{Profile: true, Sandbox: "workspace-write"}, {}} {
		dir := write(t, user)
		pp := filepath.Join(dir, Profile+".config.toml")
		if err := os.WriteFile(pp, []byte(edited), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := Render(dir, w, false)
		if err != nil || res.Conflict == "" || res.Changed {
			t.Fatalf("want conflict for %+v: %+v %v", w, res, err)
		}
		if b, _ := os.ReadFile(pp); string(b) != edited {
			t.Fatalf("profile file must be unchanged:\n%q", b)
		}
		if OwnsProfile(dir) {
			t.Fatal("an edited file is not tq-owned")
		}
	}
}

func TestOwnsProfile(t *testing.T) {
	dir := write(t, "")
	if OwnsProfile(dir) {
		t.Fatal("missing file")
	}
	if _, err := Render(dir, Want{Profile: true, Sandbox: "danger-full-access"}, false); err != nil {
		t.Fatal(err)
	}
	if !OwnsProfile(dir) {
		t.Fatal("tq-written file must be owned")
	}
}

func TestDesired(t *testing.T) {
	mk := func(u *manifest.Unattended) *manifest.Manifest {
		m := &manifest.Manifest{}
		m.Guard.Unattended = u
		return m
	}
	ws := mk(&manifest.Unattended{Workspace: true})
	if got := Desired(ws, true); !got.Top || got.Profile || got.Sandbox != "workspace-write" {
		t.Fatalf("workspace-wide: %+v", got)
	}
	pr := mk(&manifest.Unattended{Projects: []string{"app"}, CodexSandbox: "read-only"})
	if got := Desired(pr, true); got.Top || !got.Profile || got.Sandbox != "read-only" {
		t.Fatalf("projects only: %+v", got)
	}
	if got := Desired(ws, false); got != (Want{}) {
		t.Fatalf("no bypass trust: %+v", got)
	}
}
