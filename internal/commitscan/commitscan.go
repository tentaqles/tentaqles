// Package commitscan looks for secrets in what a `git commit` command is about
// to record, before the command runs. It closes the gap a plain pre-commit
// view leaves in a chained command such as `git add -A && git commit -m x`:
// at PreToolUse time nothing is staged yet, so the scan also covers what the
// chained `git add` / `commit -a` / `commit <paths>` will pick up.
//
// Every scan runs in the directory the command acts on (a leading `cd`, then
// `git -C`; see guard.GitInvocation.Dir), and fails toward asking: a file too
// large to read, a directory tq cannot resolve or an unbounded pathspec is an
// AskError, never a silent skip.
package commitscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/secrets"
)

// Runner runs git in dir and returns its stdout.
type Runner func(dir string, args ...string) (string, error)

// Hit is one finding, safe to print: it never carries the secret.
type Hit struct {
	File    string
	Line    int // 0 when the whole file is the problem (e.g. a .env file)
	Pattern string
}

func (h Hit) String() string {
	if h.Line == 0 {
		return fmt.Sprintf("%s (%s)", h.File, h.Pattern)
	}
	return fmt.Sprintf("%s:%d (%s)", h.File, h.Line, h.Pattern)
}

// AskError means the commit cannot be scanned safely; callers ask.
type AskError struct{ Reason string }

func (e *AskError) Error() string { return e.Reason }

// IsAsk reports whether err is an AskError.
func IsAsk(err error) bool {
	var a *AskError
	return errors.As(err, &a)
}

// envFileRe names files that must never be committed.
var envFileRe = regexp.MustCompile(`(?i)(^|/)\.env(\.[a-z0-9_-]+)?$|(^|/)id_(rsa|ed25519|ecdsa|dsa)$|\.(pem|p12|pfx|key)$|(^|/)credentials\.json$`)
var envTemplateRe = regexp.MustCompile(`(?i)\.env\.(example|sample|template|dist|defaults|schema)$`)

const askPathspecFile = "the commit reads its paths from --pathspec-from-file, which tq cannot bound; name the paths or stage them first"

// Plan says what a command will commit.
type Plan struct {
	Commits    bool // the command runs git commit
	WorkingDir bool // tracked, unstaged changes get committed too (commit -a, commit <paths>, or a chained git add)
	CommitAll  bool // commit -a/--all: every tracked change, regardless of AddPaths
	Untracked  bool // new files get committed too (a chained git add)
	// AddPaths limits the working-tree/untracked scan to what a chained
	// `git add <paths>` stages. Empty with Untracked set means everything
	// (git add -A / --all / . / :/).
	AddPaths []string
	// CommitPaths are the pathspecs of `git commit [-i|-o] [--] <paths>`:
	// their working-tree content is committed.
	CommitPaths []string
	// Ask is set when the commit cannot be bounded.
	Ask string
}

// PlanFor inspects command. A command without git commit yields Commits=false.
// Heredoc bodies are data, not commands, so they are ignored. It merges every
// git call regardless of directory; Plans splits them per directory.
func PlanFor(command string) Plan {
	invs := guard.GitInvocations(guard.StripHeredocs(command))
	items := make([]planItem, len(invs))
	for i, inv := range invs {
		items[i] = planItem{inv: inv}
	}
	return planOf(items)
}

// DirPlan is the plan for the git calls acting on one repository. Dir is the
// repository's top level, and every path in the plan is relative to it.
type DirPlan struct {
	Dir string
	Plan
	// Adds are the plan's git add calls, each with the directory it runs in
	// and its pathspecs as written, so git itself resolves globs and
	// relative paths when the scan lists what they stage.
	Adds []AddCall
}

// AddCall is one `git add` as the scan replays it.
type AddCall struct {
	Dir    string   // the directory the call runs in
	Specs  []string // pathspecs as written ("" none)
	Update bool     // -u/--update: tracked files only, never untracked ones
	Whole  bool     // no pathspec (or one tq cannot evaluate): the whole repository
}

