package trust

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// A new version of the tentaqles plugin is signed again in batch (tq hooks
// verify --accept --all) only when its folder is the published release,
// byte for byte. The release is the commit this tq was built from
// (ReleaseCommit, injected into the binary by the release build), read from
// the tentaqles marketplace clone Claude Code keeps at
// <config dir>/plugins/marketplaces/tentaqles: every covered file of the new
// folder must have the blob that commit has at the same path under plugin/,
// and every covered file of the release must be in the folder. Nothing the
// clone says decides which commit that is: its marketplace.json pin is never
// read, so a clone that re-pins itself to a commit it forged gains nothing.
// Location is never enough: a file edited in the new folder, a file the
// release does not have (a planted bin/tq), a release file missing (an
// emptied hooks/), a tq built without a commit (a dev build), a clone that
// lacks the commit, no clone, an origin that is not
// github.com/tentaqles/tentaqles, or git failing, and the workspace waits for
// tq hooks verify --accept <ws>. A plugin release newer than this tq is not
// the commit tq was built from either: it needs the per-workspace accept
// until tq is upgraded. Git runs against the clone alone: a cleaned
// environment (no GIT_DIR, GIT_CONFIG*, ... nor the system or user config)
// and no file-system monitor. The clone is a trust anchor: P1.3's protected
// paths keep a session from writing it; a same-user process outside the
// harness is the accepted risk AR1.

// ReleaseCommit is the full commit this tq was built from; "" for a dev
// build. internal/cmd sets it at startup from cmd.Commit (the release
// build's ldflags); tests set it to their fixture's commit.
var ReleaseCommit = ""

// ErrNoReleaseCommit refuses the batch accept of a tq built without one.
var ErrNoReleaseCommit = errors.New("this tq build has no release commit; every workspace needs tq hooks verify --accept <ws>")

// Release is the published release a new plugin folder was compared with.
type Release struct {
	// Version is the new folder's version, SHA the commit compared with
	// (ReleaseCommit).
	Version, SHA string
	// Mismatches names each difference ("<path>: <why>"); none when the
	// folder is the release.
	Mismatches []string
}

// MarketplaceClone is the tentaqles marketplace clone Claude Code keeps for
// the Claude config dir claudeDir.
func MarketplaceClone(claudeDir string) string {
	return filepath.Join(claudeDir, "plugins", "marketplaces", "tentaqles")
}

// gitEnvDrop are the variables that would point git at another repository,
// object store, index, exec path or configuration than the clone's own:
// the batch accept compares with the clone and nothing else.
var gitEnvDrop = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_EXEC_PATH",
	"GIT_REPLACE_REF_BASE", "GIT_GRAFT_FILE", "GIT_NO_REPLACE_OBJECTS"}

