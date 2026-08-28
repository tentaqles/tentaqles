package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/cli/internal/gitcfg"
	"github.com/tentaqles/tentaqles/cli/internal/paths"
)

// OpKind names a recorded mutation. Every kind has exactly one inverse, applied
// by Restore.
type OpKind string

const (
	// OpMoveDir moved From to To. Reverse: move To back to From.
	OpMoveDir OpKind = "move-dir"
	// OpMakeLink created a link at Path pointing at Target. Reverse: remove
	// the link (Path must still be a link).
	OpMakeLink OpKind = "make-link"
	// OpRemoveLink removed the link that stood at Path pointing at Target.
	// Reverse: recreate it.
	OpRemoveLink OpKind = "remove-link"
	// OpWriteFile overwrote (or created) Path. Backup is the journal-relative
	// copy of the previous content, or "" if Path did not exist.
	// Reverse: restore Backup, or delete Path.
	OpWriteFile OpKind = "write-file"
	// OpDeleteFile deleted Path, whose previous content is at Backup.
	// Reverse: restore Backup.
	OpDeleteFile OpKind = "delete-file"
	// OpGitGlobalSet set global git config Key to New. Old/Present describe
	// the value before. Reverse: set Old back, or unset Key.
	OpGitGlobalSet OpKind = "git-global-set"
	// OpRegSet set a Windows registry value. Old/Present describe the value
	// before. Reverse: set Old back, or delete the value.
	OpRegSet OpKind = "reg-set"
)

// Entry is one recorded mutation. It is written to journal.json before the
// caller performs the mutation, so a crash mid-migration still leaves a
// reversible record.
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

// Runner carries the external commands Restore needs. Production callers pass
// DefaultRunner(); tests pass fakes.
type Runner struct {
	Git func(args ...string) (string, error)
	Reg func(action, key, name, value string) (string, error)
}

// DefaultRunner returns a Runner backed by the real git and reg.exe binaries.
func DefaultRunner() Runner {
	return Runner{Git: runGit, Reg: runReg}
}

// runGit shells out to the real git binary.
func runGit(args ...string) (string, error) { return gitcfg.RunGit(args...) }

// runReg shells out to reg.exe. action is one of "query", "set", "delete";
// value is only used by "set". It errors off Windows so a caller that reaches
// it by mistake fails loudly instead of silently doing nothing.
func runReg(action, key, name, value string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("reg %s: not supported on this OS", action)
	}
	var args []string
	switch action {
	case "query":
		args = []string{"query", key, "/v", name}
	case "set":
		args = []string{"add", key, "/v", name, "/t", "REG_SZ", "/d", value, "/f"}
	case "delete":
		args = []string{"delete", key, "/v", name, "/f"}
	default:
		return "", fmt.Errorf("reg: unknown action %q", action)
	}
	out, err := exec.Command("reg", args...).CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return s, fmt.Errorf("reg %s %s\\%s: %w (%s)", action, key, name, err, s)
	}
	return s, nil
}

const journalName = "journal.json"

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
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o755); err != nil {
		return nil, fmt.Errorf("migrate.Open: %w", err)
	}
	j := &Journal{Dir: dir, TS: ts}
	if _, err := os.Stat(filepath.Join(dir, journalName)); err == nil {
		return load(dir)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("migrate.Open: %w", err)
	}
	if err := j.write(); err != nil {
		return nil, err
	}
	return j, nil
}

// Load reads an existing journal. The special timestamp "latest" selects the
// newest backup directory that contains a journal.json.
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
		if _, err := os.Stat(filepath.Join(root, e.Name(), journalName)); err == nil {
			names = append(names, e.Name())
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
		return nil, fmt.Errorf("migrate.Load: reading %s: %w", p, err)
	}
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
// fsynced, then renamed over journal.json, so a crash can never leave a
// half-written journal behind.
func (j *Journal) write() error {
	raw, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return fmt.Errorf("migrate: encoding journal: %w", err)
	}
	raw = append(raw, '\n')
	dst := filepath.Join(j.Dir, journalName)
	tmp := dst + ".tq-tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("migrate: writing %s: %w", tmp, err)
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return fmt.Errorf("migrate: writing %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("migrate: syncing %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("migrate: closing %s: %w", tmp, err)
	}
	if runtime.GOOS == "windows" {
		// os.Rename cannot replace an existing file on Windows.
		if _, err := os.Stat(dst); err == nil {
			if err := os.Remove(dst); err != nil {
				return fmt.Errorf("migrate: replacing %s: %w", dst, err)
			}
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("migrate: renaming %s to %s: %w", tmp, dst, err)
	}
	return nil
}

