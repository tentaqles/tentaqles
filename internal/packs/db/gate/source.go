package gate

import (
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/lint"
)

// An approval binds the content of packs.db.migrations[].path (EvMigrations).
// A command that makes its migrator read migrations from somewhere else
// would run migrations the human never saw, so under migrate: approve every
// source selector on the command must name a folder inside those paths.

// dirSelectors are flags whose value is the migrations folder itself, by
// program ("" applies to every program).
var dirSelectors = map[string][]string{
	"":        {"-path", "--path"}, // golang-migrate, artisan
	"migrate": {"-source", "--source"},
	"dbmate":  {"--migrations-dir", "-d"},
	"goose":   {"-dir", "--dir"},
	"atlas":   {"--dir"},
	"knex":    {"--migrations-directory"},
	"flyway":  {"-locations", "--locations"},
}

// configSelectors are flags that pick a configuration file, which can name
// any migrations folder: tq cannot bind them, so under approve they deny.
var configSelectors = map[string][]string{
	"":            {"--settings"}, // django manage.py
	"alembic":     {"-c", "--config", "-n", "--name"},
	"drizzle-kit": {"--config"},
	"knex":        {"--knexfile"},
	"liquibase":   {"--changelog-file", "--changeLogFile", "--defaults-file", "--defaultsFile"},
	"flyway":      {"-configFiles", "--configFiles", "-workingDirectory"},
	"atlas":       {"--config", "-c"},
	"sqitch":      {"-C", "--chdir", "--top-dir", "--plan-file", "-f"},
}

// envSelectors are variables that select the migrations folder.
var envSelectors = map[string]string{"DBMATE_MIGRATIONS_DIR": "dbmate", "FLYWAY_LOCATIONS": "flyway", "DJANGO_SETTINGS_MODULE": ""}

// flagValues returns every value of the named flags (-f v, -f=v, -f:v for
// one-dash names).
func flagValues(l Leaf, names ...string) []string {
	var out []string
	for i, a := range l.Args {
		for _, n := range names {
			switch {
			case a == n:
				v := ""
				if i+1 < len(l.Args) {
					v = l.Args[i+1]
				}
				out = append(out, v)
			case strings.HasPrefix(a, n+"="):
				out = append(out, a[len(n)+1:])
			}
		}
	}
	return out
}

// unboundSource returns why a migration leaf may read migrations outside
// the bound paths, or "" when every selector it names is inside them.
func unboundSource(l Leaf, g packs.GuardCtx) string {
	var roots []string
	for _, m := range lint.Migrations(g.Settings()) {
		r := path.Clean(strings.TrimPrefix(strings.ReplaceAll(m.Path, `\`, "/"), "./"))
		roots = append(roots, r)
	}
	dir, ok := leafDir(l, g)
	if !ok {
		return "the command moves to a folder tq cannot follow"
	}
	if !samePath(dir, g.Cwd()) {
		return "the command runs in another folder (" + l.Dir + ") than the one the approval was asked in, so its migrations are not the approved ones; run it from the repository's folder"
	}
	repo := g.RepoRoot()
	if repo == "" {
		repo = g.WorkspaceRoot()
	}
	if fp, err := packs.FinalPath(repo); err == nil {
		repo = fp
	}
	inside := func(p string) bool {
		if computed(p) {
			return false
		}
		p = strings.ReplaceAll(p, `\`, "/")
		abs := filepath.FromSlash(p)
		if !filepath.IsAbs(abs) {
			if isAbs(p) {
				return false // a slash path on Windows: not inside the repository
			}
			abs = filepath.Join(dir, abs)
		}
		rel, err := filepath.Rel(repo, abs)
		if err != nil {
			return false
		}
		rel = filepath.ToSlash(rel)
		if runtime.GOOS == "windows" {
			rel = strings.ToLower(rel)
		}
		if rel == ".." || strings.HasPrefix(rel, "../") {
			return false
		}
		for _, r := range roots {
			if runtime.GOOS == "windows" {
				r = strings.ToLower(r)
			}
			if rel == r || strings.HasPrefix(rel, r+"/") {
				return true
			}
		}
		return false
	}
	why := func(sel string) string {
		return "`" + sel + "` names a migrations source outside the approved paths (" + strings.Join(roots, ", ") + "), so the approval does not cover what it runs"
	}
	for _, prog := range []string{"", l.Prog} {
		for _, v := range flagValues(l, dirSelectors[prog]...) {
			for _, loc := range strings.Split(v, ",") {
				loc = strings.TrimPrefix(loc, "filesystem:")
				if strings.HasPrefix(loc, "file://") {
					loc = strings.TrimPrefix(loc, "file://")
				} else if strings.Contains(loc, "://") || strings.HasPrefix(loc, "classpath:") {
					return why(v)
				}
				if !inside(loc) {
					return why(loc)
				}
			}
		}
	}
	for _, prog := range []string{"", l.Prog} {
		if names := configSelectors[prog]; len(names) > 0 {
			for _, v := range flagValues(l, names...) {
				if l.Prog == "alembic" && (v == "alembic.ini" || v == "./alembic.ini") {
					continue // the default
				}
				return "`" + v + "` is a configuration file that can name any migrations folder; tq cannot bind it to the approval, so run the migrator with its default configuration"
			}
		}
	}
	for k, prog := range envSelectors {
		if prog != "" && prog != l.Prog {
			continue
		}
		if v := lookup(l, k); v != "" {
			if prog == "" || !inside(strings.TrimPrefix(v, "filesystem:")) {
				return "the variable " + k + " selects the migrations source; tq cannot bind it to the approval"
			}
		}
	}
	switch l.Prog {
	case "supabase":
		wd, _ := l.Flag("--workdir")
		if wd == "" {
			wd = lookup(l, "SUPABASE_WORKDIR")
		}
		if wd != "" && !inside(path.Join(strings.ReplaceAll(wd, `\`, "/"), "supabase", "migrations")) {
			return why(wd)
		}
	case "prisma":
		for _, s := range flagValues(l, "--schema") {
			s = strings.ReplaceAll(s, `\`, "/")
			if !inside(path.Join(path.Dir(s), "migrations")) {
				return why(s)
			}
		}
	}
	return ""
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