// parseAdd reads one git add call's arguments.
func parseAdd(dir string, args []string) (AddCall, string) {
	a := AddCall{Dir: dir}
	ask := ""
	for i := 0; i < len(args); i++ {
		w := args[i]
		switch {
		case w == "--":
			a.Specs = append(a.Specs, args[i+1:]...)
			i = len(args)
		case w == "--update":
			a.Update = true
		case w == "--pathspec-from-file" || strings.HasPrefix(w, "--pathspec-from-file="):
			ask = askPathspecFile
		case strings.HasPrefix(w, "--"):
		case strings.HasPrefix(w, "-") && len(w) > 1:
			if strings.Contains(w[1:], "u") {
				a.Update = true
			}
		default:
			a.Specs = append(a.Specs, w)
		}
	}
	for _, sp := range a.Specs {
		if strings.ContainsAny(sp, "$`") {
			a.Whole = true // a substitution: cannot be evaluated here
		}
	}
	if len(a.Specs) == 0 {
		a.Whole = true // git add -A / --all / -u with no pathspec: the whole tree
	}
	return a, ask
}

// planItem is one git call and the directory it runs in, relative to its
// repository's top level ("" at the top).
type planItem struct {
	inv guard.GitInvocation
	rel string
	dir string
}

// Plans groups the command's add and commit calls by the repository they act
// on (the top level of the directory each one runs in, resolved against cwd)
// and returns a plan for every repository that commits. `git add -A` in a
// subdirectory stages the whole repository, and `git -C sub commit` records
// what a root-level add staged, so grouping by directory would miss both. A
// call whose directory cannot be resolved is an AskError.
func Plans(cwd, command string, run Runner) ([]DirPlan, error) {
	invs := guard.GitInvocations(guard.StripHeredocs(command))
	if !PlanFor(command).Commits {
		return nil, nil
	}
	type repo struct {
		top   string
		items []planItem
	}
	groups := map[string]*repo{}
	tops := map[string][2]string{} // dir -> {top, prefix}
	var order []string
	for _, inv := range invs {
		switch inv.Sub {
		case "add", "stage", "commit":
		default:
			continue
		}
		d, err := inv.Dir(cwd)
		if err != nil {
			return nil, &AskError{Reason: "could not tell which repository this commit records in: " + err.Error()}
		}
		tp, ok := tops[d]
		if !ok {
			tp = [2]string{d, ""}
			if out, err := run(d, "rev-parse", "--show-toplevel", "--show-prefix"); err == nil {
				ls := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
				if t := strings.TrimSpace(ls[0]); t != "" {
					tp[0] = filepath.FromSlash(t)
					if len(ls) > 1 {
						tp[1] = strings.TrimSuffix(strings.TrimSpace(ls[1]), "/")
					}
				}
			}
			tops[d] = tp
		}
		key := strings.ToLower(filepath.Clean(tp[0]))
		g, ok := groups[key]
		if !ok {
			g = &repo{top: tp[0]}
			groups[key] = g
			order = append(order, key)
		}
		g.items = append(g.items, planItem{inv: inv, rel: tp[1], dir: d})
	}
	var out []DirPlan
	for _, k := range order {
		p := planOf(groups[k].items)
		if !p.Commits {
			continue
		}
		dp := DirPlan{Dir: groups[k].top, Plan: p}
		for _, it := range groups[k].items {
			if it.inv.Sub == "add" || it.inv.Sub == "stage" {
				a, ask := parseAdd(it.dir, it.inv.Args)
				if ask != "" {
					dp.Ask = ask
				}
				dp.Adds = append(dp.Adds, a)
			}
		}
		out = append(out, dp)
	}
	return out, nil
}

// topPath turns a pathspec given in a directory rel (relative to the top
// level) into a top-level-relative path. all reports a pathspec that names
// the whole repository or cannot be bounded here (pathspec magic such as
// `:/`, a path outside the repository, an absolute path, a substitution).
func topPath(rel, a string) (p string, all bool) {
	a = strings.Trim(a, `"'`)
	if strings.HasPrefix(a, ":") || strings.ContainsAny(a, "$`") {
		return "", true
	}
	a = filepath.ToSlash(a)
	if strings.HasPrefix(a, "/") || filepath.VolumeName(a) != "" {
		return "", true
	}
	j := path.Clean(path.Join(rel, a))
	if j == "." || j == ".." || strings.HasPrefix(j, "../") {
		return "", true
	}
	return j, false
}

