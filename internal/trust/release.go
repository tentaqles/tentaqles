package trust

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// A new version of the tentaqles plugin is signed again in batch (tq hooks
// verify --accept --all) only when its folder is the published release,
// byte for byte: the release is the commit the tentaqles marketplace pins
// (.claude-plugin/marketplace.json of the marketplace clone Claude Code keeps
// at <config dir>/plugins/marketplaces/tentaqles, source git-subdir with a
// sha), and every covered file of the new folder must have the blob that
// commit has at the same path, while every covered file of the release must
// be in the folder. Location is never enough: a file edited in the new
// folder, a file the release does not have (a planted bin/tq), a release file
// missing (an emptied hooks/), no clone, no pin, or git failing, and the
// workspace waits for tq hooks verify --accept <ws>.

// Release is the published release a new plugin folder was compared with.
type Release struct {
	// Version is the new folder's version, SHA the pinned commit.
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

// gitOutput runs git in dir with a deadline, no prompt and no pager. A seam
// for tests; nothing else replaces it.
var gitOutput = func(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat")
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// releasePin reads the pinned source of the tentaqles plugin from the clone's
// marketplace.json: the commit and the folder of the plugin in the repo.
func releasePin(clone string) (sha, dir string, err error) {
	raw, err := os.ReadFile(filepath.Join(clone, ".claude-plugin", "marketplace.json"))
	if err != nil {
		return "", "", fmt.Errorf("no tentaqles marketplace clone at %s: %w", clone, err)
	}
	var doc struct {
		Plugins []struct {
			Name   string          `json:"name"`
			Source json.RawMessage `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", "", fmt.Errorf("%s is not valid JSON", filepath.Join(clone, ".claude-plugin", "marketplace.json"))
	}
	for _, p := range doc.Plugins {
		if p.Name != "tentaqles" {
			continue
		}
		var src struct {
			Source string `json:"source"`
			Path   string `json:"path"`
			SHA    string `json:"sha"`
		}
		if json.Unmarshal(p.Source, &src) != nil || src.Source != "git-subdir" {
			return "", "", errors.New("the marketplace does not pin tentaqles to a git commit")
		}
		if !isHex(src.SHA) || (len(src.SHA) != 40 && len(src.SHA) != 64) {
			return "", "", errors.New("the marketplace pins tentaqles to no commit sha")
		}
		d := path.Clean(strings.Trim(filepath.ToSlash(src.Path), "/"))
		if d == ".." || strings.HasPrefix(d, "../") {
			return "", "", fmt.Errorf("the pinned plugin path %q leaves the repo", src.Path)
		}
		return strings.ToLower(src.SHA), d, nil
	}
	return "", "", errors.New("the marketplace lists no tentaqles plugin")
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// releaseTree lists the blobs of dir at sha in clone, by path relative to
// dir, with git ls-tree.
func releaseTree(clone, sha, dir string) (map[string]string, error) {
	args := []string{"ls-tree", "-r", "-z", "--full-tree", sha}
	prefix := ""
	if dir != "." && dir != "" {
		args = append(args, "--", dir)
		prefix = dir + "/"
	}
	out, err := gitOutput(clone, args...)
	if err != nil {
		return nil, fmt.Errorf("the release %s is not in the marketplace clone: %w", sha[:12], err)
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
		rel, ok := strings.CutPrefix(p, prefix)
		if !ok {
			continue
		}
		tree[rel] = f[0] + " " + f[2]
	}
	if len(tree) == 0 {
		return nil, fmt.Errorf("the release %s has no files under %s", sha[:12], dir)
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
// published release, for a report whose only changes are a new plugin
// version (Report.PluginBump). It returns an error when there is no release
// to compare with (no clone, no pin, git failing) and the differences in
// Release.Mismatches otherwise; the folder is the release only when there
// is no error and no mismatch.
func CheckRelease(name string, rep Report) (Release, error) {
	if !validKey(name) {
		return Release{}, fmt.Errorf("trust: invalid workspace name %q", name)
	}
	if rep.PluginFrom == "" || !rep.PluginBump {
		return Release{}, errors.New("the change is not a new plugin version alone")
	}
	claudeDir := paths.IdentityDir(name, "claude")
	root := PluginRoot(claudeDir)
	if root == "" {
		return Release{}, errors.New("no tentaqles plugin folder is installed")
	}
	clone := MarketplaceClone(claudeDir)
	sha, dir, err := releasePin(clone)
	if err != nil {
		return Release{}, err
	}
	tree, err := releaseTree(clone, sha, dir)
	if err != nil {
		return Release{}, err
	}
	rel := Release{Version: rep.PluginTo, SHA: sha}
	sha256Repo := len(sha) == 64
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
