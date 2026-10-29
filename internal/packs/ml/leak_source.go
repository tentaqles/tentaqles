package ml

// Shared helpers of the M1 lints (spec C §4.2, §4.3): loading a Python file
// or notebook through the framework scanners, call sites with argument
// positions, string literals by position, assignments, inline suppressions,
// name segments and the training-file selection.

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
)

// maxFiles caps the files one check scans (the built-in budget is 10 s). A
// variable so a test can lower it; a check that hits it says so with a
// scan-limit finding.
var maxFiles = 500

const (
	// maxFileBytes caps one scanned file.
	maxFileBytes = 8 << 20
	// maxFeatureFiles caps the paths.features files read for L7.
	maxFeatureFiles = 200
	// flagProbeFiles and flagProbeBytes bound the ml flag's content probe.
	flagProbeFiles = 50
	flagProbeBytes = 1 << 20
)

// pyFile is one scanned Python source: a .py file, or a notebook whose code
// cells are joined in order.
type pyFile struct {
	Path     string
	Lines    []scan.PyLine
	Calls    [][]pcall // per line
	Imports  []scan.Import
	Notebook bool
	// fileLine maps a joined line to the file line; cellLine to (cell, line
	// in cell), both 1-based. Nil for a plain .py file.
	fileLine func(int) int
	cellLine func(int) (int, int)
	consts   map[string]string   // name = "literal"
	lists    map[string][]string // name = ["a", "b"]
	fromImp  map[string]string   // from-imported local name -> module
	fromName map[string]string   // from-imported local name -> imported name (`load as jl_load`)
	modAlias map[string]string   // `import pickle as pkl`: pkl -> pickle
}

// resolveRecv rewrites the first segment of a call receiver through the
// module aliases: `pkl` -> `pickle`, `jl.x` -> `joblib.x`.
func (f *pyFile) resolveRecv(recv string) string {
	head, rest := recv, ""
	if i := strings.Index(recv, "."); i >= 0 {
		head, rest = recv[:i], recv[i:]
	}
	if m, ok := f.modAlias[head]; ok {
		return m + rest
	}
	return recv
}

// fromImports parses the from-imports of one logical line with their local
// names: `from joblib import load as jl_load` -> {jl_load: load}. The
// framework's scan.Import keeps the imported names only.
func fromImports(code string) (module string, local map[string]string) {
	if !strings.HasPrefix(code, "from ") {
		return "", nil
	}
	rest := strings.TrimPrefix(code, "from ")
	i := strings.Index(rest, " import ")
	if i < 0 {
		return "", nil
	}
	module = strings.TrimSpace(rest[:i])
	local = map[string]string{}
	for _, n := range strings.Split(strings.Trim(strings.TrimSpace(rest[i+8:]), "()"), ",") {
		switch fs := strings.Fields(n); {
		case len(fs) == 3 && fs[1] == "as":
			local[fs[2]] = fs[0]
		case len(fs) >= 1:
			local[fs[0]] = fs[0]
		}
	}
	return module, local
}

// loadPy reads and parses one changed file.
func loadPy(in *packs.Input, p string) (*pyFile, error) {
	b, err := in.ReadNew(p, maxFileBytes)
	if err != nil {
		return nil, err
	}
	return parsePy(p, b)
}

