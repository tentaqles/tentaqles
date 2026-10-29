package drill

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// EvidenceFile is the repo-relative evidence log (§13.5), committed with the
// work.
const EvidenceFile = "docs/ops/restore-drills.jsonl"

// MaxEvidenceBytes bounds a read of the evidence log.
const MaxEvidenceBytes = 1 << 20

// Table is one verified table: its name and row count. Nothing else.
type Table struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// Schema is the tq-chosen proof that the schema loaded.
type Schema struct {
	Tables  int64 `json:"tables"`
	Indexes int64 `json:"indexes"`
}

// Line is one evidence line. It holds names and numbers only: never row
// content, a host, a database name of a real environment or a connection
// string.
type Line struct {
	Name             string            `json:"name"`
	Engine           string            `json:"engine"`
	Source           string            `json:"source"`
	BackupTakenAt    string            `json:"backup_taken_at"`
	BackupTimeFrom   string            `json:"backup_time_from"` // dump-header, file-mtime or dump-time
	RestoredAt       string            `json:"restored_at"`
	DurationS        int64             `json:"duration_s"`
	OK               bool              `json:"ok"`
	FailedStep       string            `json:"failed_step,omitempty"` // a fixed step name, never tool output
	Tables           []Table           `json:"tables"`
	Schema           Schema            `json:"schema"`
	MigrationVersion string            `json:"migration_version"`
	MigrationFiles   string            `json:"migration_files_version,omitempty"`
	MigrationGap     bool              `json:"migration_gap,omitempty"`
	ToolVersions     map[string]string `json:"tool_versions"`
	HostKind         string            `json:"host_kind"`
}

// Marshal is the line's exact bytes (no newline).
func (l Line) Marshal() ([]byte, error) {
	if l.Tables == nil {
		l.Tables = []Table{}
	}
	if l.ToolVersions == nil {
		l.ToolVersions = map[string]string{}
	}
	return json.Marshal(l)
}

// Append writes one line to root/docs/ops/restore-drills.jsonl, creating
// the folder and file, and ends a previous line that lacks its newline.
func Append(root string, l Line) error {
	b, err := l.Marshal()
	if err != nil {
		return err
	}
	p := filepath.Join(root, filepath.FromSlash(EvidenceFile))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, st.Size()-1); err == nil && last[0] != '\n' {
			b = append([]byte{'\n'}, b...)
		}
	}
	b = append(b, '\n')
	if _, err := f.Write(b); err != nil {
		return err
	}
	return f.Sync()
}

// Entry is a parsed line with its raw bytes.
type Entry struct {
	Line
	Raw        []byte
	LineNo     int
	restoredAt time.Time
	backupAt   time.Time
}

// RestoredTime and BackupTime are the parsed timestamps.
func (e Entry) RestoredTime() time.Time { return e.restoredAt }
func (e Entry) BackupTime() time.Time   { return e.backupAt }

// ParseLog reads the evidence log. Lines that do not parse, or carry no
// valid timestamps, are returned as bad line numbers and never count.
func ParseLog(raw []byte) (entries []Entry, bad []int) {
	for i, ln := range bytes.Split(raw, []byte("\n")) {
		ln = bytes.TrimSpace(ln)
		if len(ln) == 0 {
			continue
		}
		var l Line
		if err := json.Unmarshal(ln, &l); err != nil || l.Name == "" {
			bad = append(bad, i+1)
			continue
		}
		r, err1 := time.Parse(time.RFC3339, l.RestoredAt)
		b, err2 := time.Parse(time.RFC3339, l.BackupTakenAt)
		if err1 != nil || err2 != nil {
			bad = append(bad, i+1)
			continue
		}
		entries = append(entries, Entry{Line: l, Raw: append([]byte(nil), ln...), LineNo: i + 1, restoredAt: r, backupAt: b})
	}
	return entries, bad
}

// NewestOK is the newest ok line of a drill, by restored_at.
func NewestOK(entries []Entry, name string) (Entry, bool) {
	var best Entry
	found := false
	for _, e := range entries {
		if e.Name != name || !e.OK || e.HostKind != "local" {
			continue
		}
		if !found || e.restoredAt.After(best.restoredAt) {
			best, found = e, true
		}
	}
	return best, found
}

// Clock skew tolerated for a restored_at in the future.
const futureSkew = 10 * time.Minute

// Freshness is a drill's state against its freshness at now.
type Freshness struct {
	Fresh     bool
	Found     bool
	Age       time.Duration // now - restored_at
	BackupAge time.Duration // restored_at - backup_taken_at
	Why       string
}

// Check judges the newest ok line of d at now (§13.6): it is fresh when it
// was restored within freshness, and the backup it restored was at most
// freshness old when it was restored.
func Check(entries []Entry, d Drill, now time.Time) Freshness {
	e, ok := NewestOK(entries, d.Name)
	if !ok {
		return Freshness{Why: "no successful drill is recorded"}
	}
	f := Freshness{Found: true, Age: now.Sub(e.restoredAt), BackupAge: e.restoredAt.Sub(e.backupAt)}
	switch {
	case e.restoredAt.After(now.Add(futureSkew)):
		f.Why = "the newest successful drill is dated in the future"
	case e.backupAt.After(e.restoredAt.Add(futureSkew)):
		f.Why = "the newest successful drill restored a backup dated after the restore"
	case f.Age > d.Freshness:
		f.Why = fmt.Sprintf("the newest successful drill is %s old (freshness %s)", Days(f.Age), Days(d.Freshness))
	case f.BackupAge > d.Freshness:
		f.Why = fmt.Sprintf("the newest successful drill restored a backup that was %s old (freshness %s)", Days(f.BackupAge), Days(d.Freshness))
	default:
		f.Fresh = true
	}
	return f
}

// Days prints a duration in whole days (at least "0d"), or hours under a day.
func Days(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// Binding values of the drill evidence of a db approval.
const (
	BindingNone    = "none"    // no drill is declared
	BindingSkipped = "skipped" // the human asked with --no-drill
)

// Binding is the drill evidence a db migrate approval binds (§14): a hash
// over the newest ok line of every declared drill, in name order. A drill
// without one binds "missing". It is the same whoever computes it (the
// approve command or the guard), from the evidence log bytes.
func Binding(raw []byte, drills []Drill) string {
	if len(drills) == 0 {
		return BindingNone
	}
	entries, _ := ParseLog(raw)
	names := Names(drills)
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write([]byte{0})
		if e, ok := NewestOK(entries, n); ok {
			s := sha256.Sum256(e.Raw)
			h.Write([]byte(hex.EncodeToString(s[:])))
		} else {
			h.Write([]byte("missing"))
		}
		h.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// ReadLog reads root's evidence log, bounded; a missing file is empty.
func ReadLog(root string) ([]byte, error) {
	p := filepath.Join(root, filepath.FromSlash(EvidenceFile))
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, MaxEvidenceBytes+1)
	n, _ := readFull(f, b)
	if n > MaxEvidenceBytes {
		return nil, fmt.Errorf("%s is over %d bytes", EvidenceFile, MaxEvidenceBytes)
	}
	return b[:n], nil
}

func readFull(f *os.File, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := f.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// StaleText is the fix line of a stale drill.
func StaleText(name string) string {
	return "run `tq pack db drill " + name + "` (restore-drill skill) and commit " + EvidenceFile
}

// joinWhy keeps reasons readable.
func joinWhy(xs []string) string { return strings.Join(xs, "; ") }