// shortValueOpts are git commit's short options that take a value (attached
// or as the next word); longValueOpts the long ones that take the next word
// when given without "=".
var (
	shortValueOpts = "mFCct"
	longValueOpts  = map[string]bool{"--message": true, "--file": true, "--reuse-message": true, "--reedit-message": true,
		"--fixup": true, "--squash": true, "--author": true, "--date": true, "--template": true, "--cleanup": true, "--trailer": true}
)

// commitArgs reads the arguments of one git commit.
func commitArgs(args []string) (all bool, paths []string, ask string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			paths = append(paths, args[i+1:]...)
			return all, paths, ask
		case a == "--all":
			all = true
		case a == "--pathspec-from-file" || strings.HasPrefix(a, "--pathspec-from-file="):
			ask = askPathspecFile
		case longValueOpts[a]:
			i++
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
		cluster:
			for k := 1; k < len(a); k++ {
				c := a[k]
				switch {
				case c == 'a':
					all = true
				case strings.IndexByte(shortValueOpts, c) >= 0:
					if k == len(a)-1 {
						i++ // the value is the next word
					}
					break cluster
				case c == 'u' || c == 'S':
					break cluster // optional value, attached only
				}
			}
		default:
			paths = append(paths, a)
		}
	}
	return all, paths, ask
}

func planOf(items []planItem) Plan {
	var p Plan
	sawAdd, addAll := false, false
	var paths []string
	for _, it := range items {
		inv := it.inv
		switch inv.Sub {
		case "add", "stage":
			sawAdd = true
			for _, a := range inv.Args {
				switch {
				case a == "-A" || a == "--all" || a == ":/" || a == "*" || a == "--no-ignore-removal":
					addAll = true // the whole repository, wherever git runs
				case strings.HasPrefix(a, "-"):
				case strings.ContainsAny(a, "*?[$`:"):
					// globs, magic pathspecs and substitutions: cannot be
					// resolved here, so scan everything
					addAll = true
				default:
					tp, all := topPath(it.rel, a)
					if all {
						addAll = true
					} else {
						paths = append(paths, tp)
					}
				}
			}
			if len(inv.Args) == 0 {
				addAll = true
			}
		case "commit":
			p.Commits = true
			all, cps, ask := commitArgs(inv.Args)
			if all {
				p.WorkingDir = true
				p.CommitAll = true
			}
			for _, c := range cps {
				p.WorkingDir = true
				if strings.ContainsAny(c, "*?[") && !strings.HasPrefix(c, ":") {
					// a glob: keep it, anchored at the call's directory
					p.CommitPaths = append(p.CommitPaths, path.Join(it.rel, filepath.ToSlash(c)))
					continue
				}
				if tp, whole := topPath(it.rel, c); whole {
					p.CommitAll = true
				} else {
					p.CommitPaths = append(p.CommitPaths, tp)
				}
			}
			if ask != "" {
				p.Ask = ask
			}
		}
	}
	if p.Commits && sawAdd {
		p.WorkingDir = true
		p.Untracked = true
		if !addAll {
			p.AddPaths = paths
		}
	}
	return p
}

const (
	maxFileBytes  = 1 << 20 // per file read from disk or the index
	maxTotalBytes = 8 << 20 // everything one commit decision scans
	sniffBytes    = 8000
)

// MaxTotalBytes is the most one commit decision scans; the hook bounds git
// output with it too.
const MaxTotalBytes = maxTotalBytes

// Scan returns every secret the command would commit (see ScanCtx). Errors
// are dropped; hook callers use ScanCtx.
func Scan(dir, command string, run Runner) []Hit {
	h, _ := ScanCtx(context.Background(), dir, command, run)
	return h
}

