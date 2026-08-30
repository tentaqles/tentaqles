package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// OpKind names a recorded mutation. Every kind has exactly one inverse, applied
// by Restore.
type OpKind string

const (
	// OpMoveDir moved From to To. Reverse: move To back to From.
	OpMoveDir OpKind = "move-dir"
	// OpMakeLink created a link at Path pointing at Target. Reverse: remove
	// the link.
	OpMakeLink OpKind = "make-link"
	// OpRemoveLink removed the link that stood at Path pointing at Target.
	// Reverse: recreate it.
	OpRemoveLink OpKind = "remove-link"
	// OpWriteFile overwrote (or created) Path. Backup is the journal-relative
	// copy of the previous content, or "" if Path did not exist; Bytes and
	// SHA256 describe that copy. Reverse: restore Backup, or delete Path.
	OpWriteFile OpKind = "write-file"
	// OpDeleteFile deleted Path, whose previous content is at Backup.
	// Reverse: restore Backup.
	OpDeleteFile OpKind = "delete-file"
	// OpGitGlobalSet set global git config Key to New. Old/Present describe
	// the value before. Reverse: set Old back, or unset Key.
	OpGitGlobalSet OpKind = "git-global-set"
	// OpRegSet set a Windows registry value. Old/Type/Present describe the
	// value before. Reverse: set Old back with its original type, or delete
	// the value.
	OpRegSet OpKind = "reg-set"
)

// Entry is one recorded mutation. It is written to journal.json before the
// caller performs the mutation, so a crash mid-migration still leaves a
// reversible record.
//
// Because the record is written first, the journal legitimately describes
// mutations that never happened: a crash, or a step that failed, leaves an
// entry whose mutation was never applied. Every reverse therefore treats "the
// world is already in the pre-operation state" as success, and reserves failure
// for a world it does not recognise. See Restore.
type Entry struct {
	Seq  int               `json:"seq"`
	Step string            `json:"step"`
	Op   OpKind            `json:"op"`
	Args map[string]string `json:"args"`
	At   time.Time         `json:"at"`
}

// Journal is an append-only log of reversible operations living under
// <tq home>/backups/<ts>/.
type Journal struct {
	Dir     string  `json:"-"`
	TS      string  `json:"ts"`
	Entries []Entry `json:"entries"`
}

// RegValue is a Windows registry value: its data and the type that data must be
// written back as. Restoring a REG_EXPAND_SZ value as REG_SZ would freeze
// whatever %VARIABLE% it contains into a literal path, so the type travels with
// the data everywhere.
type RegValue struct {
	Type string // REG_SZ, REG_EXPAND_SZ, REG_DWORD, REG_QWORD, REG_BINARY
	Data string
}

// regTypes are the value types tq can capture and write back byte-for-byte
// through reg.exe. REG_MULTI_SZ is deliberately absent: reg.exe renders and
// accepts its elements separated by a literal "\0" sequence, so a value whose
// data itself contains that sequence cannot be told apart from a two-element
// value and would not round-trip. Refusing to record such a value is better
// than recording one that restores wrong.
var regTypes = map[string]bool{
	"REG_SZ":        true,
	"REG_EXPAND_SZ": true,
	"REG_DWORD":     true,
	"REG_QWORD":     true,
	"REG_BINARY":    true,
}

// SupportedRegType reports whether tq can faithfully record and restore a
// registry value of this type.
func SupportedRegType(t string) bool { return regTypes[t] }

// Runner carries the external commands Restore needs. Production callers pass
// DefaultRunner(); tests pass fakes.
type Runner struct {
	// Git runs the git binary and returns its combined output.
	Git func(args ...string) (string, error)
	// Reg runs reg.exe. action is "query", "set", or "delete"; v is only used
	// by "set". It returns reg.exe's combined output.
	Reg func(action, key, name string, v RegValue) (string, error)
}

// DefaultRunner returns a Runner backed by the real git and reg.exe binaries.
func DefaultRunner() Runner {
	return Runner{Git: runGit, Reg: runReg}
}

// runGit shells out to the real git binary.
func runGit(args ...string) (string, error) { return gitcfg.RunGit(args...) }

