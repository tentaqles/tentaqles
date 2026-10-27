package guardcfg

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

func mWith(u *manifest.Unattended) *manifest.Manifest {
	m := &manifest.Manifest{Client: "acme"}
	m.Guard.Unattended = u
	return m
}

func mkdirs(t *testing.T, root string, rel ...string) {
	t.Helper()
	for _, r := range rel {
		if err := os.MkdirAll(filepath.Join(root, r), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCoversWorkspaceAndProject(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "demo-todo/src", "other")
	ws := mWith(&manifest.Unattended{Workspace: true})
	if ok, scope := Covers(ws, root, filepath.Join(root, "other")); !ok || scope != "workspace" {
		t.Fatalf("workspace: %v %q", ok, scope)
	}
	pr := mWith(&manifest.Unattended{Projects: []string{"demo-todo/"}})
	if ok, scope := Covers(pr, root, filepath.Join(root, "demo-todo", "src")); !ok || scope != "demo-todo" {
		t.Fatalf("project subdir: %v %q", ok, scope)
	}
	if ok, _ := Covers(pr, root, filepath.Join(root, "other")); ok {
		t.Fatal("other folder must not be covered")
	}
	if ok, _ := Covers(pr, root, root); ok {
		t.Fatal("the root itself is not a covered project")
	}
}

func TestCoversSiblingAndOutside(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "demo-todo", "demo-todo2")
	pr := mWith(&manifest.Unattended{Projects: []string{"demo-todo"}})
	if ok, _ := Covers(pr, root, filepath.Join(root, "demo-todo2")); ok {
		t.Fatal("sibling prefix must not be covered")
	}
	ws := mWith(&manifest.Unattended{Workspace: true})
	if ok, _ := Covers(ws, root, t.TempDir()); ok {
		t.Fatal("a folder outside the root must not be covered")
	}
	if ok, _ := Covers(ws, root, ""); ok {
		t.Fatal("empty cwd must not be covered")
	}
	if ok, _ := Covers(nil, root, root); ok {
		t.Fatal("nil manifest must not be covered")
	}
}

func TestCoversCaseOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive paths are a Windows rule")
	}
	root := t.TempDir()
	mkdirs(t, root, "Demo-Todo")
	pr := mWith(&manifest.Unattended{Projects: []string{"demo-todo"}})
	if ok, _ := Covers(pr, strings.ToUpper(root), filepath.Join(root, "Demo-Todo")); !ok {
		t.Fatal("case must not matter on Windows")
	}
}

func TestUnattendedModesAndTrust(t *testing.T) {
	root := t.TempDir()
	m := mWith(&manifest.Unattended{Workspace: true})
	for mode, want := range map[string]bool{
		"bypassPermissions": true, "auto": true,
		"default": false, "acceptEdits": false, "plan": false, "dontAsk": false, "": false, "BYPASSPERMISSIONS": false,
	} {
		if got, _ := Unattended(m, root, root, mode, true); got != want {
			t.Errorf("mode %q: got %v want %v", mode, got, want)
		}
	}
	if got, _ := Unattended(m, root, root, "bypassPermissions", false); got {
		t.Error("without bypass trust it must be off")
	}
}

func TestSkippable(t *testing.T) {
	def := mWith(&manifest.Unattended{Workspace: true})
	if !Skippable(def, []string{"tq/pii-fixture-dump", "tq/text-pii"}) {
		t.Error("PII confirms must be skippable")
	}
	if Skippable(def, []string{"tq/text-pii", "tq/iac-apply"}) {
		t.Error("a default keep_asking id must keep asking")
	}
	empty := []string{}
	none := mWith(&manifest.Unattended{Workspace: true, KeepAsking: &empty})
	if !Skippable(none, []string{"tq/iac-apply"}) {
		t.Error("explicit empty keep_asking skips iac-apply")
	}
	if Skippable(none, []string{"tq/guard-change"}) {
		t.Error("tq/guard-change is always asked")
	}
	if Skippable(none, nil) {
		t.Error("no matched rule ids: nothing to skip, keep asking")
	}
}

func TestUnknownKeepAsking(t *testing.T) {
	ka := []string{"tq/iac-apply", "tq/nope"}
	m := mWith(&manifest.Unattended{Workspace: true, KeepAsking: &ka})
	if got := UnknownKeepAsking(m, File{}); !reflect.DeepEqual(got, []string{"tq/nope"}) {
		t.Fatalf("got %v", got)
	}
	if got := UnknownKeepAsking(mWith(&manifest.Unattended{Workspace: true}), File{}); len(got) != 0 {
		t.Fatalf("defaults are all known, got %v", got)
	}
	if got := UnknownKeepAsking(nil, File{}); got != nil {
		t.Fatalf("nil manifest: %v", got)
	}
}