// ScanCtx returns every secret the command would commit, scanning git output
// in the directory each commit acts on. A plain git error (no HEAD yet)
// narrows the scan, because the commit itself will fail or record nothing;
// an AskError, an oversized git output or the end of ctx is returned.
func ScanCtx(ctx context.Context, cwd, command string, run Runner) ([]Hit, error) {
	plans, err := Plans(cwd, command, run)
	if err != nil {
		return nil, err
	}
	var hits []Hit
	seen := map[string]bool{}
	add := func(h Hit) {
		if k := h.String(); !seen[k] {
			seen[k] = true
			hits = append(hits, h)
		}
	}
	for _, dp := range plans {
		c := &collector{ctx: ctx, dir: dp.Dir, plan: dp.Plan, adds: dp.Adds, run: run, lenient: true}
		lines, names, err := c.collect()
		if err != nil {
			return hits, err
		}
		for _, f := range names {
			if bannedFile(f) {
				add(Hit{File: f, Pattern: "secret_file"})
			}
		}
		for i, l := range lines {
			if i%512 == 0 && ctx.Err() != nil {
				return hits, ctx.Err()
			}
			if name := secrets.ScanLine(l.Text); name != "" {
				add(Hit{File: l.File, Line: l.Line, Pattern: name})
			}
		}
	}
	return hits, nil
}

func bannedFile(f string) bool {
	f = filepath.ToSlash(f)
	return envFileRe.MatchString(f) && !envTemplateRe.MatchString(f)
}

// AddedLine is one line a commit would add.
type AddedLine struct {
	File string
	Line int
	Text string
}

// scanDiff scans only added lines, mapping them back to their line number in
// the new file.
func scanDiff(diff string) []Hit {
	var hits []Hit
	for _, a := range ParseDiffAdded(diff) {
		if name := secrets.ScanLine(a.Text); name != "" {
			hits = append(hits, Hit{File: a.File, Line: a.Line, Pattern: name})
		}
	}
	return hits
}

// Added returns every added line the command would commit (see AddedCtx).
func Added(dir, command string, run Runner) ([]AddedLine, error) {
	return AddedCtx(context.Background(), dir, command, run)
}

// AddedCtx returns every added line the command would commit, in the same
// scope ScanCtx uses (staged diff; plus the working tree and untracked files
// when the command also runs git add, commit -a or commit <paths>). Binary
// files are skipped; UTF-16 files with a BOM are decoded. Any git error is
// returned.
func AddedCtx(ctx context.Context, cwd, command string, run Runner) ([]AddedLine, error) {
	plans, err := Plans(cwd, command, run)
	if err != nil {
		return nil, err
	}
	var out []AddedLine
	for _, dp := range plans {
		c := &collector{ctx: ctx, dir: dp.Dir, plan: dp.Plan, adds: dp.Adds, run: run}
		lines, _, err := c.collect()
		if err != nil {
			return nil, err
		}
		out = append(out, lines...)
	}
	return out, nil
}

// collector gathers what one directory's commit records.
type collector struct {
	ctx     context.Context
	dir     string
	plan    Plan
	adds    []AddCall
	run     Runner
	lenient bool // skip plain git errors (secret scan) instead of returning them
	total   int
	top     string
}

// ErrTooLarge is what a Runner returns when git's output passed its cap.
var ErrTooLarge = errors.New("git output too large to scan")

func (c *collector) git(args ...string) (string, bool, error) { return c.gitIn(c.dir, args...) }