// runReg shells out to reg.exe. It errors off Windows so a caller that reaches
// it by mistake fails loudly instead of silently doing nothing.
func runReg(action, key, name string, v RegValue) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("reg %s: not supported on this OS", action)
	}
	var args []string
	switch action {
	case "query":
		args = []string{"query", key, "/v", name}
	case "set":
		if !SupportedRegType(v.Type) {
			return "", fmt.Errorf(`reg set %s\%s: refusing to write value type %q; tq only round-trips %s`,
				key, name, v.Type, strings.Join(sortedRegTypes(), ", "))
		}
		args = []string{"add", key, "/v", name, "/t", v.Type, "/d", v.Data, "/f"}
	case "delete":
		args = []string{"delete", key, "/v", name, "/f"}
	default:
		return "", fmt.Errorf("reg: unknown action %q", action)
	}
	out, err := exec.Command("reg", args...).CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return s, fmt.Errorf(`reg %s %s\%s: %w (%s)`, action, key, name, err, s)
	}
	return s, nil
}

func sortedRegTypes() []string {
	out := make([]string, 0, len(regTypes))
	for t := range regTypes {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

const (
	journalName      = "journal.json"
	journalTmpSuffix = ".tq-tmp"
	restoreStateName = "restore.state"
	filesDir         = "files"
)

// dirFor returns the journal directory for a timestamp.
func dirFor(ts string) string { return filepath.Join(paths.Home(), "backups", ts) }

// Open returns the journal for ts, creating <tq home>/backups/<ts>/files/ and
// an empty journal.json if they do not exist. Opening an existing journal
// loads its entries so further records append.
func Open(ts string) (*Journal, error) {
	if ts == "" {
		return nil, fmt.Errorf("migrate.Open: empty timestamp")
	}
	dir := dirFor(ts)
	// 0700: the journal and its backups hold old git identities, registry
	// values, and copies of shell profiles.
	if err := os.MkdirAll(filepath.Join(dir, filesDir), 0o700); err != nil {
		return nil, fmt.Errorf("migrate.Open: %w", err)
	}
	j := &Journal{Dir: dir, TS: ts}
	if _, err := os.Stat(filepath.Join(dir, journalName)); err == nil {
		return load(dir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("migrate.Open: %w", err)
	}
	if err := j.write(); err != nil {
		return nil, err
	}
	return j, nil
}

// Load reads an existing journal. The special timestamp "latest" selects the
// newest backup directory that contains a journal.json; it refuses to resolve
// at all if a newer directory looks half-written, rather than quietly handing
// back an older, unrelated migration.
func Load(ts string) (*Journal, error) {
	if ts == "latest" {
		found, err := latestTS()
		if err != nil {
			return nil, err
		}
		ts = found
	}
	return load(dirFor(ts))
}

func latestTS() (string, error) {
	root := filepath.Join(paths.Home(), "backups")
	ents, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("migrate.Load: reading %s: %w", root, err)
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		d := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(d, journalName)); err == nil {
			names = append(names, e.Name())
			continue
		}
		// A directory holding only the temp file is a journal that was being
		// rewritten when something went wrong. Skipping it would resolve
		// "latest" to an older migration and offer the user its restore --
		// which would put the machine into a state that never existed. Stop
		// and make them look.
		if _, err := os.Stat(filepath.Join(d, journalName+journalTmpSuffix)); err == nil {
			return "", fmt.Errorf("migrate.Load: %s holds %s but no %s: that journal was interrupted mid-write. "+
				"Inspect it and either rename the temp file into place or remove the directory; "+
				"tq will not silently fall back to an older backup, because restoring the wrong migration is worse than restoring none",
				d, journalName+journalTmpSuffix, journalName)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("migrate.Load: no journal found under %s", root)
	}
	sort.Strings(names)
	return names[len(names)-1], nil
}

func load(dir string) (*Journal, error) {
	p := filepath.Join(dir, journalName)
	raw, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// A crash between fsyncing journal.json.tq-tmp and renaming it into
			// place leaves only the temp file. Its contents are complete and
			// durable, so prefer it to reporting no journal at all.
			if j, terr := loadFile(dir, p+journalTmpSuffix); terr == nil {
				return j, nil
			}
		}
		return nil, fmt.Errorf("migrate.Load: reading %s: %w", p, err)
	}
	return decodeJournal(dir, p, raw)
}

func loadFile(dir, p string) (*Journal, error) {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return decodeJournal(dir, p, raw)
}

func decodeJournal(dir, p string, raw []byte) (*Journal, error) {
	var j Journal
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, fmt.Errorf("migrate.Load: %s is corrupt or truncated: %w", p, err)
	}
	j.Dir = dir
	if j.TS == "" {
		j.TS = filepath.Base(dir)
	}
	return &j, nil
}

