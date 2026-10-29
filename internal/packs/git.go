package packs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// GitTimeout bounds every git call.
const GitTimeout = 10 * time.Second

// maxShow caps one `git show` (64 MiB).
const maxShow = 64 << 20

// Git runs git -C root with a 10 s timeout and the process environment
// (which carries the workspace identity in a tq shell). It returns stdout.
func Git(ctx context.Context, root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, GitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	var out, errb bytes.Buffer
	lim := &limited{w: &out, n: maxShow}
	cmd.Stdout = lim
	cmd.Stderr = &limited{w: &errb, n: 64 << 10}
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("git %s: timed out after %s", args[0], GitTimeout)
		}
		msg := strings.TrimSpace(errb.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		return nil, fmt.Errorf("git %s: %v: %s", args[0], err, msg)
	}
	if lim.over {
		return nil, fmt.Errorf("git %s: output over %d bytes", args[0], maxShow)
	}
	return out.Bytes(), nil
}

type limited struct {
	w    *bytes.Buffer
	n    int
	over bool
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.n - l.w.Len(); room > 0 {
		if len(p) > room {
			l.w.Write(p[:room])
			l.over = true
		} else {
			l.w.Write(p)
		}
	} else if len(p) > 0 {
		l.over = true
	}
	return len(p), nil
}

// RepoRoot returns the git top level of dir.
func RepoRoot(ctx context.Context, dir string) (string, error) {
	out, err := Git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.FromSlash(strings.TrimSpace(string(out))), nil
}

// GitRepo is the GitReader over a repository.
type GitRepo struct {
	Root string
	Ctx  context.Context
}

func (g GitRepo) ctx() context.Context {
	if g.Ctx != nil {
		return g.Ctx
	}
	return context.Background()
}

// Show returns path at ref; ref ":" is the index.
func (g GitRepo) Show(ref, path string) ([]byte, error) {
	spec := ref + ":" + path
	if ref == ":" {
		spec = ":" + path
	}
	b, err := Git(g.ctx(), g.Root, "show", spec)
	if err != nil && absentAtRef(err) {
		return nil, fmt.Errorf("%w: %s is absent at %s", os.ErrNotExist, path, ref)
	}
	return b, err
}

// absentAtRef recognises git's "path does not exist" messages.
func absentAtRef(err error) bool {
	m := err.Error()
	return strings.Contains(m, "does not exist in") || strings.Contains(m, "exists on disk, but not in") ||
		strings.Contains(m, "not in the index") || strings.Contains(m, "does not exist (neither on disk nor in the index)")
}