// parsePy parses a .py file or a notebook (.ipynb, Fabric, Synapse).
func parsePy(p string, b []byte) (*pyFile, error) {
	f := &pyFile{Path: p}
	src := b
	if scan.DetectNotebook(p, b) != "" {
		cells, err := scan.NotebookCells(p, b)
		if err != nil {
			return nil, err
		}
		src, f.fileLine, f.cellLine = joinCells(cells)
		f.Notebook = true
	}
	f.Lines = scan.PythonLines(src)
	f.Imports = scan.PythonImports(src)
	f.Calls = make([][]pcall, len(f.Lines))
	f.consts = map[string]string{}
	f.lists = map[string][]string{}
	f.fromImp = map[string]string{}
	f.fromName = map[string]string{}
	f.modAlias = map[string]string{}
	for i, l := range f.Lines {
		if mod, local := fromImports(l.Code); mod != "" {
			for ln, orig := range local {
				f.fromImp[ln] = mod
				f.fromName[ln] = orig
			}
		}
		f.Calls[i] = lineCalls(l)
		if lhs, rhs, from, ok := splitAssign(l.Code); ok && len(lhs) == 1 {
			r := strings.TrimSpace(rhs)
			switch {
			case isStrLit(r):
				if v := stringsIn(l, from, len(l.Code)); len(v) == 1 {
					f.consts[lhs[0]] = v[0]
				}
			case strings.HasPrefix(r, "[") && strings.HasSuffix(r, "]") && onlyStrings(r[1:len(r)-1]):
				f.lists[lhs[0]] = stringsIn(l, from, len(l.Code))
			}
		}
	}
	for _, im := range f.Imports {
		if im.Alias != "" && len(im.Names) == 0 {
			f.modAlias[im.Alias] = im.Module
		}
	}
	return f, nil
}

// joinCells joins the Python code cells like scan.CodeSource and also maps
// each joined line to its cell and line in the cell.
func joinCells(cells []scan.Cell) ([]byte, func(int) int, func(int) (int, int)) {
	var b strings.Builder
	type pos struct{ file, cell, line int }
	var m []pos
	for _, c := range cells {
		if c.Kind != "code" && c.Kind != "parameters" {
			continue
		}
		if l := strings.ToLower(c.Language); l != "" && l != "python" && l != "pyspark" && l != "python3" {
			continue
		}
		lines := strings.Split(c.Source, "\n")
		for i, l := range lines {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "%") || strings.HasPrefix(t, "!") {
				l = ""
			}
			b.WriteString(l)
			b.WriteByte('\n')
			fl := c.Line
			if c.LinesExact {
				fl = c.Line + i
			}
			m = append(m, pos{fl, c.Index + 1, i + 1})
		}
		b.WriteByte('\n') // cells never join into one logical line
		m = append(m, pos{c.Line, c.Index + 1, len(lines)})
	}
	get := func(n int) pos {
		if n >= 1 && n <= len(m) {
			return m[n-1]
		}
		return pos{}
	}
	return []byte(b.String()), func(n int) int { return get(n).file }, func(n int) (int, int) { p := get(n); return p.cell, p.line }
}

// where returns the file line of joined line n and the message prefix
// ("cell N, line M: " for a notebook).
func (f *pyFile) where(n int) (int, string) {
	if f.fileLine == nil {
		return n, ""
	}
	c, l := f.cellLine(n)
	return f.fileLine(n), fmt.Sprintf("cell %d, line %d: ", c, l)
}

// find builds a finding at line index i of f.
func (f *pyFile) find(i int, level, rule, msg string) packs.Finding {
	n := 1
	if i >= 0 && i < len(f.Lines) {
		n = f.Lines[i].Line
	}
	line, pre := f.where(n)
	return packs.Finding{File: f.Path, Line: line, Level: level, Rule: rule, Message: pre + msg}
}

// --- suppressions -----------------------------------------------------------

var allowRe = regexp.MustCompile(`tq:allow\s+([A-Za-z0-9_./-]+?)\s*:\s*(\S[^#]*)?`)

// allows reports whether a comment carries `tq:allow <key>: <reason>` for
// one of keys. A suppression without a reason does not count.
func allows(comment string, keys []string) bool {
	if !strings.Contains(comment, "tq:allow") {
		return false
	}
	for _, m := range allowRe.FindAllStringSubmatch(comment, -1) {
		if strings.TrimSpace(m[2]) == "" {
			continue
		}
		for _, k := range keys {
			if strings.EqualFold(m[1], k) {
				return true
			}
		}
	}
	return false
}