// write persists the journal atomically: a sibling temp file is written and
// fsynced, then renamed over journal.json.
//
// os.Rename replaces an existing destination on every OS tq supports -- on
// Windows it is MoveFileEx with MOVEFILE_REPLACE_EXISTING -- so journal.json is
// never absent, not even for an instant. Removing it first "because Windows
// cannot replace" would open a window on every Record in which a crash, or a
// transient sharing violation from a virus scanner holding the file, destroys
// the journal outright.
func (j *Journal) write() error {
	raw, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return fmt.Errorf("migrate: encoding journal: %w", err)
	}
	raw = append(raw, '\n')
	dst := filepath.Join(j.Dir, journalName)
	tmp := dst + journalTmpSuffix
	// 0600: the journal records the user's previous git identities and
	// registry values.
	if err := writeFileSync(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := renameRetry(tmp, dst); err != nil {
		return fmt.Errorf("migrate: renaming %s to %s: %w", tmp, dst, err)
	}
	// The rename itself must reach the disk too: on ext4 with data=ordered the
	// file can be fsynced and the directory entry naming it still lost.
	if err := syncDir(j.Dir); err != nil {
		return fmt.Errorf("migrate: syncing %s: %w", j.Dir, err)
	}
	return nil
}

// writeFileSync writes data to path and fsyncs it before returning, so the
// bytes are on disk and not merely in the page cache.
func writeFileSync(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("migrate: writing %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("migrate: writing %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("migrate: syncing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("migrate: closing %s: %w", path, err)
	}
	return nil
}

// renameRetry renames tmp over dst, retrying briefly. On Windows a scanner or
// an indexer holding the destination open makes the rename fail with a sharing
// violation that clears on its own within milliseconds.
func renameRetry(tmp, dst string) error {
	var err error
	for attempt := 0; attempt < 10; attempt++ {
		if err = os.Rename(tmp, dst); err == nil {
			return nil
		}
		time.Sleep(time.Duration(2*(attempt+1)) * time.Millisecond)
	}
	return err
}

// Record appends an entry and persists the journal before returning. Callers
// must Record first and perform the mutation afterwards.
//
// Args are validated against the operation before anything is written: a
// misspelled key ("to" for "To") would otherwise produce a journal that looks
// fine and only fails months later, during a restore, when it is the only copy
// of the previous state. Prefer the typed RecordX helpers, which cannot
// misspell a key at all.
func (j *Journal) Record(step string, op OpKind, args map[string]string) error {
	if err := validate(op, args); err != nil {
		return err
	}
	cp := make(map[string]string, len(args))
	for k, v := range args {
		cp[k] = v
	}
	seq := 1
	for _, e := range j.Entries {
		if e.Seq >= seq {
			seq = e.Seq + 1
		}
	}
	j.Entries = append(j.Entries, Entry{
		Seq:  seq,
		Step: step,
		Op:   op,
		Args: cp,
		At:   time.Now().UTC(),
	})
	if err := j.write(); err != nil {
		j.Entries = j.Entries[:len(j.Entries)-1]
		return err
	}
	return nil
}

// RecordMoveDir records that from is about to be moved to to.
func (j *Journal) RecordMoveDir(step, from, to string) error {
	return j.Record(step, OpMoveDir, map[string]string{"From": from, "To": to})
}

// RecordMakeLink records that a link is about to be created at path pointing
// at target.
func (j *Journal) RecordMakeLink(step, path, target string) error {
	return j.Record(step, OpMakeLink, map[string]string{"Path": path, "Target": target})
}

// RecordRemoveLink records that the link at path, pointing at target, is about
// to be removed. target must be known: without it the reverse cannot recreate
// the link and will refuse rather than guess.
func (j *Journal) RecordRemoveLink(step, path, target string) error {
	return j.Record(step, OpRemoveLink, map[string]string{"Path": path, "Target": target})
}

// RecordWriteFile records that path is about to be overwritten or created. b is
// the result of BackupFile(path); its zero value means the file did not exist,
// and the reverse then deletes what tq created.
func (j *Journal) RecordWriteFile(step, path string, b Backup) error {
	return j.Record(step, OpWriteFile, b.args(path))
}

// RecordDeleteFile records that path is about to be deleted. b must be a real
// backup: there is nothing else to restore from.
func (j *Journal) RecordDeleteFile(step, path string, b Backup) error {
	return j.Record(step, OpDeleteFile, b.args(path))
}

// RecordGitGlobalSet records that global git config key is about to be set to
// newVal. old and present describe the value before; pass present=false when
// the key was not set at all.
func (j *Journal) RecordGitGlobalSet(step, key, old, newVal string, present bool) error {
	args := map[string]string{"Key": key, "Present": strconv.FormatBool(present), "New": newVal}
	if present {
		args["Old"] = old
	}
	return j.Record(step, OpGitGlobalSet, args)
}

// RecordRegSet records that registry value key\name is about to be set. old
// and present describe the value before; pass present=false when the value did
// not exist. The old value's type is recorded with it so the restore writes it
// back as REG_EXPAND_SZ rather than freezing %USERPROFILE% into a literal path.
func (j *Journal) RecordRegSet(step, key, name string, old RegValue, present bool) error {
	args := map[string]string{"Key": key, "Name": name, "Present": strconv.FormatBool(present)}
	if present {
		args["Old"] = old.Data
		args["Type"] = old.Type
	}
	return j.Record(step, OpRegSet, args)
}

// argSpec lists the Args an operation accepts.
type argSpec struct {
	required []string
	optional []string
}

var opArgs = map[OpKind]argSpec{
	OpMoveDir:      {required: []string{"From", "To"}},
	OpMakeLink:     {required: []string{"Path", "Target"}},
	OpRemoveLink:   {required: []string{"Path", "Target"}},
	OpWriteFile:    {required: []string{"Path"}, optional: []string{"Backup", "Bytes", "SHA256"}},
	OpDeleteFile:   {required: []string{"Path", "Backup", "Bytes", "SHA256"}},
	OpGitGlobalSet: {required: []string{"Key", "Present"}, optional: []string{"Old", "New"}},
	OpRegSet:       {required: []string{"Key", "Name", "Present"}, optional: []string{"Old", "Type"}},
}

// validate rejects an entry the restore could not act on, at record time.
func validate(op OpKind, args map[string]string) error {
	spec, ok := opArgs[op]
	if !ok {
		return fmt.Errorf("migrate.Record: unknown op %q", op)
	}
	allowed := make(map[string]bool, len(spec.required)+len(spec.optional))
	for _, k := range spec.required {
		allowed[k] = true
	}
	for _, k := range spec.optional {
		allowed[k] = true
	}
	var known []string
	known = append(known, spec.required...)
	known = append(known, spec.optional...)
	sort.Strings(known)
	for k := range args {
		if !allowed[k] {
			return fmt.Errorf("migrate.Record: %s: unknown argument %q (this op takes %s)", op, k, strings.Join(known, ", "))
		}
	}
	for _, k := range spec.required {
		if strings.TrimSpace(args[k]) == "" {
			return fmt.Errorf("migrate.Record: %s: missing required argument %q", op, k)
		}
	}
	switch op {
	case OpWriteFile, OpDeleteFile:
		if args["Backup"] != "" {
			for _, k := range []string{"Bytes", "SHA256"} {
				if args[k] == "" {
					return fmt.Errorf("migrate.Record: %s: %q is required alongside Backup; restore verifies a backup before writing it over the user's file", op, k)
				}
			}
			if _, err := strconv.ParseInt(args["Bytes"], 10, 64); err != nil {
				return fmt.Errorf("migrate.Record: %s: Bytes %q is not a number", op, args["Bytes"])
			}
			if _, err := hex.DecodeString(args["SHA256"]); err != nil || len(args["SHA256"]) != 64 {
				return fmt.Errorf("migrate.Record: %s: SHA256 %q is not a sha256 hex digest", op, args["SHA256"])
			}
		} else if args["Bytes"] != "" || args["SHA256"] != "" {
			return fmt.Errorf("migrate.Record: %s: Bytes/SHA256 given without a Backup", op)
		}
	case OpGitGlobalSet, OpRegSet:
		switch args["Present"] {
		case "true":
			if op == OpRegSet {
				if args["Type"] == "" {
					return fmt.Errorf("migrate.Record: %s: Type is required when Present is true; restoring a value without its original type can change REG_EXPAND_SZ into REG_SZ", op)
				}
				if !SupportedRegType(args["Type"]) {
					return fmt.Errorf("migrate.Record: %s: refusing to record value type %q, which tq cannot restore faithfully (it handles %s)", op, args["Type"], strings.Join(sortedRegTypes(), ", "))
				}
			}
		case "false":
			if args["Old"] != "" {
				return fmt.Errorf("migrate.Record: %s: Old is set but Present is false", op)
			}
			if op == OpRegSet && args["Type"] != "" {
				return fmt.Errorf("migrate.Record: %s: Type is set but Present is false", op)
			}
		default:
			return fmt.Errorf("migrate.Record: %s: Present must be \"true\" or \"false\", got %q", op, args["Present"])
		}
	}
	return nil
}

// Backup describes a file copied into the journal's files/ directory. The size
// and digest are recorded with the entry and checked before the copy is ever
// written back over the user's file.
type Backup struct {
	// Rel is the journal-relative slot ("files/3"), or "" when the file did
	// not exist.
	Rel    string
	Bytes  int64
	SHA256 string
}

func (b Backup) args(path string) map[string]string {
	m := map[string]string{"Path": path}
	if b.Rel != "" {
		m["Backup"] = b.Rel
		m["Bytes"] = strconv.FormatInt(b.Bytes, 10)
		m["SHA256"] = b.SHA256
	}
	return m
}

// BackupFile copies path into the journal's files/ directory and returns the
// slot plus the size and digest of what was written. A path that does not exist
// backs up to the zero Backup -- the caller records that as "there was nothing
// here before".
//
// The copy is fsynced (and on POSIX so is files/) before returning, because the
// caller's next step is to Record an entry pointing at this slot and then
// destroy the original. If the entry became durable first, a power loss would
// leave a journal confidently naming a zero-length backup, and a later restore
// would write that emptiness over the user's real file and report success.
func (j *Journal) BackupFile(path string) (Backup, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Backup{}, nil
		}
		return Backup{}, fmt.Errorf("migrate.BackupFile: reading %s: %w", path, err)
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	dir := filepath.Join(j.Dir, filesDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Backup{}, fmt.Errorf("migrate.BackupFile: %w", err)
	}

	var f *os.File
	var rel string
	for attempt := 0; ; attempt++ {
		n, err := j.nextSlot(dir)
		if err != nil {
			return Backup{}, err
		}
		rel = filesDir + "/" + strconv.Itoa(n)
		// O_EXCL so two backups can never land in the same slot even if the
		// slot arithmetic is wrong.
		f, err = os.OpenFile(filepath.Join(dir, strconv.Itoa(n)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) || attempt >= 64 {
			return Backup{}, fmt.Errorf("migrate.BackupFile: writing %s: %w", rel, err)
		}
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return Backup{}, fmt.Errorf("migrate.BackupFile: writing %s: %w", rel, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return Backup{}, fmt.Errorf("migrate.BackupFile: syncing %s: %w", rel, err)
	}
	if err := f.Close(); err != nil {
		return Backup{}, fmt.Errorf("migrate.BackupFile: closing %s: %w", rel, err)
	}
	if err := syncDir(dir); err != nil {
		return Backup{}, fmt.Errorf("migrate.BackupFile: syncing %s: %w", dir, err)
	}
	sum := sha256.Sum256(raw)
	return Backup{Rel: rel, Bytes: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])}, nil
}

