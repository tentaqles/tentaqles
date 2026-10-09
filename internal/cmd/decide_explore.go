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

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/decide"
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
			hits, err := exploreHits(root, terms)
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
	if res.Candidates == 0 {
		fmt.Fprintf(out, "no keyword hits for: %s\nTry an identifier, file name or error text from the code.\n", strings.Join(res.Terms, ", "))
		return nil
	}
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

// exploreHits runs the keyword pre-filter: a walk of root that honours
// .gitignore files and .git/info/exclude. Paths come back slash-separated
// and relative to root.
//
// It deliberately does not shell out to git. `git grep` in a repo the user
// merely cloned reads that repo's .git/config, and core.fsmonitor,
// core.pager, diff.external, textconv drivers and similar settings run
// arbitrary programs; flag-by-flag hardening has to keep up with every new
// such key. A walk in Go executes nothing. The cost is a simpler ignore
// matcher (no negations), which only affects which spans are candidates.
func exploreHits(root string, terms []string) ([]decide.Hit, error) {
	return walkHits(root, terms)
}

// exploreSkipDirs are never walked.
var exploreSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, ".venv": true,
	"venv": true, "__pycache__": true, ".next": true, ".cache": true, "target": true, ".idea": true,
}

// skipExplorePath drops files whose content must never be read or sent
// (dotenv files, private keys) and lockfiles that only add noise.
func skipExplorePath(p string) bool {
	base := strings.ToLower(path.Base(p))
	switch {
	case strings.HasPrefix(base, ".env"):
		return true
	case strings.HasPrefix(base, "id_rsa"), strings.HasPrefix(base, "id_ed25519"), strings.HasPrefix(base, "id_ecdsa"):
		return true
	case base == "package-lock.json", base == "go.sum", base == "pnpm-lock.yaml", base == "yarn.lock", strings.HasSuffix(base, ".lock"):
		return true
	case strings.HasSuffix(base, ".min.js"), strings.HasSuffix(base, ".map"):
		return true
	}
	switch path.Ext(base) {
	case ".pem", ".key", ".p12", ".pfx", ".jks", ".kdbx", ".keystore":
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

// ignoreRules is the subset of gitignore the walk honours: basename and
// anchored globs, trailing "/" for directories, one rule set per directory
// that has a .gitignore (plus .git/info/exclude at the root). Negations are
// ignored (a file they would re-include is simply skipped).
type ignoreRules struct{ pats []ignorePat }

type ignorePat struct {
	glob     string
	dirOnly  bool
	anchored bool
}

func parseIgnore(raw string) ignoreRules {
	var r ignoreRules
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		p := ignorePat{}
		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		line = strings.TrimPrefix(line, "**/")
		if strings.Contains(line, "/") {
			p.anchored = true
			line = strings.TrimPrefix(line, "/")
		}
		p.glob = line
		r.pats = append(r.pats, p)
	}
	return r
}

// match checks rel, relative to the directory owning these rules.
func (r ignoreRules) match(rel string, isDir bool) bool {
	base := path.Base(rel)
	for _, p := range r.pats {
		if p.dirOnly && !isDir {
			continue
		}
		target := base
		if p.anchored {
			target = rel
		}
		if ok, _ := path.Match(p.glob, target); ok {
			return true
		}
	}
	return false
}

// ignoreTree holds the rule sets found so far, keyed by directory ("" is
// the root).
type ignoreTree map[string]ignoreRules

// load reads dir's .gitignore (dir relative to root) without following a
// symlinked one.
func (t ignoreTree) load(root, dir string) {
	files := []string{".gitignore"}
	if dir == "" {
		files = append(files, ".git/info/exclude")
	}
	var all ignoreRules
	for _, f := range files {
		rel := path.Join(dir, f)
		p := filepath.Join(root, filepath.FromSlash(rel))
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxExploreFile {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		all.pats = append(all.pats, parseIgnore(string(raw)).pats...)
	}
	if len(all.pats) > 0 {
		t[dir] = all
	}
}

// ignored checks rel against the rules of every ancestor directory.
func (t ignoreTree) ignored(rel string, isDir bool) bool {
	dir := path.Dir(rel)
	for {
		if dir == "." {
			dir = ""
		}
		if r, ok := t[dir]; ok {
			sub := rel
			if dir != "" {
				sub = strings.TrimPrefix(rel, dir+"/")
			}
			if r.match(sub, isDir) {
				return true
			}
		}
		if dir == "" {
			return false
		}
		dir = path.Dir(dir)
	}
}

func walkHits(root string, terms []string) ([]decide.Hit, error) {
	ign := ignoreTree{}
	ign.load(root, "")
	var hits []decide.Hit
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || len(hits) >= maxExploreHits {
			if d != nil && d.IsDir() && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if p == root {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		// WalkDir never descends into a symlinked directory (it reports
		// the link itself, with ModeSymlink); links of any kind are skipped
		// here and again in readExploreFile.
		if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return nil // a link, or a Windows junction / other reparse point
		}
		if d.IsDir() {
			if exploreSkipDirs[strings.ToLower(d.Name())] || ign.ignored(rel, true) {
				return filepath.SkipDir
			}
			ign.load(root, rel)
			return nil
		}
		if !d.Type().IsRegular() || ign.ignored(rel, false) {
			return nil
		}
		raw, ok := readExploreFile(root, rel)
		if !ok {
			return nil
		}
		for i, line := range strings.Split(string(raw), "\n") {
			low := strings.ToLower(line)
			for _, t := range terms {
				if strings.Contains(low, t) {
					hits = append(hits, decide.Hit{Path: rel, Line: i + 1, Text: clipLine(line)})
					break
				}
			}
		}
		return nil
	})
	return hits, err
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
