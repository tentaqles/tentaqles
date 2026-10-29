package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func loadBody(t *testing.T, body string) (*Manifest, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), FileName)
	raw := "schema: tentaqles-client-v2\nclient: acme\n" + body
	if err := os.WriteFile(p, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

func TestUnattendedAbsentIsOff(t *testing.T) {
	m, err := loadBody(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Guard.Unattended.Configured() {
		t.Fatal("absent block must not be configured")
	}
}

func TestUnattendedDefaults(t *testing.T) {
	m, err := loadBody(t, "guard:\n  unattended:\n    projects: [demo-todo]\n")
	if err != nil {
		t.Fatal(err)
	}
	u := m.Guard.Unattended
	if !u.Configured() || u.Workspace {
		t.Fatalf("want projects-only configured, got %+v", u)
	}
	if got := u.KeepAskingIDs(); !reflect.DeepEqual(got, DefaultKeepAsking) {
		t.Fatalf("keep_asking default = %v", got)
	}
	if u.Sandbox() != "workspace-write" {
		t.Fatalf("sandbox default = %q", u.Sandbox())
	}
}

func TestUnattendedExplicitEmptyKeepAsking(t *testing.T) {
	m, err := loadBody(t, "guard:\n  unattended:\n    workspace: true\n    keep_asking: []\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Guard.Unattended.KeepAskingIDs(); len(got) != 0 {
		t.Fatalf("explicit empty list must stay empty, got %v", got)
	}
}

func TestUnattendedRejectsBadValues(t *testing.T) {
	for name, body := range map[string]string{
		"dotdot":   "guard:\n  unattended:\n    projects: [../other]\n",
		"absolute": "guard:\n  unattended:\n    projects: [/etc]\n",
		"winabs":   "guard:\n  unattended:\n    projects: ['C:\\x']\n",
		"empty":    "guard:\n  unattended:\n    projects: ['']\n",
		"root":     "guard:\n  unattended:\n    projects: ['.']\n",
		"sandbox":  "guard:\n  unattended:\n    codex_sandbox: yolo\n",
		"ruleid":   "guard:\n  unattended:\n    keep_asking: ['tq/a b']\n",
	} {
		if _, err := loadBody(t, body); err == nil || !strings.Contains(err.Error(), "guard.unattended") {
			t.Errorf("%s: want a guard.unattended load error, got %v", name, err)
		}
	}
}

func TestGuardRuleUnattendedField(t *testing.T) {
	ok := "guard:\n  rules:\n    - id: acme/no-bar\n      action: deny\n      command: 'bar'\n      reason: no bar\n      unattended: enforce\n"
	m, err := loadBody(t, ok)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Guard.Rules[0].Unattended; got != "enforce" {
		t.Fatalf("unattended = %q", got)
	}
	bad := strings.Replace(ok, "unattended: enforce", "unattended: sometimes", 1)
	if _, err := loadBody(t, bad); err == nil || !strings.Contains(err.Error(), "guard.rules[acme/no-bar].unattended") {
		t.Fatalf("want a guard.rules unattended error, got %v", err)
	}
}