// gitEnv is env without gitEnvDrop and every GIT_CONFIG* variable, with the
// system and global config off, no prompt and no pager.
func gitEnv(env []string) []string {
	out := make([]string, 0, len(env)+4)
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		u := strings.ToUpper(k) // Windows names are case-insensitive
		drop := strings.HasPrefix(u, "GIT_CONFIG")
		for _, d := range gitEnvDrop {
			if u == d {
				drop = true
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return append(out, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat",
		"GIT_NO_REPLACE_OBJECTS=1")
}

// gitCommand is git with args in the clone dir: the cleaned environment
// (gitEnv), no replace objects (a refs/replace/ ref would make one commit
// id stand for another tree), no file-system monitor (a command the
// clone's config could name), and the clone named safe so git's ownership
// check is about it alone.
func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	pre := []string{"--no-replace-objects", "-c", "core.fsmonitor=", "-c", "safe.directory=" + filepath.ToSlash(dir), "-C", dir}
	c := exec.CommandContext(ctx, "git", append(pre, args...)...)
	c.Env = gitEnv(os.Environ())
	return c
}

// gitOutput runs git in dir (gitCommand) with a deadline. A seam for tests;
// nothing else replaces it.
var gitOutput = func(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := gitCommand(ctx, dir, args...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// releaseOrigins are the remotes a marketplace clone may come from: the
// tentaqles repository on GitHub, over https or ssh.
var releaseOrigins = []string{
	"https://github.com/tentaqles/tentaqles", "https://github.com/tentaqles/tentaqles.git",
	"git@github.com:tentaqles/tentaqles", "git@github.com:tentaqles/tentaqles.git",
}

// checkOrigin refuses a clone whose origin is not the tentaqles repository:
// a release is the tentaqles one or none.
func checkOrigin(clone string) error {
	out, err := gitOutput(clone, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("the marketplace clone has no origin to check: %w", err)
	}
	url := strings.TrimSpace(string(out))
	for _, o := range releaseOrigins {
		if url == o {
			return nil
		}
	}
	return fmt.Errorf("the marketplace clone's origin is %q, not github.com/tentaqles/tentaqles", url)
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// cloneAltered names what in the clone's repository could make a commit id
// stand for other content, or "": objects borrowed from another store
// (objects/info/alternates), rewritten history (info/grafts), a shallow
// clone (shallow), or a replace ref (refs/replace/, loose or packed).
func cloneAltered(clone string) (string, error) {
	out, err := gitOutput(clone, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", fmt.Errorf("the marketplace clone is not a git repository: %w", err)
	}
	gitDir := filepath.FromSlash(strings.TrimSpace(string(out)))
	for _, rel := range []string{"objects/info/alternates", "info/grafts", "shallow"} {
		if _, err := os.Lstat(filepath.Join(gitDir, filepath.FromSlash(rel))); err == nil {
			return rel, nil
		}
	}
	refs, err := gitOutput(clone, "for-each-ref", "--format=%(refname)", "refs/replace/")
	if err != nil {
		return "", err
	}
	if r := strings.TrimSpace(string(refs)); r != "" {
		return strings.SplitN(r, "\n", 2)[0], nil
	}
	return "", nil
}

// releaseTree lists the blobs of plugin/ at commit in clone, by path relative
// to plugin/: the clone must be unaltered (cloneAltered), git rev-parse
// --verify <commit>^{commit} must answer exactly that commit, then git
// ls-tree -r of <commit>:plugin.
func releaseTree(clone, commit string) (map[string]string, error) {
	what, err := cloneAltered(clone)
	if err != nil {
		return nil, err
	}
	if what != "" {
		return nil, fmt.Errorf("the clone is altered: %s; every workspace needs tq hooks verify --accept <ws>", what)
	}
	notThere := fmt.Errorf("the clone does not have tq's release %s; every workspace needs tq hooks verify --accept <ws> (a plugin release newer than this tq needs it until tq is upgraded)", commit[:12])
	out, err := gitOutput(clone, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if err != nil {
		return nil, notThere
	}
	if got := strings.ToLower(strings.TrimSpace(string(out))); got != commit {
		return nil, fmt.Errorf("the clone resolves tq's release %s to %s", commit[:12], got)
	}
	out, err = gitOutput(clone, "ls-tree", "-r", "-z", commit+":plugin")
	if err != nil {
		return nil, fmt.Errorf("tq's release %s has no plugin folder in the clone: %w", commit[:12], err)
	}
	tree := map[string]string{}
	for _, entry := range strings.Split(string(out), "\x00") {
		if entry == "" {
			continue
		}
		meta, p, ok := strings.Cut(entry, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 || f[1] != "blob" {
			continue
		}
		tree[p] = f[0] + " " + f[2]
	}
	if len(tree) == 0 {
		return nil, fmt.Errorf("tq's release %s has no files under plugin/", commit[:12])
	}
	return tree, nil
}

// blobHash is the git blob id of content (git hash-object --no-filters:
// the bytes as they are, no line-ending conversion), sha1 or sha256 as the
// repo's ids are.
func blobHash(content []byte, sha256Repo bool) string {
	var h hash.Hash = sha1.New()
	if sha256Repo {
		h = sha256.New()
	}
	h.Write([]byte("blob " + strconv.Itoa(len(content)) + "\x00"))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// skippedInPlugin applies the coverage's skip rule to a path inside the
// plugin folder: the prompt, document and test folders at the top, and any
// __pycache__.
func skippedInPlugin(rel string) bool {
	parts := strings.Split(rel, "/")
	if len(parts) > 1 && pluginSkipTop[parts[0]] {
		return true
	}
	for _, p := range parts[:len(parts)-1] {
		if p == "__pycache__" {
			return true
		}
	}
	return false
}

// CheckRelease compares the plugin folder workspace name loads now with the
// published release this tq was built from (ReleaseCommit), for a report
// whose only changes are a new plugin version (Report.PluginBump). It
// returns an error when there is no release to compare with (a dev build, no
// clone, a foreign origin, a clone without the commit, git failing) and the
// differences in Release.Mismatches otherwise; the folder is the release
// only when there is no error and no mismatch.
func CheckRelease(name string, rep Report) (Release, error) {
	if !validKey(name) {
		return Release{}, fmt.Errorf("trust: invalid workspace name %q", name)
	}
	if rep.PluginFrom == "" || !rep.PluginBump {
		return Release{}, errors.New("the change is not a new plugin version alone")
	}
	commit := strings.ToLower(strings.TrimSpace(ReleaseCommit))
	if commit == "" {
		return Release{}, ErrNoReleaseCommit
	}
	if !isHex(commit) || (len(commit) != 40 && len(commit) != 64) {
		return Release{}, fmt.Errorf("this tq build's release commit %q is not a commit id", ReleaseCommit)
	}
	claudeDir := paths.IdentityDir(name, "claude")
	root := PluginRoot(claudeDir)
	if root == "" {
		return Release{}, errors.New("no tentaqles plugin folder is installed")
	}
	clone := MarketplaceClone(claudeDir)
	if fi, err := os.Stat(clone); err != nil || !fi.IsDir() {
		return Release{}, fmt.Errorf("no tentaqles marketplace clone at %s", clone)
	}
	if err := checkOrigin(clone); err != nil {
		return Release{}, err
	}
	tree, err := releaseTree(clone, commit)
	if err != nil {
		return Release{}, err
	}
	rel := Release{Version: rep.PluginTo, SHA: commit}
	sha256Repo := len(commit) == 64
	have := map[string]bool{}
	for _, f := range pluginFiles(root) {
		have[f] = true
		want, inRelease := tree[f]
		if !inRelease {
			rel.Mismatches = append(rel.Mismatches, f+": not in the release")
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(f))
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() {
			rel.Mismatches = append(rel.Mismatches, f+": not a regular file")
			continue
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return Release{}, fmt.Errorf("reading %s: %w", p, err)
		}
		_, blob, _ := strings.Cut(want, " ")
		if blobHash(content, sha256Repo) != blob {
			rel.Mismatches = append(rel.Mismatches, f+": differs from the release")
		}
	}
	for f := range tree {
		if !have[f] && !skippedInPlugin(f) {
			rel.Mismatches = append(rel.Mismatches, f+": in the release but missing")
		}
	}
	sort.Strings(rel.Mismatches)
	return rel, nil
}