// nextSlot returns a numeric slot under files/ that has never been used.
//
// It is one past the highest slot that exists on disk *or* that any recorded
// entry still points at, never the count of entries: deleting files/1 would
// otherwise make the next backup land on files/3 and overwrite a copy an
// earlier entry is the only record of.
func (j *Journal) nextSlot(dir string) (int, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("migrate.BackupFile: reading %s: %w", dir, err)
	}
	high := 0
	for _, e := range ents {
		if n, err := strconv.Atoi(e.Name()); err == nil && n > high {
			high = n
		}
	}
	for _, ent := range j.Entries {
		b := ent.Args["Backup"]
		if !strings.HasPrefix(b, filesDir+"/") {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimPrefix(b, filesDir+"/")); err == nil && n > high {
			high = n
		}
	}
	return high + 1, nil
}

// restoreState records how far a restore got, so an interrupted one resumes
// instead of starting over at an entry it already reversed.
type restoreState struct {
	// LowestSeqDone is the lowest Seq that has been reversed (or found already
	// in its pre-operation state). Entries at or above it are done.
	LowestSeqDone int       `json:"lowest_seq_done"`
	At            time.Time `json:"at"`
}

func (j *Journal) loadRestoreState() int {
	raw, err := os.ReadFile(filepath.Join(j.Dir, restoreStateName))
	if err != nil {
		return math.MaxInt
	}
	var s restoreState
	if err := json.Unmarshal(raw, &s); err != nil || s.LowestSeqDone <= 0 {
		return math.MaxInt
	}
	return s.LowestSeqDone
}

