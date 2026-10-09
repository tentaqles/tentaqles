package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
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

  1. keyword pre-filter: git grep (or a .gitignore-aware walk outside a repo)
     for terms taken from the question; hits become ~40-line spans, grouped
     per file, best --candidates kept.
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
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
				return fmt.Errorf("--path %s: not a directory", dir)
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

// exploreHits runs the keyword pre-filter: git grep inside a work tree, a
// .gitignore-aware walk elsewhere. Paths come back slash-separated and
// relative to root. Secret-bearing files are dropped either way.
func exploreHits(root string, terms []string) ([]decide.Hit, error) {
	if isGitWorkTree(root) {
		hits, err := gitGrepHits(root, terms)
		if err == nil {
			return hits, nil
		}
	}
	return walkHits(root, terms)
}

func isGitWorkTree(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func gitGrepHits(root string, terms []string) ([]decide.Hit, error) {
	args := []string{"-c", "core.quotepath=off", "grep", "-n", "-I", "-i", "-F", "--null", "--no-color", "--untracked"}
	for _, t := range terms {
		args = append(args, "-e", t)
	}
	args = append(args, "--", ".")
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 && stderr.Len() == 0 {
			return nil, nil // no match
		}
		return nil, fmt.Errorf("git grep: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var hits []decide.Hit
	sc := bufio.NewScanner(&stdout)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() && len(hits) < maxExploreHits {
		parts := strings.SplitN(sc.Text(), "\x00", 3)
		if len(parts) < 3 {
			continue
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		p := path.Clean(filepath.ToSlash(parts[0]))
		if skipExplorePath(p) {
			continue
		}
		hits = append(hits, decide.Hit{Path: p, Line: n, Text: clipLine(parts[2])})
	}
	return hits, nil
}

func clipLine(s string) string {
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

// exploreSkipDirs are never walked outside a repo.
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

// ignoreRules is the subset of .gitignore the walk honours: one root
// file, basename and anchored path globs, trailing "/" for directories.
// Negations are ignored (a file they would re-include is simply skipped).
type ignoreRules struct{ pats []ignorePat }

type ignorePat struct {
	glob     string
	dirOnly  bool
	anchored bool
}

func loadIgnore(root string) ignoreRules {
	raw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return ignoreRules{}
	}
	var r ignoreRules
	for _, line := range strings.Split(string(raw), "\n") {
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

func (r ignoreRules) ignored(rel string, isDir bool) bool {
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

func walkHits(root string, terms []string) ([]decide.Hit, error) {
	ign := loadIgnore(root)
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
		if d.IsDir() {
			if exploreSkipDirs[strings.ToLower(d.Name())] || ign.ignored(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || skipExplorePath(rel) || ign.ignored(rel, false) {
			return nil
		}
		fi, err := d.Info()
		if err != nil || fi.Size() > maxExploreFile {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil || isBinary(raw) {
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

func isBinary(raw []byte) bool {
	n := len(raw)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(raw[:n], 0) >= 0
}

// fileCache reads each candidate file once, for line counts and span text.
type fileCache struct {
	root  string
	lines map[string][]string
}

func (f *fileCache) get(rel string) []string {
	if l, ok := f.lines[rel]; ok {
		return l
	}
	var l []string
	if !skipExplorePath(rel) {
		if raw, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(rel))); err == nil && len(raw) <= 4*maxExploreFile {
			l = strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
		}
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