// suppressed reports whether line i (or the comment-only line just above
// it) suppresses a finding with one of keys. i < 0 means file level: any
// comment-only line of the file counts.
func (f *pyFile) suppressed(i int, keys ...string) bool {
	if i < 0 {
		for _, l := range f.Lines {
			if l.Code == "" && allows(l.Comment, keys) {
				return true
			}
		}
		return false
	}
	if i < len(f.Lines) && allows(f.Lines[i].Comment, keys) {
		return true
	}
	if i > 0 && i <= len(f.Lines) {
		p := f.Lines[i-1]
		if p.Code == "" && allows(p.Comment, keys) {
			return true
		}
	}
	return false
}

// --- call sites -------------------------------------------------------------

// arg is one argument of a call: Key is "" for a positional one. From/To
// are byte offsets in the line's Code.
type arg struct {
	Key, Text string
	From, To  int
}

// pcall is a call site with argument positions (scan.Call has none).
type pcall struct {
	Func, Receiver, Name string
	From, Open, Close    int // callee start, '(' and ')' offsets in Code
	Args                 []arg
}

func (c pcall) pos() []arg {
	var out []arg
	for _, a := range c.Args {
		if a.Key == "" {
			out = append(out, a)
		}
	}
	return out
}

func (c pcall) kw(k string) (arg, bool) {
	for _, a := range c.Args {
		if a.Key == k {
			return a, true
		}
	}
	return arg{}, false
}

// argN returns positional argument n, or the keyword key when given.
func (c pcall) argN(n int, key string) (arg, bool) {
	if key != "" {
		if a, ok := c.kw(key); ok {
			return a, true
		}
	}
	p := c.pos()
	if n < len(p) {
		return p[n], true
	}
	return arg{}, false
}

func (c pcall) hasStar() bool {
	for _, a := range c.Args {
		if strings.HasPrefix(a.Text, "*") {
			return true
		}
	}
	return false
}

var calleeRe = regexp.MustCompile(`([A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*)*)\s*\(`)

var notCallee = map[string]bool{"if": true, "elif": true, "while": true, "for": true, "return": true, "and": true, "or": true,
	"not": true, "in": true, "is": true, "with": true, "assert": true, "yield": true, "del": true, "lambda": true,
	"def": true, "class": true, "except": true, "await": true, "import": true, "from": true, "as": true, "raise": true}

// lineCalls returns the call sites of a logical line, with positions.
func lineCalls(l scan.PyLine) []pcall {
	code := l.Code
	if strings.HasPrefix(code, "def ") || strings.HasPrefix(code, "class ") || strings.HasPrefix(code, "async def ") {
		return nil
	}
	var out []pcall
	for _, m := range calleeRe.FindAllStringSubmatchIndex(code, -1) {
		callee := strings.ReplaceAll(code[m[2]:m[3]], " ", "")
		if notCallee[callee] {
			continue
		}
		fn := callee
		if m[2] > 0 && code[m[2]-1] == '.' {
			fn = "." + callee
		}
		c := pcall{Func: fn, From: m[2], Open: m[1] - 1}
		if i := strings.LastIndex(fn, "."); i >= 0 {
			c.Receiver, c.Name = fn[:i], fn[i+1:]
		} else {
			c.Name = fn
		}
		c.Close, c.Args = splitArgs(code, c.Open)
		out = append(out, c)
	}
	return out
}

