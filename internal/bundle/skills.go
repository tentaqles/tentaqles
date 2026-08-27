package bundle

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const skillMarkerName = ".tq-managed"

// SyncSkills copies each desired skill's source directory into
// <dir>/skills/<name>, marking it as tq-managed, and prunes any
// previously-owned skill directories that are no longer desired.
//
// A skills/<name> directory that already exists without a .tq-managed
// marker is left untouched and returns an error, since it wasn't created by
// tq and may hold hand-authored content.
func SyncSkills(dir string, d Desired, st *State) ([]string, error) {
	skillsRoot := filepath.Join(dir, "skills")

	changed := make([]string, 0, len(d.Skills))
	for name, src := range d.Skills {
		dst := filepath.Join(skillsRoot, name)

		if info, err := os.Stat(dst); err == nil && info.IsDir() {
			if _, err := os.Stat(filepath.Join(dst, skillMarkerName)); err != nil {
				return nil, fmt.Errorf("unmanaged skill exists: %s", dst)
			}
			if err := os.RemoveAll(dst); err != nil {
				return nil, err
			}
		}

		if err := copyDir(src, dst); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dst, skillMarkerName), []byte(src), 0o644); err != nil {
			return nil, err
		}

		changed = append(changed, name)
	}
	sort.Strings(changed)

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
	}

	st.Skills = changed

	return changed, nil
}

// copyDir recursively copies src into dst, creating dst if needed.
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
		if e.IsDir() {
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
