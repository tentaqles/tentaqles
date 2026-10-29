// Package envgate resolves the target environment of a gated command (an
// IaC apply or destroy, a database migration) from the pack's
// `environments` config and the command's signals (spec D §9.2, §9.3, §14).
// It is shared by the cloud and db packs. It does no I/O: callers collect
// the signals (flags, wrappers, variables, small local files) and pass them
// in tiers.
package envgate

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

// Policy values, loosest first (spec D §9.3).
const (
	Auto    = "auto"
	Ask     = "ask"
	Approve = "approve"
	Deny    = "deny"
)

// Actions.
const (
	Apply   = "apply"
	Destroy = "destroy"
	Migrate = "migrate"
)

// Order lists the policy values loosest first.
var Order = []string{Auto, Ask, Approve, Deny}

// MaxTTL caps approval_ttl.
const MaxTTL = 24 * time.Hour

// KeyName is the signal key that names an environment directly (cdk
// --context env=prod, sam --config-env prod).
const KeyName = "name"

// NoEnv is the virtual environment of a pack that declares none: every
// policy is ask, like the core rules it supersedes.
const NoEnv = "default"

// Rank orders a policy value; an unknown value ranks above deny (fail closed).
func Rank(p string) int {
	for i, o := range Order {
		if o == p {
			return i
		}
	}
	return len(Order)
}

// Stricter returns the stricter of two policy values.
func Stricter(a, b string) string {
	if Rank(b) > Rank(a) {
		return b
	}
	return a
}

// Spec describes one pack's environments block.
type Spec struct {
	Pack string
	// Actions are the policy keys (cloud: apply, destroy; db: migrate).
	Actions []string
	// MatchKeys are the keys allowed under match.
	MatchKeys []string
	// Unique are match keys whose values may select one environment only.
	Unique []string
}

// Cloud is the cloud pack's spec (§4).
var Cloud = Spec{
	Pack:      "cloud",
	Actions:   []string{Apply, Destroy},
	MatchKeys: []string{"workspaces", "dirs", "var_files", "stacks", "resource_groups", "subscriptions", "accounts", "profiles", "dotenv"},
	Unique:    []string{"subscriptions", "accounts", "stacks", "dotenv"},
}

// DB is the db pack's spec (§4, §14): the cloud keys plus hosts,
// project_refs and servers, with migrate in place of apply.
var DB = Spec{
	Pack:      "db",
	Actions:   []string{Migrate},
	MatchKeys: []string{"workspaces", "dirs", "var_files", "stacks", "resource_groups", "subscriptions", "accounts", "profiles", "dotenv", "hosts", "project_refs", "servers"},
	Unique:    []string{"subscriptions", "accounts", "stacks", "dotenv", "project_refs", "servers"},
}

// DefaultPolicy is the value of an action an environment does not set
// (decided at spec review): prod apply/migrate approve and destroy deny;
// staging apply/migrate ask and destroy approve; any other ask.
func DefaultPolicy(env, action string) string {
	switch strings.ToLower(env) {
	case "prod":
		if action == Destroy {
			return Deny
		}
		return Approve
	case "staging":
		if action == Destroy {
			return Approve
		}
		return Ask
	}
	return Ask
}

// Env is one declared environment, defaults applied.
type Env struct {
	Name          string
	Match         map[string][]string
	Policy        map[string]string
	TTL           time.Duration // 0: the pack default
	RequireMerged bool
	Line          int
}

func (s Spec) has(list []string, k string) bool {
	for _, x := range list {
		if x == k {
			return true
		}
	}
	return false
}

// Validate checks an environments node (the ConfigKeys validator): known
// keys and policy values, list-valued match keys, approval_ttl at most 24h,
// and no unique match value selecting two environments.
func (s Spec) Validate(n yaml.Node) error {
	_, err := s.parse(&n)
	return err
}

// Parse reads an environments node; a nil node is no environment.
func (s Spec) Parse(n *yaml.Node) ([]Env, error) { return s.parse(n) }

