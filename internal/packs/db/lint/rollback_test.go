package lint

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func rb(t *testing.T, in RollbackInput) []string {
	t.Helper()
	var out []string
	for _, f := range Rollback(in) {
		if f.Message == "" {
			t.Errorf("empty message for %s", f.Rule)
		}
		out = append(out, fmt.Sprintf("%s:%s:%s", f.Rule, f.Level, f.File))
	}
	sort.Strings(out)
	return out
}

func files(m map[string]string) (func(string) ([]byte, error), []string) {
	var all []string
	for k := range m {
		all = append(all, k)
	}
	return func(p string) ([]byte, error) {
		v, ok := m[p]
		if !ok {
			return nil, fmt.Errorf("missing %s", p)
		}
		return []byte(v), nil
	}, all
}

func TestRollbackPairedMigrators(t *testing.T) {
	cases := []struct {
		name     string
		migrator string
		fs       map[string]string
		changed  []string
		want     []string
	}{
		{"golang-migrate missing down", "golang-migrate",
			map[string]string{"db/1_a.up.sql": "create table a(id int);"}, []string{"db/1_a.up.sql"},
			[]string{"RB001:error:db/1_a.up.sql"}},
		{"golang-migrate empty down", "golang-migrate",
			map[string]string{"db/1_a.up.sql": "x", "db/1_a.down.sql": "-- nothing\n"}, []string{"db/1_a.up.sql", "db/1_a.down.sql"},
			[]string{"RB002:error:db/1_a.down.sql"}},
		{"golang-migrate ok", "golang-migrate",
			map[string]string{"db/1_a.up.sql": "x", "db/1_a.down.sql": "drop table a;"}, []string{"db/1_a.up.sql", "db/1_a.down.sql"},
			nil},
		{"dbmate no down", "dbmate",
			map[string]string{"db/migrations/20240101_a.sql": "-- migrate:up\ncreate table a(id int);\n"}, []string{"db/migrations/20240101_a.sql"},
			[]string{"RB001:error:db/migrations/20240101_a.sql"}},
		{"dbmate empty down", "dbmate",
			map[string]string{"db/migrations/20240101_a.sql": "-- migrate:up\ncreate table a(id int);\n-- migrate:down\n"}, []string{"db/migrations/20240101_a.sql"},
			[]string{"RB002:error:db/migrations/20240101_a.sql"}},
		{"dbmate ok", "dbmate",
			map[string]string{"db/migrations/20240101_a.sql": "-- migrate:up\ncreate table a(id int);\n-- migrate:down\ndrop table a;\n"}, []string{"db/migrations/20240101_a.sql"},
			nil},
		{"flyway no undo is a warn", "flyway",
			map[string]string{"sql/V2__add.sql": "x", "sql/U1__old.sql": "y"}, []string{"sql/V2__add.sql"},
			[]string{"RB001:warn:sql/V2__add.sql"}},
		{"flyway ok", "flyway",
			map[string]string{"sql/V2__add.sql": "x", "sql/U2__add.sql": "y", "sql/R__views.sql": "z"}, []string{"sql/V2__add.sql", "sql/R__views.sql"},
			nil},
		{"sqitch no revert", "sqitch",
			map[string]string{"db/deploy/orders.sql": "x", "db/verify/orders.sql": "y"}, []string{"db/deploy/orders.sql"},
			[]string{"RB001:error:db/deploy/orders.sql"}},
		{"sqitch ok", "sqitch",
			map[string]string{"db/deploy/orders.sql": "x", "db/revert/orders.sql": "drop table orders;"}, []string{"db/deploy/orders.sql"},
			nil},
		{"alembic pass", "alembic",
			map[string]string{"alembic/versions/abc_add.py": "def upgrade():\n    op.add_column('t', sa.Column('x'))\n\n\ndef downgrade():\n    \"\"\"Nothing.\"\"\"\n    # no-op\n    pass\n"}, []string{"alembic/versions/abc_add.py"},
			[]string{"RB002:error:alembic/versions/abc_add.py"}},
		{"alembic ok", "alembic",
			map[string]string{"alembic/versions/abc_add.py": "def upgrade() -> None:\n    op.add_column('t', sa.Column('x'))\n\ndef downgrade() -> None:\n    op.drop_column('t', 'x')\n"}, []string{"alembic/versions/abc_add.py"},
			nil},
		{"alembic no downgrade", "alembic",
			map[string]string{"alembic/versions/abc_add.py": "def upgrade():\n    pass\n"}, []string{"alembic/versions/abc_add.py"},
			[]string{"RB001:error:alembic/versions/abc_add.py"}},
		{"efcore empty Down", "efcore",
			map[string]string{"Migrations/20240101_Add.cs": "public partial class Add : Migration {\n protected override void Up(MigrationBuilder migrationBuilder) { migrationBuilder.AddColumn<int>(\"X\"); }\n protected override void Down(MigrationBuilder migrationBuilder)\n {\n   // nothing\n }\n}", "Migrations/20240101_Add.Designer.cs": "x"}, []string{"Migrations/20240101_Add.cs", "Migrations/20240101_Add.Designer.cs"},
			[]string{"RB002:error:Migrations/20240101_Add.cs"}},
		{"efcore ok", "efcore",
			map[string]string{"Migrations/20240101_Add.cs": "protected override void Down(MigrationBuilder migrationBuilder)\n{\n  migrationBuilder.DropColumn(name: \"X\", table: \"T\");\n}\n"}, []string{"Migrations/20240101_Add.cs"},
			nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			read, all := files(c.fs)
			got := rb(t, RollbackInput{Migrator: c.migrator, Changed: c.changed, Files: all, Read: read})
			if len(got) != len(c.want) || (len(got) > 0 && !reflect.DeepEqual(got, c.want)) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestRollbackForwardOnlyNeedsSpecSection(t *testing.T) {
	read, all := files(map[string]string{"supabase/migrations/20240101_a.sql": "x"})
	in := RollbackInput{Migrator: "supabase", Changed: []string{"supabase/migrations/20240101_a.sql"}, Files: all, Read: read}
	if got := rb(t, in); !reflect.DeepEqual(got, []string{"RB003:error:supabase/migrations/20240101_a.sql"}) {
		t.Fatalf("no spec: %v", got)
	}
	in.Specs = map[string][]byte{"docs/specs/acme.md": []byte("# Acme\n## Migration plan\nexpand first\n## Rollback\nnot nested\n")}
	if got := rb(t, in); len(got) != 1 {
		t.Fatalf("a ## Rollback outside the plan does not count: %v", got)
	}
	in.Specs = map[string][]byte{"docs/specs/acme.md": []byte("# Acme\n\n## Migration plan\n\nexpand first\n\n### Rollback\n\ndrop the column\n\n## Tests\n")}
	if got := rb(t, in); len(got) != 0 {
		t.Fatalf("with the section: %v", got)
	}
}
