package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/ignore"
	"github.com/tentaqles/tentaqles/internal/secrets"
)

// Safety model for what explore reads and may send to Jev. These are the
// security controls, and each holds on its own:
//
//  1. allowlist: only source and docs extensions/names (allowedFile);
//     config formats only with --include-config;
//  2. deny-list: key material, credential files, dumps, .env*, credential
//     directories, whatever any allowlist or ignore file says
//     (skipExplorePath);
//  3. redaction: every file is redacted line by line (internal/secrets,
//     plus whole PEM private-key blocks) before it is matched or turned
//     into a span, and tq redacts the request again before sending;
//  4. containment: no symlinks or junctions, every path component a plain
//     directory, the resolved file inside the resolved root, no nested
//     repositories, a size cap, no binaries;
//  5. no git process: a cloned repo's .git/config can run programs.
//
// Ignore files (.gitignore and friends, internal/ignore) are NOT a security
// control. They only cut noise: build output, vendored code, generated
// files. A source that cannot be read or parsed is skipped and counted in
// the footer; nothing above depends on it.

// exploreOpts are the walk's switches.
type exploreOpts struct {
	// IncludeConfig admits config formats (allowConfigExts). They are still
	// deny-listed and redacted.
	IncludeConfig bool
}

func newDecideExploreCmd() *cobra.Command {
	var dir string
	var top, candidates int
	var asJSON, noJev bool
	var opts exploreOpts
	c := &cobra.Command{
		Use:   `explore "<question>"`,
		Short: "Find the few file:line ranges that answer a question about the code",
		Long: `Two stages, so an agent reads 5 ranges instead of 50 files:

  1. keyword pre-filter: a walk (no git process) over source and docs files
     for terms taken from the question; hits become ~40-line spans, grouped
     per file, best --candidates kept.
  2. one batched Jev call asks, per span, "is this relevant to answering the
     question?" and re-ranks them (split only to stay under the size cap).

Only source and docs files are read (config formats such as JSON/YAML only
with --include-config). Key material, credential files, dumps and .env
files are never read; every span is redacted; symlinks and nested repos are
never followed. .gitignore files only reduce noise.

Prints the top --top ranges with scores. When Jev is unavailable (backend
off, no key, not in a trusted workspace, an error) the keyword ranking is
printed instead and the output says so.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			question := strings.TrimSpace(args[0])
			if question == "" {
				return errors.New("question is empty")
			}
			root, err := exploreRoot(dir)
			if err != nil {
				return err
			}
			terms := decide.ExploreTerms(question, 8)
			if len(terms) == 0 {
				return errors.New("no searchable terms in the question: name an identifier, file or concept")
			}
			hits, stats, err := exploreHits(root, terms, opts)
			if err != nil {
				return err
			}
			files := &fileCache{root: root, opts: opts, lines: map[string][]string{}}
			spans := decide.BuildSpans(hits, terms, candidates, files.count)
			for i := range spans {
				spans[i].Text = files.text(spans[i].Path, spans[i].Start, spans[i].End)
			}

			var res decide.ExploreResult
			switch {
			case len(spans) == 0:
				res = decide.RankSpans(context.Background(), nil, question, terms, nil, top)
			case noJev:
				res = decide.RankSpans(context.Background(), nil, question, terms, spans, top)
				res.Fallback = "--no-jev"
			default:
				cl, _, _, cerr := decideClientAt(root, "explore", decide.BatchTimeout)
				res = decide.RankSpans(context.Background(), cl, question, terms, spans, top)
				if cerr != nil {
					res.Fallback = "jev unavailable: " + cerr.Error()
				}
			}
			res.IgnoreSourcesSkipped = stats.IgnoreSkipped
			res.UnreadableDirs = stats.UnreadableDirs
			res.NestedRepos = stats.NestedRepos
			return printExplore(c, res, asJSON)
		},
	}
	c.Flags().StringVar(&dir, "path", "", "directory to search (default: the current directory)")
	c.Flags().IntVar(&top, "top", 5, "ranges to print")
	c.Flags().IntVar(&candidates, "candidates", 30, "spans the keyword stage keeps for Jev")
	c.Flags().BoolVar(&asJSON, "json", false, "print the result as JSON")
	c.Flags().BoolVar(&noJev, "no-jev", false, "keyword ranking only (no network)")
	c.Flags().BoolVar(&opts.IncludeConfig, "include-config", false,
		"also read config formats (.json .yaml .yml .toml .ini .properties .xml .tfvars); still deny-listed and redacted")
	return c
}

func printExplore(c *cobra.Command, res decide.ExploreResult, asJSON bool) error {
	out := c.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	// The footer gives counts only: never the path of a file or directory
	// that was left out (its name may itself look like a secret).
	footer := func() {
		if res.NestedRepos > 0 {
			fmt.Fprintf(out, "note: skipped %d nested repo(s); run explore with --path inside one to search it\n", res.NestedRepos)
		}
		if res.UnreadableDirs > 0 {
			fmt.Fprintf(out, "note: %d director(ies) could not be read and were skipped\n", res.UnreadableDirs)
		}
		if res.IgnoreSourcesSkipped > 0 {
			fmt.Fprintf(out, "note: %d ignore source(s) could not be read or parsed and were not applied (ignore files only reduce noise)\n", res.IgnoreSourcesSkipped)
		}
	}
	if res.Candidates == 0 {
		fmt.Fprintf(out, "no keyword hits for: %s\nTry an identifier, file name or error text from the code.\n", strings.Join(res.Terms, ", "))
		footer()
		return nil
	}
	defer footer()
	if res.Mode == "jev" {
		fmt.Fprintf(out, "jev ranked %d candidate spans (%d request(s)); terms: %s\n", res.Candidates, res.Batches, strings.Join(res.Terms, ", "))
	} else {
		fmt.Fprintf(out, "keyword ranking of %d candidate spans (%s); terms: %s\n", res.Candidates, res.Fallback, strings.Join(res.Terms, ", "))
	}
	for _, s := range res.Results {
		fmt.Fprintf(out, "%.2f  %s:%d-%d  [%s]\n", s.Score, s.Path, s.Start, s.End, strings.Join(s.Terms, ", "))
	}
	return nil
}

// maxExploreHits bounds the pre-filter on a huge repo; the spans that
// matter most (many distinct terms) survive the cut because every term is
// searched in the same pass.
const maxExploreHits = 20_000

// maxExploreFile skips generated blobs and data dumps.
const maxExploreFile = 1 << 20

// exploreRoot resolves --path to an absolute, symlink-free directory: every
// file explore reads must resolve inside it.
func exploreRoot(dir string) (string, error) {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("--path %s: %w", dir, err)
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("--path %s: not a directory", dir)
	}
	return root, nil
}

// walkStats reports what the walk left out, as counts.
type walkStats struct {
	// IgnoreSkipped counts ignore sources (and git configs naming them)
	// that could not be read or parsed and so were not applied. Noise
	// filtering only: not a safety concern.
	IgnoreSkipped int
	// UnreadableDirs counts directories that could not be listed.
	UnreadableDirs int
	// NestedRepos counts repositories below the root (a .git dir or
	// gitdir: file) that were not entered: they are often other projects
	// or other clients' code.
	NestedRepos int
}

// exploreHits runs the keyword pre-filter. Paths come back slash-separated
// and relative to root; hit text is already redacted.
func exploreHits(root string, terms []string, o exploreOpts) ([]decide.Hit, walkStats, error) {
	var hits []decide.Hit
	stats, err := walkFiles(root, o, func(rel string, text string) bool {
		for i, line := range strings.Split(text, "\n") {
			low := strings.ToLower(line)
			for _, t := range terms {
				if strings.Contains(low, t) {
					hits = append(hits, decide.Hit{Path: rel, Line: i + 1, Text: clipLine(line)})
					break
				}
			}
		}
		return len(hits) < maxExploreHits
	})
	return hits, stats, err
}

// --- allowlist ------------------------------------------------------------

var allowCodeExts = setOf(".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".java", ".kt", ".cs",
	".rb", ".php", ".rs", ".swift", ".c", ".h", ".cpp", ".hpp", ".scala", ".sql", ".sh", ".ps1",
	".vue", ".svelte", ".astro")

var allowDocExts = setOf(".md", ".mdx", ".rst", ".txt")

var allowNames = setOf("makefile", "dockerfile")

// allowConfigExts are formats that commonly hold secrets: read only with
// --include-config. (.env* is on the deny-list and is never read.)
var allowConfigExts = setOf(".json", ".yaml", ".yml", ".toml", ".ini", ".properties", ".xml", ".tfvars")

func setOf(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// allowedFile reports whether a file's type may ever be read. It is checked
// in addition to, never instead of, the deny-list.
func allowedFile(rel string, o exploreOpts) bool {
	base := strings.ToLower(path.Base(rel))
	ext := path.Ext(base)
	switch {
	case allowNames[base], allowCodeExts[ext], allowDocExts[ext]:
		return true
	case o.IncludeConfig && allowConfigExts[ext]:
		return true
	}
	return false
}

// --- deny-list -------------------------------------------------------------

// exploreSkipDirs are never walked: VCS internals, dependency and build
// output, and credential stores.
var exploreSkipDirs = setOf(".git", "node_modules", "vendor", "dist", "build", ".venv",
	"venv", "__pycache__", ".next", ".cache", "target", ".idea",
	".aws", ".ssh", ".gnupg", ".azure", ".kube", ".docker")

// denyNames are credential files by exact (lowercase) name.
var denyNames = setOf(".npmrc", ".pypirc", ".netrc", "_netrc", ".git-credentials", ".pgpass", ".htpasswd",
	"package-lock.json", "go.sum", "pnpm-lock.yaml", "yarn.lock")

// denyExts are key material, credential stores and data dumps.
var denyExts = setOf(".pem", ".key", ".p12", ".pfx", ".jks", ".kdbx", ".keystore",
	".sqlite", ".sqlite3", ".db", ".dump", ".bak", ".lock", ".map")

// skipExplorePath is the always-deny list, applied whatever the allowlist,
// --include-config or ignore files say: secrets, key material, dumps (never
// read or sent), and lockfiles/minified bundles (noise).
func skipExplorePath(p string) bool {
	low := strings.ToLower(p)
	for _, seg := range strings.Split(low, "/") {
		if seg == ".aws" || seg == ".ssh" || seg == ".gnupg" || seg == ".git" {
			return true
		}
	}
	base := path.Base(low)
	switch {
	case denyNames[base], denyExts[path.Ext(base)]:
		return true
	case strings.HasPrefix(base, ".env"), strings.HasPrefix(base, "id_"):
		return true
	case strings.Contains(base, "credentials"), strings.Contains(base, "secret"):
		return true
	case strings.Contains(base, ".tfstate"), strings.HasSuffix(base, ".sql.gz"):
		return true
	case strings.HasSuffix(base, ".min.js"):
		return true
	}
	return false
}

// --- redaction -------------------------------------------------------------

// redactLines redacts text line by line, so line numbers never shift, and
// blanks every line of a PEM private-key block (secrets.Redact alone only
// replaces the BEGIN line, leaving the base64 body).
func redactLines(text string) string {
	lines := strings.Split(text, "\n")
	inKey := false
	for i, l := range lines {
		begin := strings.Contains(l, "-----BEGIN") && strings.Contains(l, "PRIVATE KEY")
		if begin || inKey {
			inKey = !(strings.Contains(l, "-----END") && strings.Contains(l, "PRIVATE KEY"))
			lines[i] = "[REDACTED:private_key]"
			continue
		}
		lines[i] = secrets.Redact(l)
	}
	return strings.Join(lines, "\n")
}

// --- containment ------------------------------------------------------------

// readExploreFile returns the redacted text of rel (slash-separated,
// relative to root) only when every hard rule holds: deny-list, allowlist,
// a regular file (not a symlink, device or pipe) under plain directories,
// resolving inside root, under the size cap, not binary. root must be
// symlink-free (exploreRoot). ok is false when the file must not be read.
func readExploreFile(root, rel string, o exploreOpts) (text string, ok bool) {
	if skipExplorePath(rel) || !allowedFile(rel, o) {
		return "", false
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	if !insideDir(root, p) {
		return "", false
	}
	// Every directory between root and the file must be a plain directory:
	// this rejects symlinked parents and Windows junctions.
	parts := strings.Split(filepath.ToSlash(rel), "/")
	cur := root
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil || !fi.IsDir() || fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return "", false
		}
		if cur != root && cur != p && hasDotGit(cur) {
			return "", false // inside a nested repository
		}
	}
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxExploreFile {
		return "", false
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil || !insideDir(root, real) {
		return "", false
	}
	raw, err := os.ReadFile(real)
	if err != nil || isBinary(raw) {
		return "", false
	}
	return redactLines(string(raw)), true
}

// insideDir reports whether p is dir or below it (both absolute, clean).
func insideDir(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// hasDotGit reports whether dir holds its own .git entry (a nested repo).
// An entry that cannot even be checked is treated as one.
func hasDotGit(dir string) bool { return dotGitState(dir) != dotGitAbsent }

type dotGit int

const (
	dotGitAbsent dotGit = iota
	dotGitPresent
	dotGitUnknown // the check failed: callers must treat it as "skip"
)

func dotGitState(dir string) dotGit {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	switch {
	case err == nil:
		return dotGitPresent
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		return dotGitAbsent
	}
	return dotGitUnknown
}

// --- ignore files: relevance filtering only --------------------------------

// perDirIgnoreFiles are read in every directory (the last two are the
// ripgrep/fd conventions).
var perDirIgnoreFiles = []string{".gitignore", ".ignore", ".rgignore"}

// ignoreSet holds the ignore rules that trim the walk. It is best-effort:
// a source that cannot be loaded is skipped and counted. Nothing it decides
// is a safety control (see the safety model at the top of this file).
type ignoreSet struct {
	top     string                  // repo work tree top, or the walk root
	rules   map[string]ignore.Rules // per directory (absolute OS path)
	global  ignore.Rules            // info/exclude + global excludes, relative to top
	skipped int                     // sources that could not be applied
}

func newIgnoreSet(root string) *ignoreSet {
	s := &ignoreSet{top: root, rules: map[string]ignore.Rules{}}
	var repoConfigs []string
	if repo, found, err := ignore.FindRepo(root); err != nil {
		s.skipped++ // unresolvable .git: no info/exclude or repo config
	} else if found {
		s.top = repo.Top
		repoConfigs = []string{filepath.Join(repo.CommonDir, "config"), filepath.Join(repo.GitDir, "config.worktree")}
		s.addGlobal(filepath.Join(repo.CommonDir, "info", "exclude"))
	}
	files, problems := ignore.GlobalExcludeFiles(repoConfigs...)
	s.skipped += len(problems)
	for _, f := range files {
		s.addGlobal(f)
	}
	for d := filepath.Dir(root); d != root && insideDir(s.top, d); d = filepath.Dir(d) {
		s.loadDir(d)
		if d == s.top || filepath.Dir(d) == d {
			break
		}
	}
	return s
}

func (s *ignoreSet) addGlobal(path string) {
	r, err := ignore.LoadRules(path, false)
	if err != nil {
		s.skipped++
		return
	}
	s.global.Patterns = append(s.global.Patterns, r.Patterns...)
}

func (s *ignoreSet) loadDir(dir string) {
	var all ignore.Rules
	for _, name := range perDirIgnoreFiles {
		r, err := ignore.LoadRules(filepath.Join(dir, name), true)
		if err != nil {
			s.skipped++
			continue
		}
		all.Patterns = append(all.Patterns, r.Patterns...)
	}
	if len(all.Patterns) > 0 {
		s.rules[dir] = all
	}
}

// ignored checks an absolute path against the global rules and the rules
// of every directory from its parent up to the repo top.
func (s *ignoreSet) ignored(abs string, isDir bool) bool {
	match := func(base string, r ignore.Rules) bool {
		rel, err := filepath.Rel(base, abs)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return false
		}
		return r.Match(filepath.ToSlash(rel), isDir)
	}
	if len(s.global.Patterns) > 0 && match(s.top, s.global) {
		return true
	}
	for d := filepath.Dir(abs); ; d = filepath.Dir(d) {
		if rules, ok := s.rules[d]; ok && match(d, rules) {
			return true
		}
		if d == s.top || !insideDir(s.top, d) || filepath.Dir(d) == d {
			return false
		}
	}
}

// --- walk --------------------------------------------------------------------

// walkFiles visits every file explore may read under root, in walk order,
// with its redacted text. visit returns false to stop.
func walkFiles(root string, o exploreOpts, visit func(rel, text string) bool) (walkStats, error) {
	var stats walkStats
	ign := newIgnoreSet(root)
	stop := false
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if stop {
			return filepath.SkipAll
		}
		if err != nil {
			if d == nil || d.IsDir() || p == root {
				stats.UnreadableDirs++
				if p == root {
					return filepath.SkipAll
				}
				return filepath.SkipDir
			}
			return nil
		}
		// WalkDir never descends into a symlinked directory (it reports the
		// link itself); links and reparse points are skipped here and again
		// in readExploreFile.
		if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return nil
		}
		if d.IsDir() {
			if p == root {
				ign.loadDir(p)
				return nil
			}
			if exploreSkipDirs[strings.ToLower(d.Name())] {
				return filepath.SkipDir
			}
			switch dotGitState(p) {
			case dotGitPresent:
				stats.NestedRepos++
				return filepath.SkipDir
			case dotGitUnknown:
				// Cannot tell (e.g. the directory is not readable): skip it,
				// counted as unreadable rather than as a nested repo.
				stats.UnreadableDirs++
				return filepath.SkipDir
			}
			if ign.ignored(p, true) {
				return filepath.SkipDir
			}
			ign.loadDir(p)
			return nil
		}
		r, rerr := filepath.Rel(root, p)
		if rerr != nil || !d.Type().IsRegular() {
			return nil
		}
		rel := filepath.ToSlash(r)
		// Cheap type checks first: most files never get past the allowlist.
		if skipExplorePath(rel) || !allowedFile(rel, o) || ign.ignored(p, false) {
			return nil
		}
		text, ok := readExploreFile(root, rel, o)
		if !ok {
			return nil
		}
		if !visit(rel, text) {
			stop = true
		}
		return nil
	})
	stats.IgnoreSkipped = ign.skipped
	return stats, err
}

func clipLine(s string) string {
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

func isBinary(raw []byte) bool {
	n := len(raw)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(raw[:n], 0) >= 0
}

// fileCache reads each candidate file once, for line counts and span text,
// through readExploreFile (same rules, same redaction).
type fileCache struct {
	root  string
	opts  exploreOpts
	lines map[string][]string
}

func (f *fileCache) get(rel string) []string {
	if l, ok := f.lines[rel]; ok {
		return l
	}
	var l []string
	if text, ok := readExploreFile(f.root, rel, f.opts); ok {
		l = strings.Split(strings.TrimRight(text, "\n"), "\n")
	}
	f.lines[rel] = l
	return l
}

func (f *fileCache) count(rel string) int { return len(f.get(rel)) }

func (f *fileCache) text(rel string, start, end int) string {
	l := f.get(rel)
	if start < 1 {
		start = 1
	}
	if end > len(l) {
		end = len(l)
	}
	if start > end {
		return ""
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%d: %s\n", i, strings.TrimRight(l[i-1], "\r"))
	}
	return b.String()
}