func (s Spec) parse(n *yaml.Node) ([]Env, error) {
	if n == nil || n.Kind == 0 {
		return nil, nil
	}
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: environments must be a mapping of name -> settings", n.Line)
	}
	var out []Env
	owner := map[string]string{} // key\x00lower(value) -> env
	for i := 0; i+1 < len(n.Content); i += 2 {
		name := n.Content[i].Value
		body := n.Content[i+1]
		if body.Kind == yaml.AliasNode && body.Alias != nil {
			body = body.Alias
		}
		if name == "" {
			return nil, fmt.Errorf("line %d: an environment needs a name", n.Content[i].Line)
		}
		e := Env{Name: name, Match: map[string][]string{}, Policy: map[string]string{}, Line: n.Content[i].Line}
		if body.Kind == yaml.ScalarNode && body.Tag == "!!null" {
			out = append(out, s.defaults(e, false))
			continue
		}
		if body.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("line %d: environment %s must be a mapping", body.Line, name)
		}
		mergedSet := false
		for j := 0; j+1 < len(body.Content); j += 2 {
			k, v := body.Content[j].Value, body.Content[j+1]
			if v.Kind == yaml.AliasNode && v.Alias != nil {
				v = v.Alias
			}
			switch {
			case k == "match":
				if v.Kind != yaml.MappingNode {
					return nil, fmt.Errorf("line %d: %s.match must be a mapping", v.Line, name)
				}
				for m := 0; m+1 < len(v.Content); m += 2 {
					mk, mv := v.Content[m].Value, v.Content[m+1]
					if !s.has(s.MatchKeys, mk) {
						return nil, fmt.Errorf("line %d: %s.match: unknown match key %q (known: %s)", v.Content[m].Line, name, mk, strings.Join(s.MatchKeys, ", "))
					}
					if mv.Kind != yaml.SequenceNode {
						return nil, fmt.Errorf("line %d: %s.match.%s must be a list", mv.Line, name, mk)
					}
					for _, it := range mv.Content {
						if it.Kind != yaml.ScalarNode || strings.TrimSpace(it.Value) == "" {
							return nil, fmt.Errorf("line %d: %s.match.%s holds a value that is not a name", it.Line, name, mk)
						}
						e.Match[mk] = append(e.Match[mk], strings.TrimSpace(it.Value))
					}
				}
			case s.has(s.Actions, k):
				if v.Kind != yaml.ScalarNode || Rank(v.Value) >= len(Order) {
					return nil, fmt.Errorf("line %d: %s.%s: unknown policy %q (auto, ask, approve or deny)", v.Line, name, k, v.Value)
				}
				e.Policy[k] = v.Value
			case k == "approval_ttl":
				d, err := manifest.ParseDuration(v.Value)
				if err != nil || d <= 0 {
					return nil, fmt.Errorf("line %d: %s.approval_ttl: %q is not a duration", v.Line, name, v.Value)
				}
				if d > MaxTTL {
					return nil, fmt.Errorf("line %d: %s.approval_ttl %s is over the 24h maximum", v.Line, name, v.Value)
				}
				e.TTL = d
			case k == "require_merged":
				var b bool
				if err := v.Decode(&b); err != nil {
					return nil, fmt.Errorf("line %d: %s.require_merged must be true or false", v.Line, name)
				}
				e.RequireMerged, mergedSet = b, true
			default:
				known := append(append([]string{"match"}, s.Actions...), "approval_ttl", "require_merged")
				return nil, fmt.Errorf("line %d: %s: unknown key %q (known: %s)", body.Content[j].Line, name, k, strings.Join(known, ", "))
			}
		}
		for _, uk := range s.Unique {
			for _, val := range e.Match[uk] {
				key := uk + "\x00" + strings.ToLower(norm(uk, val))
				if other, dup := owner[key]; dup && other != name {
					return nil, fmt.Errorf("line %d: match.%s %q selects two environments (%s and %s)", e.Line, uk, val, other, name)
				}
				owner[key] = name
			}
		}
		out = append(out, s.defaults(e, mergedSet))
	}
	return out, nil
}

func (s Spec) defaults(e Env, mergedSet bool) Env {
	for _, a := range s.Actions {
		if e.Policy[a] == "" {
			e.Policy[a] = DefaultPolicy(e.Name, a)
		}
	}
	if !mergedSet {
		e.RequireMerged = strings.EqualFold(e.Name, "prod")
	}
	return e
}

// MergeRules are the tq-packs.yaml merge rules of the environments block,
// for the pack's RegisterPack Merge map: match lists are unions, policies
// stricter-wins, approval_ttl minimum (60m when the manifest is silent),
// require_merged can only be switched on. Load floors every value at the
// trusted manifest besides, so a repo file never loosens a resolution.
func (s Spec) MergeRules() map[string]packs.MergeRule {
	m := map[string]packs.MergeRule{
		"environments.*.match.*":        packs.Union(),
		"environments.*.approval_ttl":   packs.Min("60m"),
		"environments.*.require_merged": packs.Or(),
	}
	for _, a := range s.Actions {
		m["environments.*."+a] = packs.Ordered("", Order...)
	}
	return m
}