// splitArgs splits the argument list at code[open] == '('.
func splitArgs(code string, open int) (int, []arg) {
	depth, start, end := 0, open+1, len(code)
	var spans [][2]int
	for i := open; i < len(code); i++ {
		switch code[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				spans = append(spans, [2]int{start, i})
				end = i
				i = len(code)
			}
		case ',':
			if depth == 1 {
				spans = append(spans, [2]int{start, i})
				start = i + 1
			}
		}
	}
	var out []arg
	for _, s := range spans {
		raw := code[s[0]:s[1]]
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		from := s[0] + strings.Index(raw, t)
		if eq := kwargIndex(t); eq > 0 {
			v := strings.TrimSpace(t[eq+1:])
			out = append(out, arg{Key: strings.TrimSpace(t[:eq]), Text: v, From: from + strings.Index(t, v), To: s[1]})
			continue
		}
		out = append(out, arg{Text: t, From: from, To: from + len(t)})
	}
	return end, out
}

func kwargIndex(p string) int {
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '=' {
			if i+1 < len(p) && p[i+1] == '=' {
				return -1
			}
			name := strings.TrimSpace(p[:i])
			if name == "" || !identRe.MatchString(name) {
				return -1
			}
			return i
		}
		if !isIdentByte(c) && c != ' ' {
			return -1
		}
	}
	return -1
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

var identRe = regexp.MustCompile(`^[A-Za-z_]\w*$`)
var identsRe = regexp.MustCompile(`[A-Za-z_]\w*`)

// --- string literals --------------------------------------------------------

// pairIndexAt counts the string literals ("" or ” in Code) that start
// before pos.
func pairIndexAt(code string, pos int) int {
	k := 0
	for i := 0; i < len(code) && i < pos; i++ {
		if code[i] == '"' || code[i] == '\'' {
			k++
			i++
		}
	}
	return k
}

// stringsIn returns the contents of the string literals within Code[from:to].
func stringsIn(l scan.PyLine, from, to int) []string {
	var out []string
	k := 0
	for i := 0; i < len(l.Code); i++ {
		if l.Code[i] == '"' || l.Code[i] == '\'' {
			if i >= from && i < to && k < len(l.Strings) {
				out = append(out, l.Strings[k])
			}
			k++
			i++
		}
	}
	return out
}

var strLitRe = regexp.MustCompile(`^[rRbBfFuU]{0,2}(""|'')$`)

func isStrLit(t string) bool { return strLitRe.MatchString(strings.TrimSpace(t)) }

// onlyStrings reports whether a comma list holds only string literals.
func onlyStrings(s string) bool {
	any := false
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !isStrLit(p) {
			return false
		}
		any = true
	}
	return any
}

// litValue returns the string of an argument that is a literal.
func litValue(l scan.PyLine, a arg) (string, bool) {
	if !isStrLit(a.Text) {
		return "", false
	}
	k := pairIndexAt(l.Code, a.From+len(a.Text)-2)
	if k < len(l.Strings) {
		return l.Strings[k], true
	}
	return "", false
}

// strValue resolves an argument to a string: a literal or a module constant.
func (f *pyFile) strValue(l scan.PyLine, a arg) (string, bool) {
	if v, ok := litValue(l, a); ok {
		return v, true
	}
	if v, ok := f.consts[strings.TrimSpace(a.Text)]; ok {
		return v, true
	}
	return "", false
}

// --- assignments ------------------------------------------------------------

// splitAssign splits `a, b = rhs` at the top-level '='. It returns the
// simple-name targets (none when the left side is a subscript or an
// attribute), the right side and its offset in code.
func splitAssign(code string) (lhs []string, rhs string, from int, ok bool) {
	depth := 0
	for i := 0; i < len(code); i++ {
		switch c := code[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '"', '\'':
			i++
		case '=':
			if depth != 0 {
				continue
			}
			if i+1 < len(code) && code[i+1] == '=' {
				return nil, "", 0, false
			}
			if i > 0 && strings.IndexByte("=!<>+-*/%&|^@:", code[i-1]) >= 0 {
				return nil, "", 0, false
			}
			left := strings.TrimSpace(code[:i])
			if j := strings.Index(left, ":"); j > 0 { // annotation
				left = strings.TrimSpace(left[:j])
			}
			for _, p := range strings.Split(strings.Trim(left, "()[] "), ",") {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				if !identRe.MatchString(p) {
					return nil, code[i+1:], i + 1, true
				}
				lhs = append(lhs, p)
			}
			return lhs, code[i+1:], i + 1, true
		}
	}
	return nil, "", 0, false
}

