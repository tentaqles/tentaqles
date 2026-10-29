package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// stateEquivEnv isolates the homes with a CLAUDE_CONFIG_DIR inside the state
// directory (an inside variable), as the hook sees it.
func stateEquivEnv(tb interface {
	TempDir() string
	Setenv(string, string)
}) string {
	dir := tb.TempDir()
	home := filepath.Join(dir, ".tentaqles")
	tb.Setenv("TQ_HOME", home)
	tb.Setenv("HOME", dir)
	tb.Setenv("USERPROFILE", dir)
	tb.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "identities", "acme", "claude"))
	return home
}

// tableCommands collects every Command: string of the hook test tables
// (fixround2_test.go, pack_test.go, statedir_bench_test.go), evaluating
// literal concatenations, so the oracle check follows the tables as they grow.
func tableCommands(tb testing.TB) []string {
	tb.Helper()
	var eval func(ast.Expr) (string, bool)
	eval = func(e ast.Expr) (string, bool) {
		switch v := e.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				s, err := strconv.Unquote(v.Value)
				return s, err == nil
			}
		case *ast.BinaryExpr:
			if v.Op == token.ADD {
				a, ok1 := eval(v.X)
				b, ok2 := eval(v.Y)
				return a + b, ok1 && ok2
			}
		case *ast.ParenExpr:
			return eval(v.X)
		}
		return "", false
	}
	var out []string
	fs := token.NewFileSet()
	for _, name := range []string{"fixround2_test.go", "pack_test.go", "statedir_bench_test.go", "claudehook_round2_test.go"} {
		f, err := parser.ParseFile(fs, name, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if kv, ok := n.(*ast.KeyValueExpr); ok {
				if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Command" {
					if s, ok := eval(kv.Value); ok && s != "" {
						out = append(out, s)
					}
				}
			}
			return true
		})
	}
	if len(out) < 50 {
		tb.Fatalf("only %d table commands found", len(out))
	}
	return out
}

// quoteVariants are the spellings a shell folds back: quotes and backticks
// between the dots, inside the root name and after a slash.
func quoteVariants(s string) []string {
	v := []string{s}
	for _, q := range []string{"''", `""`, "``"} {
		v = append(v,
			strings.ReplaceAll(s, "..", "."+q+"."),
			strings.Replace(s, "tentaqles", "tenta"+q+"qles", 1),
			strings.Replace(s, "/", "/"+q, 1),
			strings.ReplaceAll(s, `\`, `\`+q),
		)
	}
	return v
}

// R5-2: the optimized protectedMention agrees with the pre-optimization
// oracle over every table command and its quote-inserted variants.
func TestProtectedMentionMatchesTheOracle(t *testing.T) {
	home := stateEquivEnv(t)
	cmds := tableCommands(t)
	cmds = append(cmds,
		"cat "+filepath.ToSlash(home)+"/packs/acme/ml/ledger.jsonl",
		`D=~/.tentaqles/identities; echo x >> $D/../packs/acme/ml/ledger.jsonl`,
		"cd ~/.tentaqles && ls; ls $TQ_HOME; cat $CLAUDE_CONFIG_DIR/notes.md | sort > /tmp/o; grep -n k some/file",
		"git commit -m \"see ~/.tentaqles/packs\" && ln -s a b",
	)
	n := 0
	for _, c := range cmds {
		for _, v := range quoteVariants(c) {
			n++
			if got, want := protectedMention(v), protectedMentionOracle(v); got != want {
				t.Errorf("protectedMention(%q) = %v, oracle %v", v, got, want)
			}
		}
	}
	t.Logf("%d commands compared", n)
}

func FuzzProtectedMention(f *testing.F) {
	stateEquivEnv(f)
	for _, c := range tableCommands(f) {
		f.Add(c)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 {
			return
		}
		if got, want := protectedMention(s), protectedMentionOracle(s); got != want {
			t.Fatalf("protectedMention(%q) = %v, oracle %v", s, got, want)
		}
	})
}

func FuzzRootMatcher(f *testing.F) {
	stateEquivEnv(f)
	for _, c := range tableCommands(f) {
		f.Add(normShell(c), "")
	}
	f.Add("x/tentaq~1/hub tentaq~ tentaq~12x", "/c/users/tester/.tentaqles")
	f.Fuzz(func(t *testing.T, s, extra string) {
		forms := append([]string{"c:/users/tester/.tentaqles", "~/.tentaqles", ".tentaqles", "tentaq~"}, stateDirForms()...)
		if extra != "" {
			if !utf8.ValidString(extra) {
				return // real forms are paths; the old regexp cannot even compile this
			}
			forms = append(forms, extra)
		}
		old, m := computeRootRegexOracle(forms), computeRootRegex(forms)
		if got, want := fmt.Sprint(m.FindAllStringIndex(s, -1)), fmt.Sprint(old.FindAllStringIndex(s, -1)); got != want {
			t.Fatalf("%q forms %q: got %s want %s", s, forms, got, want)
		}
		if got, want := fmt.Sprint(m.FindStringIndex(s)), fmt.Sprint(old.FindStringIndex(s)); got != want {
			t.Fatalf("%q first: got %s want %s", s, got, want)
		}
	})
}
