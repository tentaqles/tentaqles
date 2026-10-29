package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// Evidence names of a db migrate approval (§14).
const (
	EvMigrations = "migrations" // MigrationsHash of the migrations paths
	EvDrill      = "drill"      // drill.Binding of the evidence log
)

// Subject is the subject of every db migrate approval: the migrations of
// the repository, as bound by EvMigrations.
const Subject = "migrations"

// Limits of MigrationsHash. Going over one is an error: the guard denies.
const (
	MaxMigrationFiles = 512
	MaxMigrationDepth = 3
	MaxMigrationBytes = 1 << 20
	// HashBudget is the guard's own time limit for hashing, inside the 50 ms
	// guard bound.
	HashBudget = 30 * time.Millisecond
)

// Entry is a directory entry.
type Entry struct {
	Name string
	Dir  bool
}

// Reader lists and reads repo-relative paths. The guard's reader is
// GuardCtx.ListLocal/ReadLocal; the approve command's reads the disk. Both
// leave out names starting with a dot and links, so they hash the same set.
type Reader interface {
	List(dir string) ([]Entry, error)
	Read(p string) ([]byte, error)
}

// ErrHashBudget is a hash that ran over HashBudget.
var ErrHashBudget = errors.New("the migrations could not be hashed within the guard's time budget")

// MigrationsHash is the content hash the approval binds: every file under
// the migrations paths (repo-relative, slash), to MaxMigrationDepth levels,
// as "path\x00sha256(content)\n" sorted by path. A missing path hashes as
// missing. deadline zero means no time limit.
func MigrationsHash(r Reader, roots []string, deadline time.Time) (string, error) {
	var lines []string
	n := 0
	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		if !deadline.IsZero() && time.Now().After(deadline) {
			return ErrHashBudget
		}
		es, err := r.List(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && depth == 0 {
				lines = append(lines, dir+"\x00missing\n")
				return nil
			}
			return fmt.Errorf("%s: %w", dir, err)
		}
		for _, e := range es {
			if strings.HasPrefix(e.Name, ".") {
				continue
			}
			p := path.Join(dir, e.Name)
			if e.Dir {
				if depth+1 >= MaxMigrationDepth {
					return fmt.Errorf("%s is nested deeper than %d levels", p, MaxMigrationDepth)
				}
				if err := walk(p, depth+1); err != nil {
					return err
				}
				continue
			}
			if n++; n > MaxMigrationFiles {
				return fmt.Errorf("more than %d migration files", MaxMigrationFiles)
			}
			if !deadline.IsZero() && time.Now().After(deadline) {
				return ErrHashBudget
			}
			b, err := r.Read(p)
			if err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			s := sha256.Sum256(b)
			lines = append(lines, p+"\x00"+hex.EncodeToString(s[:])+"\n")
		}
		return nil
	}
	seen := map[string]bool{}
	var clean []string
	for _, r := range roots {
		r = path.Clean(strings.TrimPrefix(strings.ReplaceAll(r, `\`, "/"), "./"))
		if r == "" || r == "." || strings.HasPrefix(r, "/") || strings.HasPrefix(r, "..") || seen[r] {
			if r == "." || strings.HasPrefix(r, "/") || strings.HasPrefix(r, "..") {
				return "", fmt.Errorf("migrations path %q must be a folder inside the repository", r)
			}
			continue
		}
		seen[r] = true
		clean = append(clean, r)
	}
	if len(clean) == 0 {
		return "", errors.New("packs.db.migrations names no path")
	}
	sort.Strings(clean)
	for _, r := range clean {
		if err := walk(r, 0); err != nil {
			return "", err
		}
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// GuardReader reads through the guard's bounded local reads.
type GuardReader struct{ G packs.GuardCtx }

func (g GuardReader) List(dir string) ([]Entry, error) {
	es, err := g.G.ListLocal(dir, packs.MaxListEntries)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(es))
	for _, e := range es {
		out = append(out, Entry{Name: e.Name, Dir: e.Dir})
	}
	return out, nil
}

func (g GuardReader) Read(p string) ([]byte, error) { return g.G.ReadLocal(p, MaxMigrationBytes) }

// DiskReader reads under Root (the approve command). It applies the same
// limits as the guard: at most packs.MaxListEntries entries per folder and
// MaxMigrationBytes per file, no links.
type DiskReader struct{ Root string }

func (d DiskReader) abs(p string) string { return filepath.Join(d.Root, filepath.FromSlash(p)) }

func (d DiskReader) List(dir string) ([]Entry, error) {
	st, err := os.Lstat(d.abs(dir))
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", dir)
	}
	es, err := os.ReadDir(d.abs(dir))
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range es {
		if e.Type()&os.ModeSymlink != 0 || strings.HasPrefix(strings.ToLower(e.Name()), ".env") {
			continue
		}
		out = append(out, Entry{Name: e.Name(), Dir: e.IsDir()})
	}
	if len(out) > packs.MaxListEntries {
		return nil, fmt.Errorf("%s has more than %d entries", dir, packs.MaxListEntries)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (d DiskReader) Read(p string) ([]byte, error) {
	st, err := os.Lstat(d.abs(p))
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", p)
	}
	if st.Size() > MaxMigrationBytes {
		return nil, fmt.Errorf("%s is over %d bytes", p, MaxMigrationBytes)
	}
	return os.ReadFile(d.abs(p))
}