func (j *Journal) saveRestoreState(seq int) error {
	raw, err := json.Marshal(restoreState{LowestSeqDone: seq, At: time.Now().UTC()})
	if err != nil {
		return err
	}
	return writeFileSync(filepath.Join(j.Dir, restoreStateName), append(raw, '\n'), 0o600)
}

// ResetRestore forgets how far a previous Restore got, so the next one starts
// again at the newest entry.
//
// Use it only when the recorded progress is wrong (the journal directory was
// copied from another machine, say), not to "restore again". Entries interact:
// reversing a move-dir puts a real directory back where a newer make-link entry
// expects to find the link it created, and that newer entry will then rightly
// refuse to touch it. Replaying a journal that already restored cleanly stops
// on the first such pair.
func (j *Journal) ResetRestore() error {
	err := os.Remove(filepath.Join(j.Dir, restoreStateName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("migrate: clearing %s: %w", restoreStateName, err)
	}
	return nil
}

// Restore replays the journal in descending Seq order, applying each entry's
// inverse.
//
// Each inverse has three outcomes. It reverses the mutation; or it finds the
// world already in the entry's pre-operation state -- which is normal, since
// entries are recorded before their mutations, so a crash or a failed step
// leaves entries that never happened -- and says so and continues; or it finds
// a state it does not recognise, and stops. The first stop ends the replay and
// returns the lines completed so far plus an error naming the sequence number,
// operation, and path.
//
// Progress is written to restore.state, so a restore that stopped halfway
// resumes below the entry it reached instead of starting again at the newest
// one (which it has already reversed, and would now refuse to reverse twice).
// The journal file itself is never modified, so a fixed-up state can simply be
// restored again; what was done is appended to restore.log in the journal
// directory.
func (j *Journal) Restore(r Runner) ([]string, error) {
	ents := append([]Entry(nil), j.Entries...)
	sort.SliceStable(ents, func(a, b int) bool { return ents[a].Seq > ents[b].Seq })

	lowestDone := j.loadRestoreState()
	var lines []string
	var failure error
	for _, e := range ents {
		if e.Seq >= lowestDone {
			lines = append(lines, fmt.Sprintf("seq %d (%s): already reversed by an earlier restore", e.Seq, e.Op))
			continue
		}
		line, err := j.reverse(e, r)
		if err != nil {
			failure = err
			break
		}
		lines = append(lines, line)
		lowestDone = e.Seq
		if err := j.saveRestoreState(lowestDone); err != nil {
			failure = fmt.Errorf("restore: seq %d reversed, but recording progress failed: %w", e.Seq, err)
			break
		}
	}
	j.appendRestoreLog(lines, failure)
	return lines, failure
}

func (j *Journal) appendRestoreLog(lines []string, failure error) {
	var b strings.Builder
	fmt.Fprintf(&b, "=== restore %s ===\n", time.Now().UTC().Format(time.RFC3339))
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	if failure != nil {
		fmt.Fprintf(&b, "STOPPED: %v\n", failure)
	} else {
		b.WriteString("completed\n")
	}
	f, err := os.OpenFile(filepath.Join(j.Dir, "restore.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(b.String())
}

// opErr formats a failure so the message names the entry and the path involved.
func opErr(e Entry, path string, format string, a ...any) error {
	return fmt.Errorf("restore: seq %d (%s) %s: %s", e.Seq, e.Op, path, fmt.Sprintf(format, a...))
}

// skipped formats the "already in the pre-operation state" outcome.
func skipped(e Entry, format string, a ...any) string {
	return fmt.Sprintf("seq %d (%s): nothing to undo: %s", e.Seq, e.Op, fmt.Sprintf(format, a...))
}

// kindOf names what is at a path, for error messages.
func kindOf(fi os.FileInfo) string {
	if fi.IsDir() {
		return "directory"
	}
	return "file"
}

func (j *Journal) reverse(e Entry, r Runner) (string, error) {
	switch e.Op {
	case OpMoveDir:
		return j.reverseMoveDir(e)
	case OpMakeLink:
		return j.reverseMakeLink(e)
	case OpRemoveLink:
		return j.reverseRemoveLink(e)
	case OpWriteFile, OpDeleteFile:
		return j.reverseFile(e)
	case OpGitGlobalSet:
		return j.reverseGit(e, r)
	case OpRegSet:
		return j.reverseReg(e, r)
	default:
		return "", fmt.Errorf("restore: seq %d: unknown op %q", e.Seq, e.Op)
	}
}

func (j *Journal) reverseMoveDir(e Entry) (string, error) {
	from, to := e.Args["From"], e.Args["To"]
	toFI, toErr := os.Lstat(to)
	if toErr != nil && !errors.Is(toErr, os.ErrNotExist) {
		return "", opErr(e, to, "cannot inspect it: %v", toErr)
	}
	fromFI, fromErr := os.Lstat(from)
	if fromErr != nil && !errors.Is(fromErr, os.ErrNotExist) {
		return "", opErr(e, from, "cannot inspect it: %v", fromErr)
	}
	if toErr != nil { // destination missing: the move never happened, or was already undone
		if fromErr != nil {
			return "", opErr(e, to, "neither this nor the original %s exists; the directory is gone and tq cannot recover it", from)
		}
		if !fromFI.IsDir() {
			return "", opErr(e, from, "the original location holds a %s, not a directory; refusing to guess what happened", kindOf(fromFI))
		}
		if ok, _ := IsLink(from); ok {
			return "", opErr(e, from, "the original location is a link, not the directory tq moved; refusing to guess what happened")
		}
		return skipped(e, "%s was never moved to %s", from, to), nil
	}
	if fromErr == nil {
		return "", opErr(e, from, "the original location already exists; refusing to overwrite it")
	}
	_ = toFI
	if err := MoveDir(to, from); err != nil {
		return "", opErr(e, to, "%v", err)
	}
	return fmt.Sprintf("moved %s back to %s", to, from), nil
}

func (j *Journal) reverseMakeLink(e Entry) (string, error) {
	path, target := e.Args["Path"], e.Args["Target"]
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return skipped(e, "no link at %s; it was never created", path), nil
		}
		return "", opErr(e, path, "cannot inspect it: %v", err)
	}
	isLink, tgt := IsLink(path)
	if !isLink {
		return "", opErr(e, path, "expected the link tq created, but this is a real %s; refusing to remove it", kindOf(fi))
	}
	if target != "" && tgt != "" && !samePath(tgt, target) {
		return "", opErr(e, path, "is a link to %s, not to the %s tq linked it to; refusing to remove someone else's link", tgt, target)
	}
	if err := RemoveLink(path); err != nil {
		return "", opErr(e, path, "%v", err)
	}
	return fmt.Sprintf("removed link %s", path), nil
}

func (j *Journal) reverseRemoveLink(e Entry) (string, error) {
	path, target := e.Args["Path"], e.Args["Target"]
	if target == "" {
		// IsLink reports (true, "") for a link whose target it could not read,
		// so a journal can carry an empty Target. Recreating the link anyway
		// would point it at whatever happens to sit at that name now.
		return "", opErr(e, path, "the journal does not record where this link pointed; refusing to recreate it pointing nowhere")
	}
	fi, err := os.Lstat(path)
	if err == nil {
		if isLink, tgt := IsLink(path); isLink && tgt != "" && samePath(tgt, target) {
			return skipped(e, "link %s -> %s is already in place", path, target), nil
		}
		return "", opErr(e, path, "a %s already exists here; refusing to replace it with a link", kindOf(fi))
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", opErr(e, path, "cannot inspect it: %v", err)
	}
	if err := MakeLink(path, target); err != nil {
		return "", opErr(e, path, "%v", err)
	}
	return fmt.Sprintf("recreated link %s -> %s", path, target), nil
}

func (j *Journal) reverseFile(e Entry) (string, error) {
	path, backup := e.Args["Path"], e.Args["Backup"]
	if backup == "" {
		if _, err := os.Lstat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return skipped(e, "%s is already absent", path), nil
			}
			return "", opErr(e, path, "%v", err)
		}
		if err := os.Remove(path); err != nil {
			return "", opErr(e, path, "removing the file tq created: %v", err)
		}
		return fmt.Sprintf("deleted %s (tq created it)", path), nil
	}
	src := filepath.Join(j.Dir, filepath.FromSlash(backup))
	raw, err := os.ReadFile(src)
	if err != nil {
		return "", opErr(e, path, "backup %s is missing: %v", backup, err)
	}
	if err := verifyBackup(e, backup, raw); err != nil {
		return "", err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(src); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", opErr(e, path, "%v", err)
	}
	if err := writeFileSync(path, raw, mode); err != nil {
		return "", opErr(e, path, "%v", err)
	}
	return fmt.Sprintf("restored %s from %s", path, backup), nil
}

