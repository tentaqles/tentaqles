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
)

func newDecideExploreCmd() *cobra.Command {
	var dir string
	var top, candidates int
	var asJSON, noJev bool
	c := &cobra.Command{
		Use:   `explore "<question>"`,
		Short: "Find the few file:line ranges that answer a question about the code",
		Long: `Two stages, so an agent reads 5 ranges instead of 50 files:

  1. keyword pre-filter: a .gitignore-aware walk (no git process, so a
     repo's config can never run anything) for terms taken from the
     question; hits become ~40-line spans, grouped per file, best
     --candidates kept. Symlinks are never followed.
  2. one batched Jev call asks, per span, "is this relevant to answering the
     question?" and re-ranks them (split only to stay under the size cap).

Prints the top --top ranges with scores. When Jev is unavailable (backend
off, no key, not in a trusted workspace, an error) the keyword ranking is
printed instead and the output says so. .env files and key material are
never read or sent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			question := strings.TrimSpace(args[0])
			if question == "" {
				return errors.New("question is empty")
			}
			if dir == "" {
				dir, _ = os.Getwd()
			}
			root, err := exploreRoot(dir)
			if err != nil {
				return err
			}
			terms := decide.ExploreTerms(question, 8)
			if len(terms) == 0 {
				return errors.New("no searchable terms in the question: name an identifier, file or concept")
			}
			hits, stats, err := exploreHits(root, terms)
			if err != nil {
				return err
			}
			files := &fileCache{root: root, lines: map[string][]string{}}
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
			res.SkippedSubtrees = stats.Skipped
			res.NestedRepos = stats.NestedRepos
			return printExplore(c, res, asJSON)
		},
	}
	c.Flags().StringVar(&dir, "path", "", "directory to search (default: the current directory)")
	c.Flags().IntVar(&top, "top", 5, "ranges to print")
	c.Flags().IntVar(&candidates, "candidates", 30, "spans the keyword stage keeps for Jev")
	c.Flags().BoolVar(&asJSON, "json", false, "print the result as JSON")
	c.Flags().BoolVar(&noJev, "no-jev", false, "keyword ranking only (no network)")
	return c
}

func printExplore(c *cobra.Command, res decide.ExploreResult, asJSON bool) error {
	out := c.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	// The footer gives a count only: never the path of a file or directory
	// that was left out (its name may itself look like a secret).
	footer := func() {
		if res.SkippedSubtrees > 0 {
			fmt.Fprintf(out, "note: skipped %d subtree(s) governed by an ignore source or directory explore could not load or parse (fail closed)\n", res.SkippedSubtrees)
		}
		if res.NestedRepos > 0 {
			fmt.Fprintf(out, "note: skipped %d nested repo(s); run explore with --path inside one to search it\n", res.NestedRepos)
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

// maxExploreFile skips generated blobs and data dumps in the walk.
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

// walkStats reports what the walk refused to look at.
type walkStats struct {
	// Skipped counts subtrees left out because an ignore source governing
	// them exists but could not be loaded or parsed, or the directory
	// itself could not be read (fail closed).
	Skipped int
	// NestedRepos counts repositories below the root (a .git dir or
	// gitdir: file) that were not entered: their own ignore rules differ,
	// and they are often other projects or other clients' code.
	NestedRepos int
}

// exploreHits runs the keyword pre-filter. Paths come back slash-separated
// and relative to root.
//
// It deliberately does not shell out to git. `git grep` in a repo the user
// merely cloned reads that repo's .git/config, and core.fsmonitor,
// core.pager, diff.external, textconv drivers and similar settings run
// arbitrary programs; flag-by-flag hardening has to keep up with every new
// such key. The walk executes nothing and reads git's ignore sources as
// text (internal/ignore), erring toward exclusion: anything git would
// ignore is never read.
func exploreHits(root string, terms []string) ([]decide.Hit, walkStats, error) {
	var hits []decide.Hit
	stats, err := walkFiles(root, func(rel string, raw []byte) bool {
		for i, line := range strings.Split(string(raw), "\n") {
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

// exploreSkipDirs are never walked: VCS internals, dependency and build
// output, and credential stores.
var exploreSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, ".venv": true,
	"venv": true, "__pycache__": true, ".next": true, ".cache": true, "target": true, ".idea": true,
	".aws": true, ".ssh": true, ".gnupg": true, ".azure": true, ".kube": true, ".docker": true,
}

// denyNames are credential files by exact (lowercase) name.
var denyNames = map[string]bool{
	".npmrc": true, ".pypirc": true, ".netrc": true, "_netrc": true, ".git-credentials": true,
	".pgpass": true, ".htpasswd": true,
	"package-lock.json": true, "go.sum": true, "pnpm-lock.yaml": true, "yarn.lock": true,
}

// denyExts are key material, credential stores and data dumps.
var denyExts = map[string]bool{
	".pem": true, ".key": true, ".p12": true, ".pfx": true, ".jks": true, ".kdbx": true, ".keystore": true,
	".sqlite": true, ".sqlite3": true, ".db": true, ".dump": true, ".bak": true,
	".lock": true, ".map": true,
}

// skipExplorePath is the always-deny list, applied whatever the ignore
// files say: secrets, key material, dumps (never read or sent), and
// lockfiles/minified bundles (noise).
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

// readExploreFile reads rel (slash-separated, relative to root) only when it
// is a regular file — not a symlink, device or pipe — that resolves inside
// root, so a link (or a linked parent directory) can never pull in
// ~/.ssh/id_rsa or another client's repo. root must be symlink-free
// (exploreRoot). ok is false when the file must not be read.
func readExploreFile(root, rel string) (raw []byte, ok bool) {
	if skipExplorePath(rel) {
		return nil, false
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	if !insideDir(root, p) {
		return nil, false
	}
	// Every directory between root and the file must be a plain directory:
	// this rejects symlinked parents and Windows junctions, which Lstat
	// does not report as symlinks and EvalSymlinks may not resolve.
	parts := strings.Split(filepath.ToSlash(rel), "/")
	cur := root
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil || !fi.IsDir() || fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return nil, false
		}
	}
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxExploreFile {
		return nil, false
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil || !insideDir(root, real) {
		return nil, false
	}
	raw, err = os.ReadFile(real)
	if err != nil || isBinary(raw) {
		return nil, false
	}
	return raw, true
}

// insideDir reports whether p is dir or below it (both absolute, clean).
func insideDir(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// perDirIgnoreFiles are read in every directory (the last two are the
// ripgrep/fd conventions; honouring them only excludes more).
var perDirIgnoreFiles = []string{".gitignore", ".ignore", ".rgignore"}

// ignoreSet holds every ignore source that governs the walk. Every way a
// source can be present but not applied is an error that blocks what it
// governs; nothing defaults to "not ignored" after an error.
type ignoreSet struct {
	top   string                  // repo work tree top, or the walk root
	rules map[string]ignore.Rules // per directory (absolute OS path)
	// global holds info/exclude and the global excludes files; their
	// patterns are relative to top.
	global ignore.Rules
	// blocked: a source governing the whole walk could not be applied.
	blocked bool
}

// newIgnoreSet loads the sources that apply before the walk starts: the
// repo (resolved through gitdir:/commondir), info/exclude, every config's
// core.excludesFile, and the ignore files of root's ancestors up to the
// repo top. Any failure there blocks the whole walk.
func newIgnoreSet(root string) *ignoreSet {
	s := &ignoreSet{top: root, rules: map[string]ignore.Rules{}}
	var repoConfigs []string
	repo, found, err := ignore.FindRepo(root)
	if err != nil {
		s.blocked = true
		return s
	}
	if found {
		s.top = repo.Top
		repoConfigs = []string{filepath.Join(repo.CommonDir, "config"), filepath.Join(repo.GitDir, "config.worktree")}
		s.addGlobal(filepath.Join(repo.CommonDir, "info", "exclude"))
	}
	files, err := ignore.GlobalExcludeFiles(repoConfigs...)
	if err != nil {
		s.blocked = true
		return s
	}
	for _, f := range files {
		s.addGlobal(f)
	}
	// Ancestors between the repo top and root (root itself is loaded by
	// the walk).
	for d := filepath.Dir(root); d != root && insideDir(s.top, d); d = filepath.Dir(d) {
		if !s.loadDir(d) {
			s.blocked = true
		}
		if d == s.top || filepath.Dir(d) == d {
			break
		}
	}
	return s
}

// addGlobal loads one repo-wide source; absent is fine, anything else that
// keeps it from applying blocks the walk.
func (s *ignoreSet) addGlobal(path string) {
	r, err := ignore.LoadRules(path, false)
	if err != nil {
		s.blocked = true
		return
	}
	s.global.Patterns = append(s.global.Patterns, r.Patterns...)
}

// loadDir reads dir's own ignore files. Symlinked ignore files are not
// followed (an error, like an unreadable or unparsable one). false means
// the caller must skip everything dir governs.
func (s *ignoreSet) loadDir(dir string) bool {
	var all ignore.Rules
	for _, name := range perDirIgnoreFiles {
		r, err := ignore.LoadRules(filepath.Join(dir, name), true)
		if err != nil {
			return false
		}
		all.Patterns = append(all.Patterns, r.Patterns...)
	}
	if len(all.Patterns) > 0 {
		s.rules[dir] = all
	}
	return true
}

// ignored checks an absolute path against the global rules and the rules
// of every directory from its parent up to the repo top. A path it cannot
// relate to a rule's base counts as ignored.
func (s *ignoreSet) ignored(abs string, isDir bool) bool {
	match := func(base string, r ignore.Rules) bool {
		rel, err := filepath.Rel(base, abs)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return true // cannot evaluate: exclude
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

// nestedRepo reports whether dir (below the root) holds its own .git entry.
// An entry that cannot even be checked is treated as one.
func nestedRepo(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil || !(errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR))
}

// walkFiles visits every file explore may read under root, in walk order,
// with its content. visit returns false to stop.
func walkFiles(root string, visit func(rel string, raw []byte) bool) (walkStats, error) {
	var stats walkStats
	ign := newIgnoreSet(root)
	if ign.blocked {
		stats.Skipped = 1
		return stats, nil
	}
	stop := false
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if stop {
			return filepath.SkipAll
		}
		if err != nil {
			// A directory that cannot be listed (or vanished) is not
			// entered; its rules may be unreadable too. Nothing under it is
			// read, and the skip is counted.
			if d == nil || d.IsDir() || p == root {
				stats.Skipped++
				if p == root {
					return filepath.SkipAll
				}
				return filepath.SkipDir
			}
			return nil // a file that cannot be stat'ed is simply not read
		}
		// WalkDir never descends into a symlinked directory (it reports
		// the link itself); links and reparse points are skipped here and
		// again in readExploreFile.
		if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return nil
		}
		rel := "."
		if p != root {
			r, rerr := filepath.Rel(root, p)
			if rerr != nil {
				if d.IsDir() {
					stats.Skipped++
					return filepath.SkipDir
				}
				return nil
			}
			rel = filepath.ToSlash(r)
		}
		if d.IsDir() {
			if p != root && (exploreSkipDirs[strings.ToLower(d.Name())] || ign.ignored(p, true)) {
				return filepath.SkipDir
			}
			if p != root && nestedRepo(p) {
				stats.NestedRepos++
				return filepath.SkipDir
			}
			if !ign.loadDir(p) {
				stats.Skipped++
				if p == root {
					return filepath.SkipAll
				}
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || ign.ignored(p, false) {
			return nil
		}
		raw, ok := readExploreFile(root, rel)
		if !ok {
			return nil
		}
		if !visit(rel, raw) {
			stop = true
		}
		return nil
	})
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
// through readExploreFile (same symlink and containment checks).
type fileCache struct {
	root  string
	lines map[string][]string
}

func (f *fileCache) get(rel string) []string {
	if l, ok := f.lines[rel]; ok {
		return l
	}
	var l []string
	if raw, ok := readExploreFile(f.root, rel); ok {
		l = strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
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
