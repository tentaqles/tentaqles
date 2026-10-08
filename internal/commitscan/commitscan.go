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
	Untracked  bool // new files get committed too (a chained git add -A / . / <path>)
}

// PlanFor inspects command. A command without git commit yields Commits=false.
func PlanFor(command string) Plan {
	var p Plan
	sawAdd := false
	for _, inv := range guard.GitInvocations(command) {
		switch inv.Sub {
		case "add", "stage":
			sawAdd = true
		case "commit":
			p.Commits = true
			for _, a := range inv.Args {
				if a == "--all" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "a")) {
					p.WorkingDir = true
				}
			}
		}
	}
	if p.Commits && sawAdd {
		p.WorkingDir = true
		p.Untracked = true
	}
	return p
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
		diffs = append(diffs, []string{"diff", "--no-color", "--no-ext-diff", "-U0"})
		names = append(names, []string{"diff", "--name-only"})
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

// scanDiff walks a -U0 unified diff and scans only added lines, mapping them
// back to their line number in the new file.
func scanDiff(diff string) []Hit {
	var hits []Hit
	file := ""
	line := 0
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++ "):
			file = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
		case strings.HasPrefix(l, "@@"):
			line = hunkStart(l)
		case strings.HasPrefix(l, "+"):
			if name := secrets.ScanLine(l[1:]); name != "" && file != "/dev/null" {
				hits = append(hits, Hit{File: file, Line: line, Pattern: name})
			}
			line++
		}
	}
	return hits
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