// verifyBackup checks a backup against the size and digest recorded with the
// entry. The backup is about to be written over a real file the user still
// depends on, and a truncated or empty one -- the shape a crash between the
// copy and its fsync leaves -- would otherwise be restored silently and
// reported as a success.
func verifyBackup(e Entry, backup string, raw []byte) error {
	want := e.Args["SHA256"]
	if want == "" {
		return opErr(e, backup, "the journal records no checksum for this backup; refusing to write an unverified file over your data")
	}
	if n := e.Args["Bytes"]; n != "" {
		if size, err := strconv.ParseInt(n, 10, 64); err == nil && size != int64(len(raw)) {
			return opErr(e, backup, "backup is %d bytes but the journal recorded %d; it is truncated or corrupt, so tq will not write it over your file", len(raw), size)
		}
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != want {
		return opErr(e, backup, "backup checksum is %s but the journal recorded %s; it is corrupt, so tq will not write it over your file", got, want)
	}
	return nil
}

// gitExitNoSuchKey is git's exit code for `config --unset` of a key that is not
// set, and for `config --get` of one.
const gitExitNoSuchKey = 5

func isGitExit(err error, code int) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == code
}

// GitGlobalGet reads a global git config value, reporting whether it is set at
// all. Callers use it to capture the pre-migration value for RecordGitGlobalSet.
func GitGlobalGet(r Runner, key string) (string, bool, error) {
	if r.Git == nil {
		return "", false, fmt.Errorf("migrate: no git runner configured")
	}
	out, err := r.Git("config", "--global", "--get", key)
	if err != nil {
		// git exits non-zero simply because the key is not set.
		return "", false, nil
	}
	return strings.TrimSpace(out), true, nil
}