// lhsText returns the text left of the top-level '=' ("" when none).
func lhsText(code string) string {
	if _, _, from, ok := splitAssign(code); ok {
		return strings.TrimSpace(code[:from-1])
	}
	return ""
}

// --- names ------------------------------------------------------------------

var (
	camelRe    = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	nonAlphaRe = regexp.MustCompile(`[^A-Za-z]+`)
)

// segments splits a name into lower-case parts on _, digits and camelCase.
func segments(id string) []string {
	id = camelRe.ReplaceAllString(id, "${1}_${2}")
	var out []string
	for _, p := range nonAlphaRe.Split(strings.ToLower(id), -1) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hasSeg(id string, set ...string) bool {
	for _, s := range segments(id) {
		for _, w := range set {
			if s == w {
				return true
			}
		}
	}
	return false
}

// heldOut reports whether a name names a test, holdout or validation set.
func heldOut(id string) bool {
	for _, s := range segments(id) {
		if heldOutSeg(s) {
			return true
		}
		if len(s) > 1 && (s[0] == 'x' || s[0] == 'y') && heldOutSeg(s[1:]) {
			return true
		}
	}
	return false
}

func heldOutSeg(s string) bool {
	switch s {
	case "holdout", "val", "valid", "validation", "vali":
		return true
	}
	return strings.HasPrefix(s, "test")
}

// trainish reports whether an argument names training data.
func trainish(text string) bool {
	return strings.Contains(strings.ToLower(text), "train")
}

// idents returns the identifiers of an expression, without string contents.
func idents(text string) []string { return identsRe.FindAllString(text, -1) }

// imports reports whether f imports a module with one of the prefixes.
func (f *pyFile) imports(mods ...string) bool {
	for _, im := range f.Imports {
		for _, m := range mods {
			if im.Module == m || strings.HasPrefix(im.Module, m+".") {
				return true
			}
		}
	}
	return false
}

// --- file selection ---------------------------------------------------------

// pyLike reports whether a path is Python source or a notebook by name.
func pyLike(p string) bool {
	l := strings.ToLower(p)
	return strings.HasSuffix(l, ".py") || strings.HasSuffix(l, ".ipynb")
}

func lintMatchAny(globs []string, p string) bool {
	for _, g := range globs {
		if packs.MatchGlob(g, p) {
			return true
		}
	}
	return false
}

// mlPaths are the configured path globs.
type mlPaths struct{ train, features, eval []string }

func pathsOf(in *packs.Input) mlPaths {
	s := in.Settings()
	return mlPaths{s.Strings("paths.train"), s.Strings("paths.features"), s.Strings("paths.eval")}
}

func (p mlPaths) any() []string {
	return append(append(append([]string{}, p.train...), p.features...), p.eval...)
}

// candidate reports whether a changed path can hold Python for the lints:
// a .py or .ipynb, or a Synapse JSON notebook under a configured path.
func (p mlPaths) candidate(path string) bool {
	if pyLike(path) {
		return true
	}
	return strings.HasSuffix(strings.ToLower(path), ".json") && lintMatchAny(p.any(), path)
}

// trainingCall reports whether a call trains a model (§4.1): .fit(,
// optimizer.step( or trainer.train(.
func trainingCall(c pcall) bool {
	r := strings.ToLower(c.Receiver)
	switch c.Name {
	case "fit":
		return true
	case "step":
		return strings.Contains(r, "optim")
	case "train":
		return strings.Contains(r, "trainer")
	}
	return false
}

func (f *pyFile) trains() bool {
	for _, cs := range f.Calls {
		for _, c := range cs {
			if trainingCall(c) {
				return true
			}
		}
	}
	return false
}

// selected is a scanned file and its role.
type selected struct {
	f        *pyFile
	training bool
	feature  bool
}

// selectFiles loads the changed candidate files. A training file matches
// paths.train, or, when paths.train is empty, calls a training API. With
// scope "lint" the paths.features and paths.eval files are kept too. Read
// and parse errors are returned as warn findings.
func selectFiles(ctx context.Context, in *packs.Input, lint bool) ([]selected, []packs.Finding) {
	ps := pathsOf(in)
	var out []selected
	var errs []packs.Finding
	n := 0
	var skip skipCounter
	for _, p := range in.ChangedPaths() {
		if !ps.candidate(p) {
			continue
		}
		inTrain := lintMatchAny(ps.train, p)
		inFeat := lintMatchAny(ps.features, p)
		inEval := lintMatchAny(ps.eval, p)
		if len(ps.train) > 0 && !inTrain && !(lint && (inFeat || inEval)) {
			continue
		}
		if skip.stop(ctx, n, p) {
			continue
		}
		n++
		f, err := loadPy(in, p)
		if err != nil {
			errs = append(errs, packs.Finding{File: p, Level: packs.LevelWarn, Rule: "unreadable", Message: "cannot scan this file: " + shortErr(err)})
			continue
		}
		training := inTrain || (len(ps.train) == 0 && f.trains())
		if !training && !(lint && (inFeat || inEval)) {
			continue
		}
		out = append(out, selected{f: f, training: training, feature: inFeat || len(ps.features) == 0})
	}
	return out, append(errs, skip.finding(n, "candidate files")...)
}

const ruleScanLimit = "scan-limit"

// skipCounter counts the candidate files a check did not scan because it hit
// the file cap or its deadline, so the result says so instead of passing on
// a partial scan.
type skipCounter struct {
	n     int
	first string
	late  bool // the deadline, not the cap
}

// stop reports whether the check must skip path p, and counts it.
func (s *skipCounter) stop(ctx context.Context, scanned int, p string) bool {
	if ctx.Err() == nil && scanned < maxFiles {
		return false
	}
	if s.n == 0 {
		s.first = p
	}
	if ctx.Err() != nil {
		s.late = true
	}
	s.n++
	return true
}

// finding returns the warn finding of the skipped files, if any.
func (s *skipCounter) finding(scanned int, what string) []packs.Finding {
	if s.n == 0 {
		return nil
	}
	why := fmt.Sprintf("the %d-file cap", maxFiles)
	if s.late {
		why = "the check's time limit"
	}
	return []packs.Finding{{File: s.first, Level: packs.LevelWarn, Rule: ruleScanLimit,
		Message: fmt.Sprintf("scanned %d of %d %s; %d skipped from here on (%s); run the check on a smaller change set", scanned, scanned+s.n, what, s.n, why)}}
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// exists reports whether a repo path exists on the new side.
func existsNew(in *packs.Input, p string) bool {
	if in.Staged && in.Git != nil {
		_, err := in.Git.Show(":", p)
		return err == nil
	}
	_, err := os.Stat(filepath.Join(in.Root, filepath.FromSlash(p)))
	return err == nil
}

// changedSet is the set of changed (not deleted) paths.
func changedSet(in *packs.Input) map[string]bool {
	m := map[string]bool{}
	for _, p := range in.ChangedPaths() {
		m[p] = true
	}
	return m
}

func joinDir(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return path.Join(dir, name)
}

func lintPlural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// isTestPath reports whether a path is test code.
func lintTestPath(p string) bool {
	b := path.Base(p)
	return strings.HasPrefix(p, "tests/") || strings.HasPrefix(p, "test/") || strings.Contains(p, "/tests/") || strings.Contains(p, "/test/") ||
		strings.HasPrefix(b, "test_") || strings.HasSuffix(b, "_test.py") || b == "conftest.py"
}