// Gate is the resolved environments of one call: the merged config (the
// manifest plus tq-packs.yaml) and the trusted manifest alone. Every answer
// is the stricter of the two, so the repo layer can add and tighten but
// never loosen.
type Gate struct {
	spec    Spec
	merged  set
	trusted set
}

type set struct {
	envs []Env
	def  string // default_environment as written ("" when unset)
}

func load(s Spec, st packs.Settings) set {
	envs, err := s.Parse(st.Node("environments"))
	if err != nil {
		// An invalid block never loads (the manifest validator refuses it);
		// if one gets here, fail closed: one environment that denies.
		envs = []Env{{Name: "invalid", Match: map[string][]string{}, Policy: map[string]string{}}}
		for _, a := range s.Actions {
			envs[0].Policy[a] = Deny
		}
	}
	return set{envs: envs, def: st.String("default_environment")}
}

// Load builds the gate from the trusted manifest's settings and the merged
// settings of the pack.
func Load(s Spec, trusted, merged packs.Settings) Gate {
	g := Gate{spec: s, merged: load(s, merged), trusted: load(s, trusted)}
	// default_environment is honoured from the manifest only.
	g.merged.def = g.trusted.def
	return g
}

// FromManifest is Load for a caller holding the trusted manifest and the
// merged config.
func FromManifest(s Spec, m *manifest.Manifest, merged packs.Config) Gate {
	var trusted packs.Settings
	if m != nil {
		trusted = packs.Merge(m.Packs, nil).Pack(s.Pack)
	}
	return Load(s, trusted, merged.Pack(s.Pack))
}

func (st set) find(name string) (Env, bool) {
	for _, e := range st.envs {
		if e.Name == name {
			return e, true
		}
	}
	return Env{}, false
}

// Envs lists the declared environment names (merged), sorted.
func (g Gate) Envs() []string {
	var out []string
	for _, e := range g.merged.envs {
		out = append(out, e.Name)
	}
	sort.Strings(out)
	return out
}

// Declared reports whether name is a declared environment (or NoEnv when
// none is declared).
func (g Gate) Declared(name string) bool {
	if len(g.merged.envs) == 0 {
		return name == NoEnv
	}
	_, ok := g.merged.find(name)
	return ok
}

// Env returns the merged environment.
func (g Gate) Env(name string) (Env, bool) { return g.merged.find(name) }

// Policy is the stricter of the merged and trusted value of action for env.
// An undeclared environment denies (fail closed).
func (g Gate) Policy(env, action string) string {
	if len(g.merged.envs) == 0 && env == NoEnv {
		return Ask
	}
	e, ok := g.merged.find(env)
	if !ok {
		return Deny
	}
	p := e.Policy[action]
	if p == "" {
		p = DefaultPolicy(env, action)
	}
	if t, ok := g.trusted.find(env); ok {
		tp := t.Policy[action]
		if tp == "" {
			tp = DefaultPolicy(env, action)
		}
		p = Stricter(p, tp)
	}
	return p
}

// TTL is the approval_ttl of env (0: the pack default), the minimum of the
// two layers.
func (g Gate) TTL(env string) time.Duration {
	var d time.Duration
	for _, st := range []set{g.merged, g.trusted} {
		if e, ok := st.find(env); ok && e.TTL > 0 && (d == 0 || e.TTL < d) {
			d = e.TTL
		}
	}
	return d
}

// RequireMerged is true when either layer requires it.
func (g Gate) RequireMerged(env string) bool {
	for _, st := range []set{g.merged, g.trusted} {
		if e, ok := st.find(env); ok && e.RequireMerged {
			return true
		}
	}
	return false
}

// Match returns env's match lists (merged).
func (g Gate) Match(env string) map[string][]string {
	if e, ok := g.merged.find(env); ok {
		return e.Match
	}
	return nil
}

// Owners returns the environments whose match key lists value.
func (g Gate) Owners(key, value string) []string {
	var out []string
	for _, e := range g.merged.envs {
		for _, m := range e.Match[key] {
			if matches(key, m, value) {
				out = append(out, e.Name)
				break
			}
		}
	}
	return out
}

// strictness orders environments: the given action's policy, then the
// strictest policy overall, then the name (stable).
func (g Gate) stricterEnv(a, b, action string) string {
	if a == "" {
		return b
	}
	if b == "" || a == b {
		return a
	}
	if action != "" {
		ra, rb := Rank(g.Policy(a, action)), Rank(g.Policy(b, action))
		if ra != rb {
			if rb > ra {
				return b
			}
			return a
		}
	}
	ma, mb := g.maxRank(a), g.maxRank(b)
	if ma != mb {
		if mb > ma {
			return b
		}
		return a
	}
	if b < a {
		return b
	}
	return a
}