func (j *Journal) reverseGit(e Entry, r Runner) (string, error) {
	key := e.Args["Key"]
	if r.Git == nil {
		return "", opErr(e, key, "no git runner configured")
	}
	cur, present, _ := GitGlobalGet(r, key)
	want := e.Args["Old"]

	if e.Args["Present"] == "true" {
		if present && cur == want {
			return skipped(e, "git config --global %s is already %q", key, want), nil
		}
		if _, err := r.Git("config", "--global", key, want); err != nil {
			return "", opErr(e, key, "%v", err)
		}
		return fmt.Sprintf("git config --global %s restored to %q", key, want), nil
	}

	// The key was not set before the migration, so the inverse is to unset it.
	if !present {
		return skipped(e, "git config --global %s is already unset", key), nil
	}
	if _, err := r.Git("config", "--global", "--unset", key); err != nil {
		// Exit 5 means "no such key": the state we wanted anyway.
		if !isGitExit(err, gitExitNoSuchKey) {
			return "", opErr(e, key, "%v", err)
		}
	}
	return fmt.Sprintf("git config --global %s unset", key), nil
}

// parseRegQuery pulls the value named name out of `reg query` output, which
// renders each value as `    NAME    REG_TYPE    data`.
func parseRegQuery(out, name string) (RegValue, bool) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		t := strings.TrimLeft(line, " \t")
		if len(t) < len(name) || !strings.EqualFold(t[:len(name)], name) {
			continue
		}
		rest := t[len(name):]
		if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			continue // a longer name that merely starts with this one
		}
		rest = strings.TrimLeft(rest, " \t")
		typ, data := rest, ""
		if i := strings.IndexAny(rest, " \t"); i >= 0 {
			typ, data = rest[:i], strings.TrimLeft(rest[i:], " \t")
		}
		if !strings.HasPrefix(typ, "REG_") {
			continue
		}
		return RegValue{Type: typ, Data: data}, true
	}
	return RegValue{}, false
}

