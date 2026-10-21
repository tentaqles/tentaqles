// Package commitscan looks for secrets in what a `git commit` command is about
// to record, before the command runs. It closes the gap a plain pre-commit
// view leaves in a chained command such as `git add -A && git commit -m x`:
// at PreToolUse time nothing is staged yet, so the scan also covers what the
// chained `git add` / `commit -a` will pick up.
package commitscan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

// envFileRe names files that must never be committed.
var envFileRe = regexp.MustCompile(`(?i)(^|/)\.env(\.[a-z0-9_-]+)?$|(^|/)id_(rsa|ed25519|ecdsa|dsa)$|\.(pem|p12|pfx|key)$|(^|/)credentials\.json$`)
var envTemplateRe = regexp.MustCompile(`(?i)\.env\.(example|sample|template|dist|defaults|schema)$`)

// Plan says what a command will commit.
type Plan struct {
	Commits    bool // the command runs git commit
	WorkingDir bool // tracked, unstaged changes get committed too (commit -a, or a chained git add)
	CommitAll  bool // commit -a/--all: every tracked change, regardless of AddPaths
	Untracked  bool // new files get committed too (a chained git add)
	// AddPaths limits the working-tree/untracked scan to what a chained
	// `git add <paths>` stages. Empty with Untracked set means everything
	// (git add -A / --all / . / :/).
	AddPaths []string
}

// PlanFor inspects command. A command without git commit yields Commits=false.
// Heredoc bodies are data, not commands, so they are ignored.
func PlanFor(command string) Plan {
	var p Plan
	sawAdd, addAll := false, false
	var paths []string
	for _, inv := range guard.GitInvocations(guard.StripHeredocs(command)) {
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
			for _, a := range inv.Args {
				if a == "--all" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "a")) {
					p.WorkingDir = true
					p.CommitAll = true
				}
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

const maxFileBytes = 1 << 20

// Scan returns every secret the command would commit, scanning git output in
// dir. A git error (not a repo, no HEAD yet) narrows the scan rather than
// failing: the commit itself will fail or record nothing in those cases.
func Scan(dir, command string, run Runner) []Hit {
	p := PlanFor(command)
	if !p.Commits {
		return nil
	}
	var hits []Hit
	seen := map[string]bool{}
	add := func(h Hit) {
		k := h.String()
		if !seen[k] {
			seen[k] = true
			hits = append(hits, h)
		}
	}

	diffs := [][]string{{"diff", "--cached", "--no-color", "--no-ext-diff", "-U0"}}
	names := [][]string{{"diff", "--cached", "--name-only"}}
	if p.WorkingDir {
		wd := []string{"diff", "--no-color", "--no-ext-diff", "-U0"}
		wn := []string{"diff", "--name-only"}
		if len(p.AddPaths) > 0 && !p.CommitAll {
			wd = append(append(wd, "--"), p.AddPaths...)
			wn = append(append(wn, "--"), p.AddPaths...)
		}
		diffs = append(diffs, wd)
		names = append(names, wn)
	}
	for _, args := range names {
		out, err := run(dir, args...)
		if err != nil {
			continue
		}
		for _, f := range splitLines(out) {
			if bannedFile(f) {
				add(Hit{File: f, Pattern: "secret_file"})
			}
		}
	}
	for _, args := range diffs {
		out, err := run(dir, args...)
		if err != nil {
			continue
		}
		for _, h := range scanDiff(out) {
			add(h)
		}
	}
	if p.Untracked {
		out, err := run(dir, "ls-files", "--others", "--exclude-standard")
		if err == nil {
			for _, f := range splitLines(out) {
				if !p.inScope(f) {
					continue
				}
				if bannedFile(f) {
					add(Hit{File: f, Pattern: "secret_file"})
					continue
				}
				for _, fd := range scanFile(filepath.Join(dir, filepath.FromSlash(f))) {
					add(Hit{File: f, Line: fd.Line, Pattern: fd.Pattern})
				}
			}
		}
	}
	return hits
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

// Added returns every added line the command would commit, in the same scope
// Scan uses (staged diff; plus the working tree and untracked files when the
// command also runs git add or commit -a). Binary files are skipped.
func Added(dir, command string, run Runner) ([]AddedLine, error) {
	p := PlanFor(command)
	if !p.Commits {
		return nil, nil
	}
	diffs := [][]string{{"diff", "--cached", "--no-color", "--no-ext-diff", "-U0"}}
	if p.WorkingDir {
		wd := []string{"diff", "--no-color", "--no-ext-diff", "-U0"}
		if len(p.AddPaths) > 0 && !p.CommitAll {
			wd = append(append(wd, "--"), p.AddPaths...)
		}
		diffs = append(diffs, wd)
	}
	var out []AddedLine
	for _, args := range diffs {
		o, err := run(dir, args...)
		if err != nil {
			return nil, fmt.Errorf("git %s: %w", args[0], err)
		}
		out = append(out, ParseDiffAdded(o)...)
	}
	if p.Untracked {
		o, err := run(dir, "ls-files", "--others", "--exclude-standard")
		if err != nil {
			return nil, fmt.Errorf("git ls-files: %w", err)
		}
		{
			for _, f := range splitLines(o) {
				if !p.inScope(f) {
					continue
				}
				path := filepath.Join(dir, filepath.FromSlash(f))
				fi, err := os.Stat(path)
				if err != nil || fi.IsDir() || fi.Size() > maxFileBytes {
					continue
				}
				raw, err := os.ReadFile(path)
				if err != nil || isBinary(raw) {
					continue
				}
				for i, l := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
					if l == "" && i > 0 {
						continue
					}
					out = append(out, AddedLine{File: f, Line: i + 1, Text: l})
				}
			}
		}
	}
	return out, nil
}

var hunkRe = regexp.MustCompile(`\+(\d+)`)

func hunkStart(h string) int {
	m := hunkRe.FindStringSubmatch(h)
	if m == nil {
		return 0
	}
	n := 0
	fmt.Sscanf(m[1], "%d", &n)
	return n
}

func scanFile(path string) []secrets.Finding {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Size() > maxFileBytes {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil || isBinary(raw) {
		return nil
	}
	return secrets.Scan(string(raw))
}

func isBinary(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
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
