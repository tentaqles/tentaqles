package guardcfg

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
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

// builtinIDs lists every rule id tq ships: regex rules, hook rules, Jev
// judgment rules and the guard's fail-safe ask.
func builtinIDs() map[string]bool {
	ids := map[string]bool{GuardErrorID: true}
	for _, r := range policy.Builtin() {
		ids[r.ID] = true
	}
	for id := range HookRules {
		ids[id] = true
	}
	for _, r := range decide.BuiltinJudgeRules() {
		ids[r.ID] = true
	}
	return ids
}

func TestAlwaysEnforcedShim(t *testing.T) {
	got := AlwaysEnforced()
	for _, g := range []UnattendedGroup{GroupIdentity, GroupSecret, GroupSelfProtection, GroupPrivacy} {
		if len(got[g]) == 0 {
			t.Errorf("group %s is empty", g)
		}
	}
	if _, ok := got[GroupSkippable]; ok {
		t.Error("skippable rules are not always enforced")
	}
	if !slices.Contains(got[GroupSelfProtection], "tq/guard-change") || !slices.Contains(got[GroupIdentity], "tq/cross-client-mcp") || !slices.Contains(got[GroupPrivacy], "tq/commit-pii") {
		t.Fatalf("got %v", got)
	}
	if !slices.IsSorted(got[GroupSecret]) {
		t.Errorf("ids must be sorted: %v", got[GroupSecret])
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
	if got := UnknownOverrides(m, File{RulesOverride: map[string]Value{"me/x": ValueAsk, "tq/iac-destroy": ValueAsk}}); !reflect.DeepEqual(got, []string{"me/x", "tq/nope"}) {
		t.Fatalf("UnknownOverrides: %v", got)
	}
}

func TestCoversIgnoresEscapingProjectEntries(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "demo-todo/src", "x")
	for _, p := range []string{".", "..", "../x", ""} {
		m := mWith(&manifest.Unattended{Projects: []string{p}})
		for _, cwd := range []string{root, filepath.Join(root, "demo-todo", "src"), filepath.Dir(root)} {
			if ok, scope := Covers(m, root, cwd); ok {
				t.Errorf("entry %q must not cover %q (scope %q)", p, cwd, scope)
			}
		}
	}
}
