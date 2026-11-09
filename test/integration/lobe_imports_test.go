package integration

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// lobeMarker is the path fragment of every lobe package.
const lobeMarker = "/internal/lobe/"

// modulePrefix is the import prefix of this repository.
const modulePrefix = "github.com/tentaqles/tentaqles/"

// guardFiles are the guard's files in internal/cmd (the package also holds the lobe's own command file,
// so the rule is checked per file).
var guardFiles = []string{"claudehook.go", "claudehook_main.go", "codexhook.go"}

// lobeAllowed are the repository packages a lobe package may import, besides other internal/lobe packages.
var lobeAllowed = []string{
	"internal/resolve",
	"internal/paths",
	"internal/registry",
	"internal/manifest",
}

// repoRoot is the repository root, two levels above this package.
func repoRoot() string {
	return filepath.Join("..", "..")
}

// goList runs go list -deps for pkg from the repository root and returns its output lines. It fails the
// test if the command fails or runs past two minutes.
func goList(t *testing.T, args ...string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", append([]string{"list", "-deps", "-f", "{{.ImportPath}}"}, args...)...)
	cmd.Dir = repoRoot()
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("go list %v: timed out", args)
	}
	if err != nil {
		t.Fatalf("go list %v: %v\n%s", args, err, out)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// lobeFiles returns every non-test .go file under internal/lobe/, as paths relative to the repository root.
func lobeFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	root := filepath.Join(repoRoot(), "internal", "lobe")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no lobe source files found under internal/lobe")
	}
	return files
}

// importsOf parses the imports of one Go file.
func importsOf(t *testing.T, file string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var out []string
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("%s: import %s: %v", file, imp.Path.Value, err)
		}
		out = append(out, p)
	}
	return out
}

// Rule 1: the guard never reaches a lobe package, neither through its package nor through its files.
func TestLobeGuardNeverImportsLobe(t *testing.T) {
	for _, line := range goList(t, "./internal/guard") {
		if strings.Contains(line, lobeMarker) {
			t.Errorf("./internal/guard reaches lobe package %s", line)
		}
	}

	for _, name := range guardFiles {
		file := filepath.Join(repoRoot(), "internal", "cmd", name)
		for _, imp := range importsOf(t, file) {
			if strings.Contains(imp, lobeMarker) || strings.HasSuffix(imp, "/internal/lobe") {
				t.Errorf("%s imports lobe package %s directly", file, imp)
			}
			if !strings.HasPrefix(imp, modulePrefix) || imp == modulePrefix+"internal/cmd" {
				continue
			}
			path := "./" + strings.TrimPrefix(imp, modulePrefix)
			for _, line := range goList(t, path) {
				if strings.Contains(line, lobeMarker) {
					t.Errorf("%s imports %s, which reaches lobe package %s", file, imp, line)
				}
			}
		}
	}
}

// Rule 2: no lobe package runs anything at start-up: no init function, and no package-level variable
// whose initializer calls a function. A function literal that is only referenced is fine; calls inside
// its body run later, so they are not looked at.
func TestLobeNoInitAndNoStartupCalls(t *testing.T) {
	for _, file := range lobeFiles(t) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.Name == "init" {
					t.Errorf("%s:%d: lobe package has an init function", file, fset.Position(d.Pos()).Line)
				}
			case *ast.GenDecl:
				if d.Tok != token.VAR {
					continue
				}
				for _, spec := range d.Specs {
					vs := spec.(*ast.ValueSpec)
					for _, v := range vs.Values {
						if call := startupCall(v); call != nil {
							t.Errorf("%s:%d: package-level var %s is initialized by a call", file, fset.Position(call.Pos()).Line, varName(vs))
						}
					}
				}
			}
		}
	}
}

// startupCall returns the first call expression in a package-level initializer, or nil. It does not
// descend into function literals, whose bodies run only when called.
func startupCall(expr ast.Expr) *ast.CallExpr {
	var found *ast.CallExpr
	ast.Inspect(expr, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			found = x
			return false
		}
		return true
	})
	return found
}

// varName names the first identifier of a var spec, for a failure message.
func varName(vs *ast.ValueSpec) string {
	if len(vs.Names) == 0 {
		return "(unnamed)"
	}
	return vs.Names[0].Name
}

// Rule 3: a lobe package imports from this repository only the four allowed packages and other lobe packages.
func TestLobeImportsOnlyTheFour(t *testing.T) {
	for _, file := range lobeFiles(t) {
		for _, imp := range importsOf(t, file) {
			if !strings.HasPrefix(imp, modulePrefix) {
				continue
			}
			rel := strings.TrimPrefix(imp, modulePrefix)
			if strings.HasPrefix(rel, "internal/lobe/") {
				continue
			}
			ok := false
			for _, allowed := range lobeAllowed {
				if rel == allowed {
					ok = true
					break
				}
			}
			if !ok {
				t.Errorf("%s imports %s, which is not one of internal/resolve, internal/paths, internal/registry, internal/manifest or internal/lobe/...", file, imp)
			}
		}
	}
}

// Rule 2 is only meaningful if the hook is linked into the program: the hook must be in the binary's deps.
func TestLobeEmbeddedInOneBinary(t *testing.T) {
	want := modulePrefix + "internal/lobe/hook"
	for _, line := range goList(t, "./cmd/tentaqles") {
		if line == want {
			return
		}
	}
	t.Errorf("./cmd/tentaqles does not link %s", want)
}
