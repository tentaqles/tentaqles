package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const skillMarkerName = ".tq-managed"

// SyncSkills copies each desired skill's source directory into
// <dir>/skills/<name>, marking it as tq-managed, and prunes any
// previously-owned skill directories that are no longer desired.
//
// A skills/<name> directory that already exists without a .tq-managed
// marker is left untouched and returns an error, since it wasn't created by
// tq and may hold hand-authored content.
//
// A skill whose destination marker already records the source tree's
// current content fingerprint is left untouched (no write, not reported in
// changed). changed contains desired names actually (re)written, plus
// "-"+name for pruned skills, sorted.
func SyncSkills(dir string, d Desired, st *State) ([]string, error) {
	skillsRoot := filepath.Join(dir, "skills")

	var changed []string
	names := make([]string, 0, len(d.Skills))
	for name := range d.Skills {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if !validName(name) {
			return nil, fmt.Errorf("invalid skill name: %q", name)
		}
	}
	for _, name := range st.Skills {
		if !validName(name) {
			return nil, fmt.Errorf("invalid skill name: %q", name)
		}
	}

	for _, name := range names {
		src := d.Skills[name]
		dst := filepath.Join(skillsRoot, name)

		sum, err := fingerprintDir(src)
		if err != nil {
			return nil, err
		}

		info, statErr := os.Stat(dst)
		if statErr == nil && info.IsDir() {
			markerPath := filepath.Join(dst, skillMarkerName)
			markerRaw, err := os.ReadFile(markerPath)
			if err != nil {
				return nil, fmt.Errorf("unmanaged skill exists: %s", dst)
			}
			_, existingSha, ok := parseSkillMarker(markerRaw)
			if ok && existingSha == sum {
				continue // up to date, no write
			}
			if err := os.RemoveAll(dst); err != nil {
				return nil, err
			}
		}

		if err := copySkillAtomic(src, dst, sum); err != nil {
			return nil, err
		}

		changed = append(changed, name)
	}

	for _, name := range st.Skills {
		if _, ok := d.Skills[name]; ok {
			continue
		}
		dst := filepath.Join(skillsRoot, name)
		if _, err := os.Stat(filepath.Join(dst, skillMarkerName)); err != nil {
			continue // not managed (or already gone); leave it alone
		}
		if err := os.RemoveAll(dst); err != nil {
			return nil, err
		}
		changed = append(changed, "-"+name)
	}
	sort.Strings(changed)

	st.Skills = names

	return changed, nil
}

// copySkillAtomic copies src into a temp sibling of dst, writes the marker
// file recording src and its content fingerprint, then removes any
// leftover dst and renames the temp dir into place. This ensures a crash
// mid-copy never leaves a marker-less partial directory at dst.
func copySkillAtomic(src, dst, sum string) error {
	tmp := fmt.Sprintf("%s.tmp-%d", dst, os.Getpid())
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := copyDir(src, tmp); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	marker := fmt.Sprintf("source=%s\nsha=%s\n", src, sum)
	if err := os.WriteFile(filepath.Join(tmp, skillMarkerName), []byte(marker), 0o644); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.RemoveAll(dst); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return nil
}

// parseSkillMarker parses a marker file of the form
// "source=<path>\nsha=<hex>\n" and reports whether both fields were found.
func parseSkillMarker(raw []byte) (source, sha string, ok bool) {
	for _, line := range strings.Split(string(raw), "\n") {
		if v, found := strings.CutPrefix(line, "source="); found {
			source = v
		}
		if v, found := strings.CutPrefix(line, "sha="); found {
			sha = v
		}
	}
	return source, sha, source != "" && sha != ""
}

// fingerprintDir returns a sha256 hex digest computed over every file under
// root (sorted by relative path), hashing the path then the file's content.
// A file named .tq-managed at the top level is skipped so a skill's own
// marker never contributes to its source fingerprint.
func fingerprintDir(root string) (string, error) {
	var relPaths []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == skillMarkerName {
			return nil
		}
		relPaths = append(relPaths, rel)
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(relPaths)

	h := sha256.New()
	for _, rel := range relPaths {
		h.Write([]byte(rel))
		h.Write([]byte{0})
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return "", err
		}
		f.Close()
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyDir recursively copies src into dst, creating dst if needed. Symlinks
// (including Windows junctions/reparse points, which os.Lstat also reports
// as symlink-like) are resolved via os.Stat and their target's contents
// copied in place — copyDir never creates a link at dst.
func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())

		lst, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}

		isDir := lst.IsDir()
		if lst.Mode()&os.ModeSymlink != 0 {
			resolved, err := os.Stat(srcPath)
			if err != nil {
				return err
			}
			isDir = resolved.IsDir()
		}

		if isDir {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