func (c *collector) gitIn(dir string, args ...string) (string, bool, error) {
	if err := c.ctx.Err(); err != nil {
		return "", false, err
	}
	out, err := c.run(dir, args...)
	if err != nil {
		if c.ctx.Err() != nil {
			return "", false, c.ctx.Err()
		}
		if errors.Is(err, ErrTooLarge) {
			return "", false, &AskError{Reason: fmt.Sprintf("what this commit records is larger than the %d MiB scan limit; commit fewer files", maxTotalBytes>>20)}
		}
		if c.lenient && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, true, nil
}

func (c *collector) charge(n int) error {
	c.total += n
	if c.total > maxTotalBytes {
		return &AskError{Reason: fmt.Sprintf("what this commit records is larger than the %d MiB scan limit; commit fewer files", maxTotalBytes>>20)}
	}
	return nil
}

func (c *collector) collect() (lines []AddedLine, names []string, err error) {
	if c.plan.Ask != "" {
		return nil, nil, &AskError{Reason: c.plan.Ask}
	}
	type diffSpec struct {
		dir    string
		args   []string
		staged bool
	}
	diffArgs := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "-U0"}
	specs := []diffSpec{{c.dir, append([]string{"diff", "--cached"}, diffArgs[1:]...), true}}
	type nameSpec struct {
		dir  string
		args []string
	}
	nameSpecs := []nameSpec{{c.dir, []string{"diff", "--cached", "--name-only"}}}
	if c.plan.WorkingDir {
		// the working-tree scope: the whole tree, or the union of what the
		// commit names and what each add stages (pathspecs resolved by git
		// in the directory each call runs in)
		whole := c.plan.CommitAll
		type scope struct {
			dir   string
			specs []string
		}
		var scopes []scope
		if len(c.plan.CommitPaths) > 0 {
			scopes = append(scopes, scope{c.dir, c.plan.CommitPaths})
		}
		for _, a := range c.adds {
			if a.Whole {
				whole = true
			} else {
				scopes = append(scopes, scope{a.Dir, a.Specs})
			}
		}
		if len(c.adds) == 0 && len(c.plan.CommitPaths) == 0 {
			whole = true
		}
		if whole {
			scopes = []scope{{c.dir, nil}}
		}
		for _, sc := range scopes {
			wd := append([]string{}, diffArgs...)
			wn := []string{"diff", "--name-only"}
			if len(sc.specs) > 0 {
				wd = append(append(wd, "--"), sc.specs...)
				wn = append(append(wn, "--"), sc.specs...)
			}
			specs = append(specs, diffSpec{sc.dir, wd, false})
			nameSpecs = append(nameSpecs, nameSpec{sc.dir, wn})
		}
	}
	for _, ns := range nameSpecs {
		out, ok, err := c.gitIn(ns.dir, ns.args...)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			names = append(names, splitLines(out)...)
		}
	}
	for _, s := range specs {
		out, ok, err := c.gitIn(s.dir, s.args...)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}
		if err := c.charge(len(out)); err != nil {
			return nil, nil, err
		}
		added, binaries := ParseDiff(out)
		lines = append(lines, added...)
		for _, b := range binaries {
			got, err := c.binary(b, s.staged)
			if err != nil {
				return nil, nil, err
			}
			lines = append(lines, got...)
		}
	}
	if c.plan.Untracked {
		// the untracked files the add calls stage, listed by git itself
		// (top-level paths): -u adds none; no pathspec means the whole
		// repository; otherwise git resolves globs and relative paths
		var listed []string
		seen := map[string]bool{}
		ok := true
		for _, a := range c.adds {
			if a.Update {
				continue
			}
			dir, args := a.Dir, []string{"ls-files", "--others", "--exclude-standard", "--full-name"}
			if a.Whole {
				dir = c.dir
			} else {
				args = append(append(args, "--"), a.Specs...)
			}
			out, got, err := c.gitIn(dir, args...)
			if err != nil {
				return nil, nil, err
			}
			ok = ok && got
			for _, f := range splitLines(out) {
				if !seen[f] {
					seen[f] = true
					listed = append(listed, f)
				}
			}
		}
		if ok {
			for _, f := range listed {
				names = append(names, f)
				if bannedFile(f) {
					continue
				}
				if err := c.ctx.Err(); err != nil {
					return nil, nil, err
				}
				text, ok, err := readBounded(filepath.Join(c.dir, filepath.FromSlash(f)), f)
				if err != nil {
					return nil, nil, err
				}
				if !ok {
					continue
				}
				if err := c.charge(len(text)); err != nil {
					return nil, nil, err
				}
				lines = append(lines, fileLines(f, text)...)
			}
		}
	}
	return lines, names, nil
}

