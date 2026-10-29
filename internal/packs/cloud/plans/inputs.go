package plans

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// MaxInputFiles bounds the files an inputs hash covers.
const MaxInputFiles = 20000

// InputsHash is the content hash of an IaC root plus its var files (§9.4):
// sha256 over the sorted "path\x00sha256(content)\n" lines of every file git
// tracks or would track (untracked, not ignored) under the paths, read from
// the working tree. Deleted files drop out. Paths are repo-relative slash
// paths ("." is the repo). It needs git (a bounded call); the guard never
// computes it.
func InputsHash(ctx context.Context, root string, paths []string) (string, error) {
	files, err := inputFiles(ctx, root, paths)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, f := range files {
		fh, err := fileHash(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%s\n", f, fh)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// MaxTreeFiles bounds the program files of Pulumi's weak binding: the guard
// re-reads each within its 50 ms budget.
const MaxTreeFiles = 64

// TreeFiles lists the program files under dir for Pulumi's weak binding:
// what git tracks or would track, without .terraform/ and without dotenv
// files (the guard never reads those). More than MaxTreeFiles is an error:
// record an update plan (--save-plan) instead.
func TreeFiles(ctx context.Context, root, dir string) ([]string, error) {
	files, err := inputFiles(ctx, root, []string{dir})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		if strings.HasPrefix(strings.ToLower(path.Base(f)), ".env") {
			continue
		}
		out = append(out, f)
	}
	if len(out) > MaxTreeFiles {
		return nil, fmt.Errorf("plans: the Pulumi program has %d files, over the %d the weak binding re-checks; record an update plan with --save-plan", len(out), MaxTreeFiles)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("plans: no program files under %s", dir)
	}
	return out, nil
}

func inputFiles(ctx context.Context, root string, paths []string) ([]string, error) {
	args := []string{"ls-files", "-z", "--cached", "--others", "--exclude-standard", "--"}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	args = append(args, paths...)
	out, err := packs.Git(ctx, root, args...)
	if err != nil {
		return nil, fmt.Errorf("plans: git ls-files: %w", err)
	}
	seen := map[string]bool{}
	var files []string
	for _, f := range bytes.Split(out, []byte{0}) {
		s := string(f)
		if s == "" || seen[s] || strings.Contains("/"+s, "/.terraform/") {
			continue
		}
		seen[s] = true
		files = append(files, s)
		if len(files) > MaxInputFiles {
			return nil, fmt.Errorf("plans: more than %d files under the IaC paths", MaxInputFiles)
		}
	}
	sort.Strings(files)
	return files, nil
}

func fileHash(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", os.ErrNotExist
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Clean reports whether git sees no change under the paths (tracked or
// untracked), for require_merged.
func Clean(ctx context.Context, root string, paths []string) (bool, error) {
	args := append([]string{"status", "--porcelain", "-z", "--untracked-files=all", "--"}, paths...)
	out, err := packs.Git(ctx, root, args...)
	if err != nil {
		return false, err
	}
	for _, e := range bytes.Split(out, []byte{0}) {
		s := string(e)
		if len(s) < 4 {
			continue // the source half of a rename, or empty
		}
		if !strings.Contains("/"+s[3:], "/.terraform/") {
			return false, nil
		}
	}
	return true, nil
}

// Head is git rev-parse HEAD ("" without a commit).
func Head(ctx context.Context, root string) string {
	out, err := packs.Git(ctx, root, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// DefaultRemoteRef is the default branch's remote ref: origin/HEAD's
// target, else origin/main, else origin/master ("" when none exists).
func DefaultRemoteRef(ctx context.Context, root string) string {
	if out, err := packs.Git(ctx, root, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return s
		}
	}
	for _, r := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master"} {
		if _, err := packs.Git(ctx, root, "rev-parse", "--verify", "-q", r); err == nil {
			return r
		}
	}
	return ""
}

// Merged reports whether commit is an ancestor of ref.
func Merged(ctx context.Context, root, commit, ref string) bool {
	if commit == "" || ref == "" {
		return false
	}
	_, err := packs.Git(ctx, root, "merge-base", "--is-ancestor", commit, ref)
	return err == nil
}

var subjectBad = regexp.MustCompile(`[^A-Za-z0-9_.:@/+-]`)

// Subject turns a dir or stack into an approval subject ([A-Za-z0-9_.:@/+-],
// at most 200 characters).
func Subject(s string) string {
	s = path.Clean(strings.ReplaceAll(s, `\`, "/"))
	s = subjectBad.ReplaceAllString(s, "_")
	if len(s) > 200 {
		s = s[:200]
	}
	if s == "" {
		s = "."
	}
	return s
}