// RegQuery reads the current data and type of key\name, reporting whether the
// value exists. Callers use it to capture the pre-migration value for
// RecordRegSet, which is the only way the type survives into the journal.
func RegQuery(r Runner, key, name string) (RegValue, bool, error) {
	if r.Reg == nil {
		return RegValue{}, false, fmt.Errorf("migrate: no registry runner configured")
	}
	out, err := r.Reg("query", key, name, RegValue{})
	if err != nil {
		// reg.exe exits non-zero when the value (or the key) is not there.
		return RegValue{}, false, nil
	}
	v, ok := parseRegQuery(out, name)
	return v, ok, nil
}

func (j *Journal) reverseReg(e Entry, r Runner) (string, error) {
	key, name := e.Args["Key"], e.Args["Name"]
	where := key + `\` + name
	if runtime.GOOS != "windows" {
		return "", opErr(e, where, "not supported on this OS")
	}
	if r.Reg == nil {
		return "", opErr(e, where, "no registry runner configured")
	}
	cur, present, _ := RegQuery(r, key, name)
	old := RegValue{Type: e.Args["Type"], Data: e.Args["Old"]}

	if e.Args["Present"] == "true" {
		if present && cur == old {
			return skipped(e, "registry %s is already %s %q", where, old.Type, old.Data), nil
		}
		if _, err := r.Reg("set", key, name, old); err != nil {
			return "", opErr(e, where, "%v", err)
		}
		return fmt.Sprintf("registry %s restored to %s %q", where, old.Type, old.Data), nil
	}

	// The value did not exist before the migration, so the inverse is to delete
	// it. reg.exe errors when there is nothing to delete, which is the state we
	// wanted anyway.
	if !present {
		return skipped(e, "registry %s is already absent", where), nil
	}
	if _, err := r.Reg("delete", key, name, RegValue{}); err != nil {
		if _, stillThere, _ := RegQuery(r, key, name); stillThere {
			return "", opErr(e, where, "%v", err)
		}
	}
	return fmt.Sprintf("registry %s deleted", where), nil
}