// Record appends an entry and persists the journal before returning. Callers
// must Record first and perform the mutation afterwards.
func (j *Journal) Record(step string, op OpKind, args map[string]string) error {
	cp := make(map[string]string, len(args))
	for k, v := range args {
		cp[k] = v
	}
	j.Entries = append(j.Entries, Entry{
		Seq:  len(j.Entries) + 1,
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

// BackupFile copies path into the journal's files/ directory and returns the
// journal-relative slot ("files/3"). A path that does not exist backs up to ""
// — the caller records that as "there was nothing here before".
func (j *Journal) BackupFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("migrate.BackupFile: reading %s: %w", path, err)
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	n, err := j.nextSlot()
	if err != nil {
		return "", err
	}
	rel := fmt.Sprintf("files/%d", n)
	if err := os.WriteFile(filepath.Join(j.Dir, filepath.FromSlash(rel)), raw, mode); err != nil {
		return "", fmt.Errorf("migrate.BackupFile: writing %s: %w", rel, err)
	}
	return rel, nil
}

// nextSlot returns the lowest unused numeric slot under files/.
func (j *Journal) nextSlot() (int, error) {
	dir := filepath.Join(j.Dir, "files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("migrate.BackupFile: %w", err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("migrate.BackupFile: reading %s: %w", dir, err)
	}
	return len(ents), nil
}

// Restore replays the journal in reverse Seq order, applying each entry's
// inverse. Every inverse verifies its preconditions first; the first failure
// stops the replay and returns the lines completed so far plus an error naming
// the sequence number, operation, and path. The journal file itself is never
// modified, so a fixed-up state can simply be restored again; what was done is
// appended to restore.log in the journal directory.
func (j *Journal) Restore(r Runner) ([]string, error) {
	var lines []string
	var failure error
	for i := len(j.Entries) - 1; i >= 0; i-- {
		line, err := j.reverse(j.Entries[i], r)
		if err != nil {
			failure = err
			break
		}
		lines = append(lines, line)
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
	f, err := os.OpenFile(filepath.Join(j.Dir, "restore.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
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

func (j *Journal) reverse(e Entry, r Runner) (string, error) {
	switch e.Op {
	case OpMoveDir:
		from, to := e.Args["From"], e.Args["To"]
		if _, err := os.Lstat(to); err != nil {
			return "", opErr(e, to, "expected the moved directory here, but it is gone (%v)", err)
		}
		if _, err := os.Lstat(from); err == nil {
			return "", opErr(e, from, "the original location already exists; refusing to overwrite it")
		}
		if err := MoveDir(to, from); err != nil {
			return "", opErr(e, to, "%v", err)
		}
		return fmt.Sprintf("moved %s back to %s", to, from), nil

	case OpMakeLink:
		path := e.Args["Path"]
		if ok, _ := IsLink(path); !ok {
			return "", opErr(e, path, "expected a link created by tq, but it is not a link (it may have been removed or replaced by hand)")
		}
		if err := RemoveLink(path); err != nil {
			return "", opErr(e, path, "%v", err)
		}
		return fmt.Sprintf("removed link %s", path), nil

	case OpRemoveLink:
		path, target := e.Args["Path"], e.Args["Target"]
		if _, err := os.Lstat(path); err == nil {
			return "", opErr(e, path, "something already exists here; refusing to replace it with a link")
		}
		if err := MakeLink(path, target); err != nil {
			return "", opErr(e, path, "%v", err)
		}
		return fmt.Sprintf("recreated link %s -> %s", path, target), nil

	case OpWriteFile, OpDeleteFile:
		path, backup := e.Args["Path"], e.Args["Backup"]
		if backup == "" {
			if _, err := os.Lstat(path); err != nil {
				if os.IsNotExist(err) {
					return fmt.Sprintf("%s already absent", path), nil
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
		mode := os.FileMode(0o644)
		if fi, err := os.Stat(src); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", opErr(e, path, "%v", err)
		}
		if err := os.WriteFile(path, raw, mode); err != nil {
			return "", opErr(e, path, "%v", err)
		}
		return fmt.Sprintf("restored %s from %s", path, backup), nil

	case OpGitGlobalSet:
		key := e.Args["Key"]
		if r.Git == nil {
			return "", opErr(e, key, "no git runner configured")
		}
		if e.Args["Present"] == "true" {
			if _, err := r.Git("config", "--global", key, e.Args["Old"]); err != nil {
				return "", opErr(e, key, "%v", err)
			}
			return fmt.Sprintf("git config --global %s restored to %q", key, e.Args["Old"]), nil
		}
		if _, err := r.Git("config", "--global", "--unset", key); err != nil {
			return "", opErr(e, key, "%v", err)
		}
		return fmt.Sprintf("git config --global %s unset", key), nil

	case OpRegSet:
		key, name := e.Args["Key"], e.Args["Name"]
		where := key + `\` + name
		if runtime.GOOS != "windows" {
			return "", opErr(e, where, "not supported on this OS")
		}
		if r.Reg == nil {
			return "", opErr(e, where, "no registry runner configured")
		}
		if e.Args["Present"] == "true" {
			if _, err := r.Reg("set", key, name, e.Args["Old"]); err != nil {
				return "", opErr(e, where, "%v", err)
			}
			return fmt.Sprintf("registry %s restored to %q", where, e.Args["Old"]), nil
		}
		if _, err := r.Reg("delete", key, name, ""); err != nil {
			return "", opErr(e, where, "%v", err)
		}
		return fmt.Sprintf("registry %s deleted", where), nil

	default:
		return "", fmt.Errorf("restore: seq %d: unknown op %q", e.Seq, e.Op)
	}
}
