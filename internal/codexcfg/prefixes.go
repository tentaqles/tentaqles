package codexcfg

import "sort"

// Token is one position of a Codex prefix_rule pattern: a single word, or
// alternatives at that position.
type Token []string

// Pattern is a Codex prefix_rule pattern: the command's first words.
type Pattern []Token

// Match reports whether words starts with p.
func (p Pattern) Match(words []string) bool {
	if len(p) == 0 || len(words) < len(p) {
		return false
	}
	for i, tok := range p {
		hit := false
		for _, w := range tok {
			if words[i] == w {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

func (p Pattern) equal(q Pattern) bool {
	if len(p) != len(q) {
		return false
	}
	for i := range p {
		if len(p[i]) != len(q[i]) {
			return false
		}
		for j := range p[i] {
			if p[i][j] != q[i][j] {
				return false
			}
		}
	}
	return true
}

func w(s ...string) Token { return Token(s) }

// prefixes is the curated table (spec E §12) of the tq rules that have an
// exact prefix form in Codex's exec policy. A rule whose match sits anywhere
// in the command (a flag such as gh pr merge --admin, a path, a regex over
// the whole line) has no entry: the hook alone handles it, and in Codex an
// ask of such a rule is a deny.
//
// Each pattern must only match commands the rule itself matches
// (TestPrefixesAgreeWithTheirRules). Locked categories never appear: their
// asks are hook denies in Codex.
var prefixes = map[string][]Pattern{
	"tq/gh-repo-delete": {{w("gh"), w("repo"), w("delete")}},
	"tq/force-push":     {{w("git"), w("push"), w("--force", "-f")}},
	"tq/iac-destroy":    {{w("terraform", "tofu", "pulumi", "cdk", "sst"), w("destroy")}},
	"tq/iac-apply": {
		{w("terraform", "tofu"), w("apply")},
		{w("pulumi"), w("up")},
		{w("cdk", "cdktf", "sam", "serverless", "sls"), w("deploy")},
		{w("kubectl"), w("apply")},
		{w("helm"), w("install", "upgrade")},
		{w("aws"), w("cloudformation"), w("deploy")},
	},
	"tq/remote-migration": {
		{w("prisma"), w("migrate"), w("deploy")},
		{w("alembic"), w("upgrade")},
		{w("flyway"), w("migrate")},
		{w("liquibase"), w("update")},
		{w("dbmate", "goose"), w("up")},
		{w("knex"), w("migrate:latest")},
		{w("sqitch"), w("deploy")},
		{w("dotnet"), w("ef"), w("database"), w("update")},
		{w("supabase"), w("db"), w("push")},
		{w("supabase"), w("migration"), w("up")},
		{w("drizzle-kit"), w("push", "migrate")},
		{w("atlas"), w("migrate"), w("apply")},
		{w("rails"), w("db:migrate")},
		{w("php"), w("artisan"), w("migrate")},
		{w("python", "python3"), w("manage.py"), w("migrate")},
	},
	"tq/registry-write": {{w("reg", "reg.exe"), w("add", "delete", "import")}},
}

// PrefixIDs returns the rule ids that have prefix patterns, sorted.
func PrefixIDs() []string {
	out := make([]string, 0, len(prefixes))
	for id := range prefixes {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Prefixes returns a copy of id's patterns (nil: hook only).
func Prefixes(id string) []Pattern {
	ps := prefixes[id]
	if ps == nil {
		return nil
	}
	out := make([]Pattern, len(ps))
	for i, p := range ps {
		q := make(Pattern, len(p))
		for j, tok := range p {
			q[j] = append(Token(nil), tok...)
		}
		out[i] = q
	}
	return out
}
