package drill

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// VersionQuery is the query that reads the newest applied migration of a
// migrator from its version table, per engine ("" when tq does not know the
// table: the version is recorded as unknown).
func VersionQuery(engine, migrator string) string {
	if engine == SQLServer {
		switch migrator {
		case "efcore":
			return "SELECT TOP 1 MigrationId FROM __EFMigrationsHistory ORDER BY MigrationId DESC"
		case "flyway":
			return "SELECT TOP 1 version FROM flyway_schema_history WHERE success = 1 AND version IS NOT NULL ORDER BY installed_rank DESC"
		case "golang-migrate", "dbmate":
			return "SELECT TOP 1 CAST(version AS nvarchar(64)) FROM schema_migrations ORDER BY version DESC"
		}
		return ""
	}
	switch migrator {
	case "supabase":
		return "select max(version) from supabase_migrations.schema_migrations"
	case "prisma":
		return "select max(migration_name) from _prisma_migrations where finished_at is not null and rolled_back_at is null"
	case "golang-migrate":
		return "select max(version)::text from schema_migrations where not dirty"
	case "dbmate":
		return "select max(version) from schema_migrations"
	case "flyway":
		return "select version from flyway_schema_history where success and version is not null order by installed_rank desc limit 1"
	case "alembic":
		return "select version_num from alembic_version limit 1"
	case "sqitch":
		return "select change from sqitch.changes order by committed_at desc limit 1"
	}
	return ""
}

var (
	leadDigits = regexp.MustCompile(`^([0-9]+)`)
	flywayV    = regexp.MustCompile(`^V([0-9]+(?:[._][0-9]+)*)__`)
)

// FileVersion is the version a migration file or folder name stands for,
// in the form the migrator's version table stores ("" when it has none or
// the migrator's files carry no orderable version).
func FileVersion(migrator, name string) string {
	base := path.Base(strings.ReplaceAll(name, `\`, "/"))
	switch migrator {
	case "supabase", "golang-migrate", "dbmate":
		if m := leadDigits.FindStringSubmatch(base); m != nil {
			return strings.TrimLeft(m[1], "0")
		}
	case "prisma":
		if leadDigits.MatchString(base) && !strings.Contains(base, ".") {
			return base // the migration folder name is migration_name
		}
	case "efcore":
		if strings.HasSuffix(base, ".cs") && !strings.HasSuffix(base, ".Designer.cs") && leadDigits.MatchString(base) {
			return strings.TrimSuffix(base, ".cs")
		}
	case "flyway":
		if m := flywayV.FindStringSubmatch(base); m != nil {
			return strings.ReplaceAll(m[1], "_", ".")
		}
	}
	return ""
}

// NormVersion is a version as stored, for comparison with FileVersion.
func NormVersion(migrator, v string) string {
	v = strings.TrimSpace(v)
	switch migrator {
	case "supabase", "golang-migrate", "dbmate":
		return strings.TrimLeft(v, "0")
	}
	return v
}

// Newer reports whether version a sorts after b: numerically per dotted
// segment when both are numeric, else as strings.
func Newer(a, b string) bool {
	if b == "" {
		return a != ""
	}
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		x, y := "0", "0"
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x == y {
			continue
		}
		xn, e1 := strconv.ParseUint(x, 10, 64)
		yn, e2 := strconv.ParseUint(y, 10, 64)
		if e1 == nil && e2 == nil {
			return xn > yn
		}
		if len(x) != len(y) && isDigits(x) && isDigits(y) {
			return len(x) > len(y)
		}
		return x > y
	}
	return false
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// NewestFileVersion is the newest version among the names.
func NewestFileVersion(migrator string, names []string) string {
	best := ""
	for _, n := range names {
		if v := FileVersion(migrator, n); v != "" && Newer(v, best) {
			best = v
		}
	}
	return best
}
