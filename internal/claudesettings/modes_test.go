package claudesettings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

const ghRepoDelete = "Bash(gh repo delete:*)"

// spec E §8.1: a settings deny blocks in every Claude mode and would beat a
// relaxed workspace's confirm, so gh repo delete moves from deny to ask where
// git-history is loosened past its own action.
func TestDesired_GhRepoDeleteMovesToAskWhenGitHistoryLoosened(t *testing.T) {
	cases := []struct {
		name  string
		guard manifest.Guard
		ask   bool
	}{
		{"default", manifest.Guard{}, false},
		{"strict", manifest.Guard{Mode: "strict"}, false},
		{"relaxed", manifest.Guard{Mode: "relaxed"}, true},
		{"custom on relaxed", manifest.Guard{Mode: "custom", Base: "relaxed"}, true},
		{"custom default + git-history ask", manifest.Guard{Mode: "custom", Categories: map[string]string{"git-history": "ask"}}, true},
		{"custom relaxed + git-history rule", manifest.Guard{Mode: "custom", Base: "relaxed", Categories: map[string]string{"git-history": "rule"}}, false},
		{"custom relaxed + git-history deny", manifest.Guard{Mode: "custom", Base: "relaxed", Categories: map[string]string{"git-history": "deny"}}, false},
		{"rules_override ask", manifest.Guard{RulesOverride: map[string]string{"tq/gh-repo-delete": "ask"}}, true},
		{"rules_override deny", manifest.Guard{Mode: "relaxed", RulesOverride: map[string]string{"tq/gh-repo-delete": "deny"}}, false},
		{"project relaxed", manifest.Guard{Projects: map[string]manifest.GuardProject{"demo": {Mode: "relaxed"}}}, true},
		{"legacy unattended", manifest.Guard{Unattended: &manifest.Unattended{Workspace: true}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := man("")
			m.Guard = c.guard
			d := Desired(m, false, "")
			if got := contains(d.Ask, ghRepoDelete); got != c.ask {
				t.Fatalf("ask has gh repo delete = %v, want %v (ask %v)", got, c.ask, d.Ask)
			}
			if got := contains(d.Deny, ghRepoDelete); got == c.ask {
				t.Fatalf("deny has gh repo delete = %v, want %v", got, !c.ask)
			}
			// The secret reads stay denied in every mode (locked).
			for _, e := range []string{"Read(~/.ssh/id_*)", "Bash(printenv)"} {
				if !contains(d.Deny, e) {
					t.Errorf("deny lost %s", e)
				}
			}
		})
	}
}

// A deny the manifest itself lists stays a deny.
func TestDesired_ManifestDenyOfGhRepoDeleteStays(t *testing.T) {
	m := man("")
	m.Guard = manifest.Guard{Mode: "relaxed"}
	m.Claude.Permissions = &manifest.Permissions{Deny: []string{ghRepoDelete}}
	d := Desired(m, false, "")
	if !contains(d.Deny, ghRepoDelete) || contains(d.Ask, ghRepoDelete) {
		t.Fatalf("deny %v ask %v", d.Deny, d.Ask)
	}
}

// skipDangerousModePermissionPrompt is rendered (the launcher always starts
// in bypass), and defaultMode still follows claude.permission_mode.
func TestDesired_SkipDangerousPromptAndDefaultMode(t *testing.T) {
	for _, pm := range []string{"", "acceptEdits", "plan", "auto", "dontAsk"} {
		d := Desired(man(pm), false, "")
		if !d.SkipDangerousPrompt {
			t.Errorf("mode %q: SkipDangerousPrompt not set", pm)
		}
		if d.DefaultMode != ModeFor(pm) {
			t.Errorf("mode %q: defaultMode %q, want %q", pm, d.DefaultMode, ModeFor(pm))
		}
	}
	next, _ := Apply(map[string]any{}, Desired(man("plan"), false, ""), Owned{})
	if next["skipDangerousModePermissionPrompt"] != true {
		t.Fatalf("not rendered: %v", next["skipDangerousModePermissionPrompt"])
	}
	if p := next["permissions"].(map[string]any); p["defaultMode"] != "plan" {
		t.Fatalf("defaultMode = %v", p["defaultMode"])
	}
}

// The global guard.yaml is a layer too (spec E §6.1): relaxed there moves the
// repo-delete entry to ask like a relaxed manifest does. An unreadable global
// file applies nothing, so the deny stays (fail closed). The launch layer
// (--tq-mode, TQ_MODE) is per session and cannot be rendered into settings.
func TestDesired_GlobalGuardFileLoosensGitHistory(t *testing.T) {
	cases := []struct {
		name, file string
		ask        bool
	}{
		{"none", "", false},
		{"global relaxed", "mode: relaxed\n", true},
		{"global strict", "mode: strict\n", false},
		{"global custom git-history ask", "mode: custom\ncategories:\n  git-history: ask\n", true},
		{"global rules_override skip", "rules_override:\n  tq/gh-repo-delete: skip\n", true},
		{"global relaxed, override deny", "mode: relaxed\nrules_override:\n  tq/gh-repo-delete: deny\n", false},
		{"unparsable", "mode: [relaxed\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("TQ_HOME", home)
			if c.file != "" {
				if err := os.WriteFile(filepath.Join(home, "guard.yaml"), []byte(c.file), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			d := Desired(man(""), false, "")
			if got := contains(d.Ask, ghRepoDelete); got != c.ask {
				t.Fatalf("ask has the repo-delete entry = %v, want %v", got, c.ask)
			}
			if got := contains(d.Deny, ghRepoDelete); got == c.ask {
				t.Fatalf("deny has the repo-delete entry = %v, want %v", got, !c.ask)
			}
		})
	}
}