// binary reads a file the diff reported as binary: the staged blob, or the
// working-tree file. A UTF-16 file is decoded and returned whole; a real
// binary yields nothing.
func (c *collector) binary(name string, staged bool) ([]AddedLine, error) {
	var text string
	var ok bool
	var err error
	if staged {
		raw, got, gerr := c.git("cat-file", "-p", ":"+name)
		if gerr != nil {
			if IsAsk(gerr) {
				return nil, &AskError{Reason: fmt.Sprintf("%s is larger than the %d MiB scan limit and was not scanned; commit it separately and confirm", name, maxTotalBytes>>20)}
			}
			return nil, gerr
		}
		if !got {
			return nil, nil
		}
		text, ok, err = decodeLimited([]byte(raw), name)
	} else {
		if c.top == "" {
			out, got, gerr := c.git("rev-parse", "--show-toplevel")
			if gerr != nil {
				return nil, gerr
			}
			if !got {
				return nil, nil
			}
			c.top = strings.TrimSpace(out)
		}
		text, ok, err = readBounded(filepath.Join(c.top, filepath.FromSlash(name)), name)
	}
	if err != nil || !ok {
		return nil, err
	}
	if err := c.charge(len(text)); err != nil {
		return nil, err
	}
	return fileLines(name, text), nil
}

func fileLines(f, text string) []AddedLine {
	var out []AddedLine
	for i, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if l == "" && i > 0 {
			continue
		}
		out = append(out, AddedLine{File: f, Line: i + 1, Text: l})
	}
	return out
}

func tooLarge(name string) error {
	return &AskError{Reason: fmt.Sprintf("%s is larger than the %d MiB scan limit and was not scanned; commit it separately and confirm", name, maxFileBytes>>20)}
}

// readBounded reads a file from disk for scanning. ok is false for anything
// that is not a regular file (a symlink, a FIFO, a device, a directory: git
// records a link's text, never its target) and for binary content. A text
// file over the per-file cap is an AskError naming the file.
func readBounded(p, name string) (text string, ok bool, err error) {
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false, nil
	}
	f, err := openFile(p)
	if err != nil {
		return "", false, &AskError{Reason: name + " could not be opened to scan it; check the file and confirm"}
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return "", false, &AskError{Reason: name + " could not be read to scan it; check the file and confirm"}
	}
	return decodeLimited(raw, name)
}

// openFile opens a file for scanning; tests replace it.
var openFile = os.Open

// decodeLimited turns raw file bytes into text: UTF-16 LE/BE with a BOM is
// decoded, a UTF-8 BOM dropped, other NUL-carrying content is binary (ok
// false). Text over the per-file cap is an AskError.
func decodeLimited(raw []byte, name string) (string, bool, error) {
	text, isText := decode(raw)
	if !isText {
		return "", false, nil
	}
	if len(raw) > maxFileBytes {
		return "", false, tooLarge(name)
	}
	return text, true, nil
}

// DecodeLimited is decodeLimited for the push rule: UTF-16 with a BOM is
// decoded, binary is ok=false, text over the per-file cap is an AskError.
func DecodeLimited(raw []byte, name string) (string, bool, error) { return decodeLimited(raw, name) }

// FileLines splits decoded file text into numbered lines.
func FileLines(name, text string) []AddedLine { return fileLines(name, text) }

// MaxFileBytes is the per-file cap.
const MaxFileBytes = maxFileBytes

func decode(raw []byte) (string, bool) {
	switch {
	case len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE:
		return decodeUTF16(raw[2:], false), true
	case len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF:
		return decodeUTF16(raw[2:], true), true
	case len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF:
		raw = raw[3:]
	}
	if isBinary(raw) {
		return "", false
	}
	return string(raw), true
}

func decodeUTF16(b []byte, bigEndian bool) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return string(utf16.Decode(u))
}

func isBinary(b []byte) bool {
	n := len(b)
	if n > sniffBytes {
		n = sniffBytes
	}
	for _, c := range b[:n] {
		if c == 0 {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		l = strings.Trim(strings.TrimSpace(l), `"`)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
