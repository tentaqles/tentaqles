package gate

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// Integration round 3: a Supabase global flag tq does not know, written
// before the subcommand, must not hide db push, db reset or migration up.
// The root is linked to dev (migrate: auto); the strictest environment is
// prod (migrate: approve), where an unreadable migration is denied.
func TestSupabaseUnknownGlobalFlag(t *testing.T) {
	cases := []struct {
		cmd  string
		want policy.Action
	}{
		{"supabase db push", policy.Allow},
		// the documented globals, value and boolean, keep the resolution
		{"supabase --agent no db push", policy.Allow},
		{"supabase --dns-resolver https db push", policy.Allow},
		{"supabase --yes --experimental db push", policy.Allow},
		{"supabase --profile acme -o json db push", policy.Allow},
		{"supabase --workdir=. db push", policy.Allow},
		// an unknown flag before a non-flag word: the strictest environment
		{"supabase --unknown x db push", policy.Deny},
		{"supabase --unknown x migration up", policy.Deny},
		{"supabase -Z x y db push", policy.Deny},
		{"supabase --unknown x --debug db push", policy.Deny},
		{"supabase --unknown x projects list", policy.Deny},
		{"npx supabase --unknown x db push", policy.Deny},
	}
	for _, c := range cases {
		t.Run(c.cmd, func(t *testing.T) {
			f := newMonorepo(t)
			a, r, _ := f.migrate("Bash", c.cmd)
			if a != c.want {
				t.Fatalf("got %s (%s), want %s", a, r, c.want)
			}
			if c.want == policy.Deny && !strings.Contains(r, "prod") {
				t.Fatalf("the floor is the strictest environment (prod): %s", r)
			}
		})
	}
}

// db reset --linked behind an unknown global flag is still a restore.
func TestSupabaseUnknownGlobalFlagReset(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		deny bool
	}{
		{"supabase --unknown x db reset --linked", true},
		{"supabase --agent yes db reset --linked", true},
		{"supabase --unknown x db reset", false},
	} {
		t.Run(c.cmd, func(t *testing.T) {
			f := newFixture(t, dbBlock)
			a, r, _ := f.restore("Bash", c.cmd)
			if (a == policy.Deny) != c.deny {
				t.Fatalf("got %s (%s), want deny=%v", a, r, c.deny)
			}
		})
	}
}

// pipedPsql: a -c or -l that belongs to another command in the same
// opaque payload does not count for the psql that reads the pipe.
func TestPipedPsqlOwnWords(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		deny bool
	}{
		{`gunzip -c x.gz | ssh box 'psql acme && psql -l -q'`, true},
		{`gunzip -c x.gz | ssh box 'psql -l -q; psql acme'`, true},
		{`gunzip -c x.gz | ssh box "psql acme; echo -c x"`, true},
		{`gunzip -c x.gz | ssh box psql acme -c x`, false},
		{`gunzip -c x.gz | ssh box 'psql -c "select 1"'`, false},
		{`ssh box psql -l | grep acme`, false},
	} {
		t.Run(c.cmd, func(t *testing.T) {
			f := newFixture(t, dbBlock)
			a, r, _ := f.restore("Bash", c.cmd)
			if (a == policy.Deny) != c.deny {
				t.Fatalf("got %s (%s), want deny=%v", a, r, c.deny)
			}
		})
	}
}