// ListFiles lists the tracked files.
func (g GitRepo) ListFiles() ([]string, error) {
	out, err := Git(g.ctx(), g.Root, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	return splitZ(out), nil
}

func splitZ(b []byte) []string {
	var out []string
	for _, s := range strings.Split(string(b), "\x00") {
		if s != "" {
			out = append(out, strings.ReplaceAll(s, `\`, "/"))
		}
	}
	return out
}

// Selection is the changed-set flag of tq pack check (spec C §2.5).
type Selection struct {
	Staged bool
	Paths  []string
	Base   string
	All    bool
}

// ErrSelection is a conflicting or bad selection (a usage error).
var ErrSelection = errors.New("bad selection")

// Select computes the changed set and the old-side ref.
func Select(ctx context.Context, root string, s Selection) ([]ChangedFile, string, error) {
	n := 0
	for _, on := range []bool{s.Staged, len(s.Paths) > 0, s.Base != "", s.All} {
		if on {
			n++
		}
	}
	if n > 1 {
		return nil, "", fmt.Errorf("%w: use only one of --staged, --paths, --base and --all", ErrSelection)
	}
	if strings.HasPrefix(s.Base, "-") {
		return nil, "", fmt.Errorf("%w: --base %q is not a ref", ErrSelection, s.Base)
	}
	head := "HEAD"
	if _, err := Git(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err != nil {
		head = "" // a repository with no commit yet
	}
	switch {
	case s.All:
		files, err := GitRepo{Root: root, Ctx: ctx}.ListFiles()
		if err != nil {
			return nil, "", err
		}
		out := make([]ChangedFile, 0, len(files))
		for _, f := range files {
			out = append(out, ChangedFile{Path: f, Status: 'A'})
		}
		return out, "", nil
	case len(s.Paths) > 0:
		return selectPaths(ctx, root, head, s.Paths)
	case s.Staged:
		// With no commit yet, git compares the index with the empty tree.
		out, err := Git(ctx, root, "diff", "--cached", "--name-status", "-z")
		if err != nil {
			return nil, "", err
		}
		return parseNameStatus(out), head, nil
	}
	base := s.Base
	if base == "" {
		base = head
	}
	var changed []ChangedFile
	if base != "" {
		out, err := Git(ctx, root, "diff", "--name-status", "-z", base, "--")
		if err != nil {
			return nil, "", err
		}
		changed = parseNameStatus(out)
	} else {
		out, err := Git(ctx, root, "diff", "--cached", "--name-status", "-z")
		if err == nil {
			changed = parseNameStatus(out)
		}
	}
	untracked, err := Git(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, "", err
	}
	for _, p := range splitZ(untracked) {
		changed = append(changed, ChangedFile{Path: p, Status: 'A'})
	}
	return changed, base, nil
}

// parseNameStatus reads `git diff --name-status -z`: "M\0path\0",
// "R100\0old\0new\0", "C75\0old\0new\0".
func parseNameStatus(b []byte) []ChangedFile {
	parts := strings.Split(string(b), "\x00")
	var out []ChangedFile
	for i := 0; i < len(parts); i++ {
		st := parts[i]
		if st == "" {
			continue
		}
		switch st[0] {
		case 'R', 'C':
			if i+2 >= len(parts) {
				return out
			}
			old, nw := parts[i+1], parts[i+2]
			i += 2
			s := byte('R')
			if st[0] == 'C' {
				s = 'A'
				old = ""
			}
			out = append(out, ChangedFile{Path: slash(nw), OldPath: slash(old), Status: s})
		default:
			if i+1 >= len(parts) {
				return out
			}
			p := parts[i+1]
			i++
			s := st[0]
			if s != 'A' && s != 'D' {
				s = 'M' // M, T (type change), U (unmerged) are modifications
			}
			out = append(out, ChangedFile{Path: slash(p), Status: s})
		}
	}
	return out
}

func slash(p string) string { return strings.ReplaceAll(p, `\`, "/") }

// selectPaths resolves --paths: each path is cleaned and must stay inside
// the repository (an absolute path inside root is made relative); a
// directory expands to its tracked and untracked files; the status (M when
// present at HEAD, else A) comes from one ls-tree call, matched
// case-insensitively on Windows with git's own spelling kept.
func selectPaths(ctx context.Context, root, head string, in []string) ([]ChangedFile, string, error) {
	absRoot, _ := filepath.Abs(root)
	var rels []string
	for _, raw := range in {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "-") {
			return nil, "", fmt.Errorf("%w: --paths %q is not a path", ErrSelection, raw)
		}
		if filepath.IsAbs(p) {
			r, err := filepath.Rel(absRoot, p)
			if err != nil {
				return nil, "", fmt.Errorf("%w: --paths %q is outside the repository", ErrSelection, raw)
			}
			p = r
		}
		p = path.Clean(strings.ReplaceAll(p, `\`, "/"))
		if p == ".." || strings.HasPrefix(p, "../") || path.IsAbs(p) || (len(p) > 1 && p[1] == ':') {
			return nil, "", fmt.Errorf("%w: --paths %q is outside the repository", ErrSelection, raw)
		}
		if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil && fi.IsDir() {
			args := []string{"ls-files", "-z", "--cached", "--others", "--exclude-standard", "--"}
			if p != "." {
				args = append(args, p)
			}
			out, err := Git(ctx, root, args...)
			if err != nil {
				return nil, "", err
			}
			rels = append(rels, splitZ(out)...)
			continue
		}
		rels = append(rels, p)
	}
	atHead := map[string]string{} // key -> git's spelling
	key := func(s string) string {
		if runtime.GOOS == "windows" {
			return strings.ToLower(s)
		}
		return s
	}
	if runtime.GOOS == "windows" && len(rels) > 0 {
		// Git pathspecs are case-sensitive; take git's own spelling of each
		// tracked path first (one call, :(icase) magic).
		args := []string{"ls-files", "-z", "--"}
		for _, p := range rels {
			args = append(args, ":(icase,literal)"+p)
		}
		if out, err := Git(ctx, root, args...); err == nil {
			spell := map[string]string{}
			for _, f := range splitZ(out) {
				spell[strings.ToLower(f)] = f
			}
			for i, p := range rels {
				if g, ok := spell[strings.ToLower(p)]; ok {
					rels[i] = g
				}
			}
		}
	}
	if head != "" && len(rels) > 0 {
		args := append([]string{"ls-tree", "-r", "--name-only", "-z", head, "--"}, rels...)
		if out, err := Git(ctx, root, args...); err == nil {
			for _, f := range splitZ(out) {
				atHead[key(f)] = f
			}
		} else {
			return nil, "", err
		}
	}
	seen := map[string]bool{}
	var out []ChangedFile
	for _, p := range rels {
		k := key(p)
		if seen[k] {
			continue
		}
		seen[k] = true
		if g, ok := atHead[k]; ok {
			out = append(out, ChangedFile{Path: g, Status: 'M'})
		} else {
			out = append(out, ChangedFile{Path: p, Status: 'A'})
		}
	}
	return out, head, nil
}
