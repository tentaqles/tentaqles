package envgate_test

// These tests merge a real repo layer through packs.Merge with the cloud
// pack's registered rules (imported for its init), then resolve through
// envgate.Load: the review-r0 C3 probe.

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	_ "github.com/tentaqles/tentaqles/internal/packs/cloud"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
	"gopkg.in/yaml.v3"
)

func pc(t *testing.T, src string) manifest.PackConfig {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	on := true
	return manifest.PackConfig{Enabled: &on, Raw: *doc.Content[0]}
}

const trustedBlock = `
default_environment: prod
environments:
  dev:
    match: {dirs: [infra/envs/dev]}
    apply: auto
  prod:
    match: {dirs: [infra/envs/prod]}
`

func gateFor(t *testing.T, repo string) envgate.Gate {
	t.Helper()
	tr := map[string]manifest.PackConfig{"cloud": pc(t, trustedBlock)}
	var rp map[string]manifest.PackConfig
	if repo != "" {
		rp = map[string]manifest.PackConfig{"cloud": pc(t, repo)}
	}
	merged := packs.Merge(tr, rp)
	trusted := packs.Merge(tr, nil)
	return envgate.Load(envgate.Cloud, trusted.Pack("cloud"), merged.Pack("cloud"))
}

func qa() []envgate.Tier {
	return []envgate.Tier{{{Key: "dirs", Value: "infra/envs/qa"}}}
}

// C3: a repo file can neither extend a manifest environment's match lists
// nor add a looser environment that captures an unresolved target.
func TestRepoLayerCannotLoosenThroughMerge(t *testing.T) {
	base := gateFor(t, "")
	if r := base.Resolve(envgate.Apply, qa()); r.Env != "prod" || r.Resolved || base.Policy(r.Env, envgate.Apply) != envgate.Approve {
		t.Fatalf("trusted only: %+v %s", r, base.Policy(r.Env, envgate.Apply))
	}
	for name, repo := range map[string]string{
		"extend dev.match.dirs":    "environments:\n  dev:\n    match: {dirs: [infra/envs/qa]}\n",
		"add a match key to dev":   "environments:\n  dev:\n    match: {workspaces: [qa], dirs: [infra/envs/qa]}\n",
		"repo-only auto env":       "environments:\n  sandbox:\n    match: {dirs: [infra/envs/qa]}\n    apply: auto\n    destroy: auto\n",
		"repo-only env, no policy": "environments:\n  sandbox:\n    match: {dirs: [infra/envs/qa]}\n",
		"loosen prod":              "environments:\n  prod:\n    apply: auto\n",
		"default to dev":           "default_environment: dev\n",
	} {
		g := gateFor(t, repo)
		r := g.Resolve(envgate.Apply, qa())
		p := g.Policy(r.Env, envgate.Apply)
		if envgate.Rank(p) < envgate.Rank(envgate.Approve) {
			t.Errorf("%s: resolved %+v with policy %s, looser than approve", name, r, p)
		}
		if d := g.Policy(r.Env, envgate.Destroy); envgate.Rank(d) < envgate.Rank(envgate.Deny) {
			t.Errorf("%s: destroy %s, looser than deny", name, d)
		}
		// the dev route stays the manifest's
		if r := g.Resolve(envgate.Apply, []envgate.Tier{{{Key: "dirs", Value: "infra/envs/dev"}}}); r.Env != "dev" || g.Policy("dev", envgate.Apply) != envgate.Auto {
			t.Errorf("%s: dev route %+v %s", name, r, g.Policy("dev", envgate.Apply))
		}
	}
	// a repo-only environment can only tighten: it captures into a deny
	g := gateFor(t, "environments:\n  locked:\n    match: {dirs: [infra/envs/dev/locked]}\n    apply: deny\n")
	r := g.Resolve(envgate.Apply, []envgate.Tier{{{Key: "dirs", Value: "infra/envs/dev/locked"}}})
	if r.Env != "locked" || g.Policy("locked", envgate.Apply) != envgate.Deny {
		t.Fatalf("tightening env: %+v %s", r, g.Policy("locked", envgate.Apply))
	}
	// the repo may tighten a manifest policy it states explicitly
	g = gateFor(t, "environments:\n  dev:\n    apply: ask\n")
	if g.Policy("dev", envgate.Apply) != envgate.Ask {
		t.Fatalf("tighten dev: %s", g.Policy("dev", envgate.Apply))
	}
}

// ResolveFloor never returns an environment looser than Default.
func TestResolveFloor(t *testing.T) {
	g := gateFor(t, "")
	r := g.ResolveFloor(envgate.Apply, []envgate.Tier{{{Key: "dirs", Value: "infra/envs/dev"}}})
	if r.Env != "prod" || r.Resolved {
		t.Fatalf("floor: %+v", r)
	}
	r = g.ResolveFloor(envgate.Apply, []envgate.Tier{{{Key: "dirs", Value: "infra/envs/prod"}}})
	if r.Env != "prod" || !r.Resolved {
		t.Fatalf("prod stays resolved: %+v", r)
	}
}

// review fin M1: the stricter of two environments, and an undeclared
// environment (policy deny) wins.
func TestStricterEnv(t *testing.T) {
	g := gateFor(t, "")
	for _, c := range [][4]string{
		{"dev", "prod", envgate.Apply, "prod"},
		{"prod", "dev", envgate.Apply, "prod"},
		{"dev", "dev", envgate.Apply, "dev"},
		{"dev", "gone", envgate.Apply, "gone"},
		{"", "dev", envgate.Destroy, "dev"},
	} {
		if got := g.StricterEnv(c[0], c[1], c[2]); got != c[3] {
			t.Errorf("StricterEnv(%q, %q, %s) = %q, want %q", c[0], c[1], c[2], got, c[3])
		}
	}
	if p := g.Policy("gone", envgate.Apply); p != envgate.Deny {
		t.Errorf("undeclared policy: %s", p)
	}
}
