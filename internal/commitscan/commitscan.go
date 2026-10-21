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
	return planOf(guard.GitInvocations(guard.StripHeredocs(command)))
}

// DirPlan is the plan for the git calls acting on one directory.
type DirPlan struct {
	Dir string
	Plan
}

// Plans groups the command's add and commit calls by the directory they act
// on (resolved against cwd) and returns a plan for every directory that
// commits. A call whose directory cannot be resolved is an AskError.
func Plans(cwd, command string) ([]DirPlan, error) {
	invs := guard.GitInvocations(guard.StripHeredocs(command))
	if !planOf(invs).Commits {
		return nil, nil
	}
	groups := map[string][]guard.GitInvocation{}
	dirs := map[string]string{}
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
		key := strings.ToLower(filepath.Clean(d))
		if _, ok := groups[key]; !ok {
			order = append(order, key)
			dirs[key] = d
		}
		groups[key] = append(groups[key], inv)
	}
	var out []DirPlan
	for _, k := range order {
		if p := planOf(groups[k]); p.Commits {
			out = append(out, DirPlan{Dir: dirs[k], Plan: p})
		}
	}
	return out, nil
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

func planOf(invs []guard.GitInvocation) Plan {
	var p Plan
	sawAdd, addAll := false, false
	var paths []string
	for _, inv := range invs {
		switch inv.Sub {
		case "add", "stage":
			sawAdd = true
			for _, a := range inv.Args {
				switch {
				case a == "-A" || a == "--all" || a == "." || a == ":/" || a == "*" || a == "--no-ignore-removal":
					addAll = true
				case strings.HasPrefix(a, "-"):
				case strings.ContainsAny(a, "*?[$`:"):
					// globs, magic pathspecs and substitutions: cannot be
					// resolved here, so scan everything
					addAll = true
				default:
					paths = append(paths, strings.Trim(a, `"'`))
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
			if len(cps) > 0 {
				p.WorkingDir = true
				p.CommitPaths = append(p.CommitPaths, cps...)
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

// inScope reports whether a repo-relative file is covered by AddPaths.
func (p Plan) inScope(f string) bool {
	if len(p.AddPaths) == 0 {
		return true
	}
	f = filepath.ToSlash(f)
	for _, a := range p.AddPaths {
		a = strings.TrimSuffix(filepath.ToSlash(a), "/")
		if a == "" || f == a || strings.HasPrefix(f, a+"/") {
			return true
		}
	}
	return false
}

// wdPathspec is the pathspec limiting the working-tree diff, or nil for the
// whole tree.
func (p Plan) wdPathspec() []string {
	if p.CommitAll || (p.Untracked && len(p.AddPaths) == 0) {
		return nil
	}
	return append(append([]string{}, p.AddPaths...), p.CommitPaths...)
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
	plans, err := Plans(cwd, command)
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
		c := &collector{ctx: ctx, dir: dp.Dir, plan: dp.Plan, run: run, lenient: true}
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
	plans, err := Plans(cwd, command)
	if err != nil {
		return nil, err
	}
	var out []AddedLine
	for _, dp := range plans {
		c := &collector{ctx: ctx, dir: dp.Dir, plan: dp.Plan, run: run}
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
	run     Runner
	lenient bool // skip plain git errors (secret scan) instead of returning them
	total   int
	top     string
}

// ErrTooLarge is what a Runner returns when git's output passed its cap.
var ErrTooLarge = errors.New("git output too large to scan")

func (c *collector) git(args ...string) (string, bool, error) {
	if err := c.ctx.Err(); err != nil {
		return "", false, err
	}
	out, err := c.run(c.dir, args...)
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
		args   []string
		staged bool
	}
	specs := []diffSpec{{[]string{"diff", "--cached", "--no-color", "--no-ext-diff", "--no-textconv", "-U0"}, true}}
	nameSpecs := [][]string{{"diff", "--cached", "--name-only"}}
	if c.plan.WorkingDir {
		wd := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "-U0"}
		wn := []string{"diff", "--name-only"}
		if ps := c.plan.wdPathspec(); len(ps) > 0 {
			wd = append(append(wd, "--"), ps...)
			wn = append(append(wn, "--"), ps...)
		}
		specs = append(specs, diffSpec{wd, false})
		nameSpecs = append(nameSpecs, wn)
	}
	for _, args := range nameSpecs {
		out, ok, err := c.git(args...)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			names = append(names, splitLines(out)...)
		}
	}
	for _, s := range specs {
		out, ok, err := c.git(s.args...)
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
		out, ok, err := c.git("ls-files", "--others", "--exclude-standard")
		if err != nil {
			return nil, nil, err
		}
		if ok {
			for _, f := range splitLines(out) {
				if !c.plan.inScope(f) {
					continue
				}
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
func readBounded(path, name string) (text string, ok bool, err error) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false, nil
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return "", false, nil
	}
	return decodeLimited(raw, name)
}

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
