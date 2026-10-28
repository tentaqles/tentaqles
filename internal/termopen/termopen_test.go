package termopen

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func look(found ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, f := range found {
			if f == name {
				return "/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestPlan_WindowsPrefersWindowsTerminal(t *testing.T) {
	dir := t.TempDir()
	l, err := Plan("windows", dir, look("wt"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "wt" || !reflect.DeepEqual(l.Args, []string{"-d", dir}) || l.Dir != dir {
		t.Fatalf("got %+v", l)
	}
}

// Without wt, cmd's start opens the window; the folder travels as the child's working directory,
// never through cmd.exe's own parsing.
func TestPlan_WindowsFallsBackToCmdStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a & calc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := Plan("windows", dir, look())
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "cmd" || !reflect.DeepEqual(l.Args, []string{"/c", "start", "", "cmd"}) || l.Dir != dir {
		t.Fatalf("got %+v", l)
	}
}

// wt splits its own argv on ';' into subcommands, so a folder with ';' goes through the cmd start
// plan, which passes it only as the working directory.
func TestPlan_WindowsSemicolonFolderAvoidsWT(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a; new-tab calc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := Plan("windows", dir, look("wt"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "cmd" || !reflect.DeepEqual(l.Args, []string{"/c", "start", "", "cmd"}) || l.Dir != dir {
		t.Fatalf("got %+v", l)
	}
}

func TestPlan_Darwin(t *testing.T) {
	dir := t.TempDir()
	l, err := Plan("darwin", dir, look())
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "open" || !reflect.DeepEqual(l.Args, []string{"-a", "Terminal", dir}) {
		t.Fatalf("got %+v", l)
	}
}

func TestPlan_LinuxCandidates(t *testing.T) {
	dir := t.TempDir()
	l, err := Plan("linux", dir, look("gnome-terminal", "xterm"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "gnome-terminal" || !reflect.DeepEqual(l.Args, []string{"--working-directory=" + dir}) || l.Dir != dir {
		t.Fatalf("got %+v", l)
	}
	if _, err := Plan("linux", dir, look()); err == nil {
		t.Fatal("no emulator on PATH must be an error")
	}
}

func TestPlan_RefusesBadFolders(t *testing.T) {
	if _, err := Plan("windows", "", look("wt")); err == nil {
		t.Error("empty folder")
	}
	if _, err := Plan("windows", "relative", look("wt")); err == nil {
		t.Error("relative folder")
	}
	if _, err := Plan("windows", filepath.Join(t.TempDir(), "missing"), look("wt")); err == nil {
		t.Error("missing folder")
	}
	if _, err := Plan("windows", t.TempDir()+"\n", look("wt")); err == nil {
		t.Error("control character")
	}
}

func TestOpen_StartsAndNeverWaits(t *testing.T) {
	dir := t.TempDir()
	var got Launch
	prev := start
	start = func(l Launch) error { got = l; return nil }
	t.Cleanup(func() { start = prev })
	l, err := open("windows", dir, look("wt"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(l, got) || got.Name != "wt" {
		t.Fatalf("started %+v, planned %+v", got, l)
	}
}
