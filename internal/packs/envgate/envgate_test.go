package envgate

import (
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

func node(t *testing.T, src string) yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return *doc.Content[0]
}

// settings builds a cloud Settings whose Raw is the given pack block.
func settings(t *testing.T, src string) packs.Settings {
	t.Helper()
	pc := manifest.PackConfig{Raw: node(t, src)}
	return packs.Merge(map[string]manifest.PackConfig{"cloud": pc}, nil).Pack("cloud")
}

const block = `
environments:
  dev:
    match: {workspaces: [dev], dirs: [infra/envs/dev], subscriptions: [11111111-1111-1111-1111-111111111111], profiles: [acme-dev], dotenv: [.env.dev]}
    apply: auto
    destroy: ask
  staging:
    match: {dirs: [infra/envs/staging]}
  prod:
    match: {workspaces: [prod], dirs: [infra/envs/prod], subscriptions: [22222222-2222-2222-2222-222222222222], stacks: [acme-prod]}
`

func TestValidate(t *testing.T) {
	ok := node(t, `{dev: {match: {dirs: [a]}, apply: auto}, prod: {approval_ttl: 2h, require_merged: false}}`)
	if err := Cloud.Validate(ok); err != nil {
		t.Fatal(err)
	}
	for src, want := range map[string]string{
		`{dev: {apply: sometimes}}`:    "unknown policy",
		`{dev: {match: {hosts: [x]}}}`: "unknown match key",
		`{dev: {color: red}}`:          "unknown key",
		`{prod: {approval_ttl: 25h}}`:  "24h",
		`{a: {match: {subscriptions: [S]}}, b: {match: {subscriptions: [s]}}}`: "selects two environments",
		`{a: {match: {dotenv: [.env.x]}}, b: {match: {dotenv: [.env.x]}}}`:     "selects two environments",
		`[dev]`:                     "mapping",
		`{dev: {match: {dirs: a}}}`: "list",
		`{dev: {migrate: auto}}`:    "unknown key",
	} {
		err := Cloud.Validate(node(t, src))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", src, err, want)
		}
	}
	// the db spec knows migrate and hosts
	if err := DB.Validate(node(t, `{dev: {match: {hosts: [localhost], project_refs: [abc]}, migrate: auto}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultsByName(t *testing.T) {
	g := Load(Cloud, settings(t, block), settings(t, block))
	for _, c := range []struct{ env, action, want string }{
		{"prod", Apply, Approve}, {"prod", Destroy, Deny},
		{"staging", Apply, Ask}, {"staging", Destroy, Approve},
		{"dev", Apply, Auto}, {"dev", Destroy, Ask},
	} {
		if got := g.Policy(c.env, c.action); got != c.want {
			t.Errorf("%s %s = %s, want %s", c.env, c.action, got, c.want)
		}
	}
	if !g.RequireMerged("prod") || g.RequireMerged("dev") {
		t.Fatal("require_merged defaults: true for prod only")
	}
	if g.TTL("prod") != 0 {
		t.Fatal("no ttl set")
	}
	// strictest declared is prod (approve/deny)
	if g.Default() != "prod" {
		t.Fatalf("default: %s", g.Default())
	}
}

func TestResolveTiersAndStricter(t *testing.T) {
	g := Load(Cloud, settings(t, block), settings(t, block))
	r := g.Resolve(Apply, []Tier{{{Key: "dirs", Value: "infra/envs/dev/network"}}})
	if r.Env != "dev" || !r.Resolved {
		t.Fatalf("dir under dev: %+v", r)
	}
	// tier 1 wins over tier 3
	r = g.Resolve(Apply, []Tier{{{Key: "dirs", Value: "infra/envs/dev"}}, nil, {{Key: "workspaces", Value: "prod"}}})
	if r.Env != "dev" {
		t.Fatalf("first tier: %+v", r)
	}
	// two environments in one tier: the stricter
	r = g.Resolve(Apply, []Tier{{{Key: "dirs", Value: "infra/envs/dev"}, {Key: "stacks", Value: "acme/acme-prod"}}})
	if r.Env != "prod" {
		t.Fatalf("stricter: %+v", r)
	}
	// nothing: the default, unresolved
	r = g.Resolve(Apply, []Tier{{{Key: "dirs", Value: "elsewhere"}}})
	if r.Env != "prod" || r.Resolved {
		t.Fatalf("unresolved: %+v", r)
	}
	// case-insensitive subscription
	r = g.Resolve(Apply, []Tier{nil, nil, {{Key: "subscriptions", Value: "11111111-1111-1111-1111-111111111111"}}})
	if r.Env != "dev" {
		t.Fatalf("subscription: %+v", r)
	}
	// an explicit name
	r = g.Resolve(Apply, []Tier{{{Key: KeyName, Value: "staging"}}})
	if r.Env != "staging" {
		t.Fatalf("name: %+v", r)
	}
}

func TestDefaultEnvironmentKey(t *testing.T) {
	s := settings(t, block+"default_environment: staging\n")
	g := Load(Cloud, s, s)
	if g.Default() != "staging" {
		t.Fatal(g.Default())
	}
	// an undeclared default fails closed: the strictest
	s = settings(t, block+"default_environment: qa\n")
	g = Load(Cloud, s, s)
	if g.Default() != "prod" {
		t.Fatal(g.Default())
	}
	// no environments at all: a virtual "default" that asks
	g = Load(Cloud, settings(t, "require_plan: true\n"), settings(t, "require_plan: true\n"))
	if g.Default() != NoEnv || g.Policy(NoEnv, Apply) != Ask || g.Policy(NoEnv, Destroy) != Ask {
		t.Fatalf("no envs: %s %s", g.Default(), g.Policy(NoEnv, Apply))
	}
	if r := g.Resolve(Apply, nil); r.Env != NoEnv || r.Resolved {
		t.Fatalf("%+v", r)
	}
}

// The repo layer can only tighten: a repo-only environment or a repo match
// added to a loose environment never resolves a command looser than the
// trusted manifest alone would.
func TestRepoLayerFloor(t *testing.T) {
	trusted := settings(t, block)
	merged := settings(t, block+`
  sandbox:
    match: {dirs: [infra/envs/prod]}
    apply: auto
`)
	g := Load(Cloud, trusted, merged)
	r := g.Resolve(Apply, []Tier{{{Key: "dirs", Value: "infra/envs/prod"}}})
	// a repo-only environment always denies, so it can only tighten
	if r.Env != "sandbox" || g.Policy("sandbox", Apply) != Deny || g.Policy("sandbox", Destroy) != Deny {
		t.Fatalf("a repo env loosened prod: %+v %s", r, g.Policy(r.Env, Apply))
	}
	// a repo match added to a manifest environment is dropped
	merged = settings(t, strings.Replace(block, "dirs: [infra/envs/dev]", "dirs: [infra/envs/dev, infra/envs/qa]", 1))
	g = Load(Cloud, trusted, merged)
	if r := g.Resolve(Apply, []Tier{{{Key: "dirs", Value: "infra/envs/qa"}}}); r.Env != "prod" || r.Resolved {
		t.Fatalf("a repo match routed into dev: %+v", r)
	}
	// repo loosening a trusted policy
	merged = settings(t, strings.Replace(block, "match: {workspaces: [prod]", "apply: auto\n    match: {workspaces: [prod]", 1))
	g = Load(Cloud, trusted, merged)
	if g.Policy("prod", Apply) != Approve {
		t.Fatalf("repo loosened prod apply: %s", g.Policy("prod", Apply))
	}
	// repo-only env elsewhere still resolves (it can only add)
	merged = settings(t, block+"  qa: {match: {dirs: [infra/envs/qa]}, apply: deny}\n")
	g = Load(Cloud, trusted, merged)
	r = g.Resolve(Apply, []Tier{{{Key: "dirs", Value: "infra/envs/qa"}}})
	if r.Env != "qa" || g.Policy("qa", Apply) != Deny {
		t.Fatalf("repo qa: %+v %s", r, g.Policy("qa", Apply))
	}
	// a repo ttl can only shorten
	merged = settings(t, strings.Replace(block, "stacks: [acme-prod]}", "stacks: [acme-prod]}\n    approval_ttl: 10m", 1))
	g = Load(Cloud, trusted, merged)
	if g.TTL("prod") != 10*time.Minute {
		t.Fatal(g.TTL("prod"))
	}
}

func TestMergeRulesRegister(t *testing.T) {
	m := Cloud.MergeRules()
	if _, ok := m["environments.*.match.*"]; ok {
		t.Error("no union on match lists (ruling S1)")
	}
	for _, k := range []string{"environments.*", "environments.*.match", "environments.*.apply", "environments.*.destroy", "environments.*.approval_ttl", "environments.*.require_merged"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	if _, ok := DB.MergeRules()["environments.*.migrate"]; !ok {
		t.Fatal("db migrate")
	}
}

func TestStricter(t *testing.T) {
	if Stricter(Auto, Ask) != Ask || Stricter(Deny, Approve) != Deny || Stricter("bogus", Auto) != "bogus" {
		t.Fatal("order")
	}
	if Rank("bogus") <= Rank(Deny) {
		t.Fatal("unknown is strictest")
	}
}
