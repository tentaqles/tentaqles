package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// worktree is one linked (non-main) git worktree.
type worktree struct {
	Repo, Path, Branch string
	Detached, Locked   bool
	Dirty, Merged      bool
	Age                time.Duration // since the last commit on its HEAD
	Missing            bool          // directory is gone (prunable)
}

// prunable is the conservative rule `prune` applies: never a dirty or locked
// worktree; its work must already be in the default branch (or the directory
// is already gone), and it must be older than minAge.
func (w worktree) prunable(minAge time.Duration) bool {
	if w.Missing {
		return true
	}
	return !w.Dirty && !w.Locked && w.Merged && w.Age >= minAge
}

func newWorktreesCmd() *cobra.Command {
	c := &cobra.Command{Use: "worktrees", Short: "List and prune leftover agent git worktrees across workspaces"}
	var minDays int
	list := &cobra.Command{
		Use:   "list",
		Short: "List linked worktrees in every workspace repo, marking the ones prune would remove",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			wts, err := scanWorktrees()
			if err != nil {
				return err
			}
			minAge := time.Duration(minDays) * 24 * time.Hour
			n := 0
			for _, w := range wts {
				mark := "keep "
				if w.prunable(minAge) {
					mark = "PRUNE"
					n++
				}
				fmt.Fprintf(c.OutOrStdout(), "%s  %-40s %-28s %s\n", mark, short(w.Path, 40), short(w.Branch, 28), w.why())
			}
			fmt.Fprintf(c.OutOrStdout(), "\n%d worktree(s), %d prunable (clean, merged, older than %d days, or already deleted). Run: tq worktrees prune --yes\n", len(wts), n, minDays)
			return nil
		},
	}
	var yes bool
	prune := &cobra.Command{
		Use:   "prune",
		Short: "Remove clean, merged, old worktrees (git worktree remove; never --force)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			wts, err := scanWorktrees()
			if err != nil {
				return err
			}
			minAge := time.Duration(minDays) * 24 * time.Hour
			repos := map[string]bool{}
			for _, w := range wts {
				if !w.prunable(minAge) {
					continue
				}
				if !yes {
					fmt.Fprintf(c.OutOrStdout(), "would remove %s (%s)\n", w.Path, w.why())
					continue
				}
				repos[w.Repo] = true
				if w.Missing {
					continue // git worktree prune below drops the record
				}
				if out, err := gitcfg.RunGitIn(w.Repo, "worktree", "remove", w.Path); err != nil {
					fmt.Fprintf(c.ErrOrStderr(), "skip %s: %s\n", w.Path, strings.TrimSpace(out))
					continue
				}
				fmt.Fprintf(c.OutOrStdout(), "removed %s\n", w.Path)
			}
			for r := range repos {
				_, _ = gitcfg.RunGitIn(r, "worktree", "prune")
			}
			if !yes {
				fmt.Fprintln(c.OutOrStdout(), "dry run; pass --yes to remove")
			}
			return nil
		},
	}
	prune.Flags().BoolVar(&yes, "yes", false, "actually remove (default is a dry run)")
	for _, sub := range []*cobra.Command{list, prune} {
		sub.Flags().IntVar(&minDays, "older-than", 3, "only worktrees whose last commit is older than N days")
	}
	c.AddCommand(list, prune)
	return c
}

func (w worktree) why() string {
	if w.Missing {
		return "directory deleted"
	}
	var bits []string
	if w.Dirty {
		bits = append(bits, "dirty")
	}
	if w.Locked {
		bits = append(bits, "locked")
	}
	if w.Merged {
		bits = append(bits, "merged")
	} else {
		bits = append(bits, "unmerged")
	}
	bits = append(bits, fmt.Sprintf("%dd old", int(w.Age.Hours()/24)))
	return strings.Join(bits, ", ")
}

func short(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n+1:]
}

// scanWorktrees finds git repos at the root of each workspace and one level
// below it, and lists their linked worktrees.
func scanWorktrees() ([]worktree, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	wss, _ := resolve.ListWorkspaces(cfg)
	seen := map[string]bool{}
	var out []worktree
	for _, ws := range wss {
		cands := []string{ws.Root}
		if ents, err := os.ReadDir(ws.Root); err == nil {
			for _, e := range ents {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					cands = append(cands, filepath.Join(ws.Root, e.Name()))
				}
			}
		}
		for _, dir := range cands {
			if fi, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !fi.IsDir() {
				continue // not a main checkout (linked worktrees have a .git file)
			}
			key := strings.ToLower(filepath.Clean(dir))
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, repoWorktrees(dir)...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func repoWorktrees(repo string) []worktree {
	raw, err := gitcfg.RunGitIn(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	def := defaultBranch(repo)
	var out []worktree
	var cur *worktree
	first := true
	flush := func() {
		if cur != nil && !first {
			out = append(out, *cur)
		}
	}
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			if cur != nil {
				flush()
				first = false
			}
			cur = &worktree{Repo: repo, Path: filepath.FromSlash(strings.TrimPrefix(line, "worktree "))}
		case cur == nil:
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Detached = true
			cur.Branch = "(detached)"
		case strings.HasPrefix(line, "locked"):
			cur.Locked = true
		case strings.HasPrefix(line, "prunable"):
			cur.Missing = true
		}
	}
	if cur != nil && !first {
		out = append(out, *cur)
	}
	for i := range out {
		w := &out[i]
		if _, err := os.Stat(w.Path); err != nil {
			w.Missing = true
			continue
		}
		if s, err := gitcfg.RunGitIn(w.Path, "status", "--porcelain"); err == nil && strings.TrimSpace(s) != "" {
			w.Dirty = true
		}
		if ts, err := gitcfg.RunGitIn(w.Path, "log", "-1", "--format=%ct"); err == nil {
			if sec, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64); err == nil {
				w.Age = time.Since(time.Unix(sec, 0))
			}
		}
		if def != "" {
			// HEAD is merged when it is an ancestor of the default branch.
			_, err := gitcfg.RunGitIn(w.Path, "merge-base", "--is-ancestor", "HEAD", def)
			w.Merged = err == nil
		}
	}
	return out
}

func defaultBranch(repo string) string {
	if ref, err := gitcfg.RunGitIn(repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return strings.TrimSpace(ref)
	}
	for _, b := range []string{"main", "master"} {
		if _, err := gitcfg.RunGitIn(repo, "rev-parse", "--verify", "--quiet", b); err == nil {
			return b
		}
	}
	return ""
}
