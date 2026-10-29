// Package baseline is the external-tool baseline ratchet (spec C §2.6a). A
// baseline file lists known findings by fingerprint; a finding of an external
// check that matches one is downgraded to warn, a new one keeps its level.
// Entries never hold a value from the scanned file: the fingerprint is a
// sha256 of the rule, the normalised location and the message with digits
// stripped.
package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// Schema is the baseline file's schema id.
const Schema = "tq.packs.baseline/v1"

// Entry is one known finding.
type Entry struct {
	Check       string `json:"check"`
	Tool        string `json:"tool,omitempty"`
	Rule        string `json:"rule"`
	Location    string `json:"location"`
	Fingerprint string `json:"fingerprint"`
}

// File is the on-disk baseline.
type File struct {
	Schema  string  `json:"schema"`
	Entries []Entry `json:"entries"`
}

// NormLocation is the location used in a fingerprint: slash form, no ./,
// no line number (a finding that moves lines still matches).
func NormLocation(loc string) string {
	loc = strings.TrimSpace(strings.ReplaceAll(loc, `\`, "/"))
	loc = strings.TrimPrefix(loc, "./")
	if loc != "" && !strings.Contains(loc, "://") {
		loc = path.Clean(loc)
	}
	return loc
}

// Fingerprint is sha256(rule NUL location NUL message-without-digits).
func Fingerprint(rule, location, message string) string {
	msg := strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return -1
		}
		return r
	}, message)
	msg = strings.Join(strings.Fields(msg), " ")
	sum := sha256.Sum256([]byte(rule + "\x00" + NormLocation(location) + "\x00" + msg))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// EntryFor builds the entry of one finding.
func EntryFor(checkID, tool string, f packs.Finding) Entry {
	return Entry{Check: checkID, Tool: tool, Rule: f.Rule, Location: NormLocation(f.File), Fingerprint: Fingerprint(f.Rule, f.File, f.Message)}
}

// Read parses a baseline file.
func Read(raw []byte) (File, error) {
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return File{}, fmt.Errorf("baseline: %w", err)
	}
	if f.Schema != "" && f.Schema != Schema {
		return File{}, fmt.Errorf("baseline: schema %q, want %s", f.Schema, Schema)
	}
	return f, nil
}

// MaxSize caps a baseline file (the growth check reads at most this much).
const MaxSize = 16 << 20

// Load reads a baseline file; a missing file is an empty baseline, a file
// over MaxSize an error (fail closed: nothing is downgraded).
func Load(p string) (File, error) {
	fi, err := os.Stat(p)
	if os.IsNotExist(err) {
		return File{Schema: Schema}, nil
	}
	if err != nil {
		return File{}, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > MaxSize {
		return File{}, fmt.Errorf("baseline %s: not a regular file of at most %d bytes", p, MaxSize)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return File{}, err
	}
	return Read(raw)
}

// Write writes entries sorted and de-duplicated (0644: it is repo content).
func Write(p string, entries []Entry) error {
	seen := map[string]bool{}
	var out []Entry
	for _, e := range entries {
		k := e.Check + "\x00" + e.Fingerprint
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Check != out[j].Check {
			return out[i].Check < out[j].Check
		}
		if out[i].Location != out[j].Location {
			return out[i].Location < out[j].Location
		}
		return out[i].Fingerprint < out[j].Fingerprint
	})
	if out == nil {
		out = []Entry{}
	}
	b, err := json.MarshalIndent(File{Schema: Schema, Entries: out}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o644)
}

// Set is the union of the enabled packs' baselines; it implements
// packs.Baseline.
type Set struct {
	byCheck map[string]map[string]bool // check id -> fingerprint
}

// NewSet indexes entries.
func NewSet(entries ...Entry) *Set {
	s := &Set{byCheck: map[string]map[string]bool{}}
	s.Add(entries...)
	return s
}

// Add adds entries.
func (s *Set) Add(entries ...Entry) {
	for _, e := range entries {
		m := s.byCheck[e.Check]
		if m == nil {
			m = map[string]bool{}
			s.byCheck[e.Check] = m
		}
		m[e.Fingerprint] = true
	}
}

// Match reports whether f of checkID is in the baseline.
func (s *Set) Match(checkID string, f packs.Finding) bool {
	if s == nil {
		return false
	}
	return s.byCheck[checkID][Fingerprint(f.Rule, f.File, f.Message)]
}

// Len is the number of entries.
func (s *Set) Len() int {
	n := 0
	for _, m := range s.byCheck {
		n += len(m)
	}
	return n
}

// LoadForConfig loads the baseline file of every enabled pack that sets one
// (repo relative paths under root). It returns the set and, per pack, the
// repo path it read.
func LoadForConfig(root string, cfg packs.Config) (*Set, map[string]string, error) {
	s := NewSet()
	files := map[string]string{}
	for _, name := range cfg.Enabled() {
		b := cfg.Pack(name).Baseline
		if b == "" {
			continue
		}
		f, err := Load(filepath.Join(root, filepath.FromSlash(b)))
		if err != nil {
			return s, files, fmt.Errorf("packs.%s.baseline %s: %w", name, b, err)
		}
		s.Add(f.Entries...)
		files[name] = b
	}
	return s, files, nil
}

// Count returns the number of entries in raw baseline JSON (0 when it does
// not parse), for the baseline-grew check.
func Count(raw []byte) int {
	f, err := Read(raw)
	if err != nil {
		return 0
	}
	return len(f.Entries)
}