func (g Gate) maxRank(env string) int {
	m := 0
	for _, act := range g.spec.Actions {
		if r := Rank(g.Policy(env, act)); r > m {
			m = r
		}
	}
	return m
}

// Default is the environment an unresolved target counts as: the
// manifest's default_environment when it names a declared environment,
// otherwise the strictest declared one (fail closed), or NoEnv when none is
// declared.
func (g Gate) Default() string {
	if len(g.merged.envs) == 0 {
		return NoEnv
	}
	if d := g.trusted.def; d != "" && g.Declared(d) {
		return d
	}
	best := ""
	for _, e := range g.merged.envs {
		best = g.stricterEnv(best, e.Name, "")
	}
	return best
}

// Signal is one piece of evidence about the target: a match key and the
// value the command carries (a dir, a stack name, a subscription id).
type Signal struct{ Key, Value string }

// Tier is a group of signals of one strength (§9.2: flags, then the dotenv
// wrapper, then variables, then local files).
type Tier []Signal

// Resolution is where a command goes.
type Resolution struct {
	Env      string
	Resolved bool   // false: no signal matched; Env is Default()
	By       Signal // the signal that selected Env
}

func (st set) resolve(g Gate, action string, tiers []Tier) (string, Signal) {
	for _, tier := range tiers {
		best, by := "", Signal{}
		for _, sig := range tier {
			for _, e := range st.envs {
				if hit(e, sig) {
					if pick := g.stricterEnv(best, e.Name, action); pick != best {
						best, by = pick, sig
					}
				}
			}
		}
		if best != "" {
			return best, by
		}
	}
	return "", Signal{}
}

func hit(e Env, sig Signal) bool {
	if sig.Value == "" {
		return false
	}
	if sig.Key == KeyName {
		return strings.EqualFold(e.Name, sig.Value)
	}
	for _, m := range e.Match[sig.Key] {
		if matches(sig.Key, m, sig.Value) {
			return true
		}
	}
	return false
}

// Resolve finds the environment of a command from its signal tiers: in the
// first tier with a hit, the stricter of the environments hit; with no hit,
// Default(). The trusted manifest is resolved too and the stricter result
// wins.
func (g Gate) Resolve(action string, tiers []Tier) Resolution {
	if len(g.merged.envs) == 0 {
		return Resolution{Env: NoEnv}
	}
	m, mby := g.merged.resolve(g, action, tiers)
	t, tby := g.trusted.resolve(g, action, tiers)
	if m == "" && t == "" {
		return Resolution{Env: g.Default()}
	}
	if t != "" && (m == "" || g.stricterEnv(m, t, action) == t) {
		return Resolution{Env: t, Resolved: true, By: tby}
	}
	return Resolution{Env: m, Resolved: true, By: mby}
}

// Norm cleans a value for comparison: slash paths without ./ and trailing /.
func norm(key, v string) string {
	v = strings.TrimSpace(v)
	switch key {
	case "dirs", "var_files", "dotenv":
		v = strings.ReplaceAll(v, `\`, "/")
		v = path.Clean(v)
		v = strings.TrimPrefix(v, "./")
	}
	return v
}

// matches compares a match entry m with a signal value v. Paths compare
// case-insensitively after cleaning; a dir matches itself and anything
// under it; a dotenv entry without a slash also matches by base name; a
// stack matches the whole name or its last /-segment (Pulumi org/project/
// stack); everything else is a case-insensitive equality.
func matches(key, m, v string) bool {
	m, v = norm(key, m), norm(key, v)
	switch key {
	case "dirs":
		lm, lv := strings.ToLower(m), strings.ToLower(v)
		return lv == lm || lm == "." || strings.HasPrefix(lv, strings.TrimSuffix(lm, "/")+"/")
	case "dotenv":
		if strings.EqualFold(m, v) {
			return true
		}
		return !strings.Contains(m, "/") && strings.EqualFold(path.Base(v), m)
	case "stacks":
		if strings.EqualFold(m, v) {
			return true
		}
		if i := strings.LastIndex(v, "/"); i >= 0 {
			return strings.EqualFold(m, v[i+1:])
		}
		return false
	}
	return strings.EqualFold(m, v)
}

// Matches is matches for callers outside the package (identity checks).
func Matches(key, m, v string) bool { return matches(key, m, v) }
