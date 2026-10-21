// Package store is the hub's per-workspace SQLite database: schema,
// migrations, queries and masking at write. There is one database per client
// workspace under $TQ_HOME/hub/<workspace>/, never inside a repository, and
// no query ever joins two workspaces.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/redact"
	_ "modernc.org/sqlite" // pure-Go driver "sqlite"; no cgo
)

var (
	// ErrNoHub means the workspace has no hub.db yet.
	ErrNoHub = errors.New("no hub database for this workspace")
	// ErrNotFound wraps every missing card, run, lane or decision.
	ErrNotFound = errors.New("not found")
)

func Dir(workspace string) string          { return filepath.Join(paths.Home(), "hub", workspace) }
func DBPath(workspace string) string       { return filepath.Join(Dir(workspace), "hub.db") }
func RunsDir(workspace string) string      { return filepath.Join(Dir(workspace), "runs") }
func ActivityPath(workspace string) string { return filepath.Join(Dir(workspace), "activity.jsonl") }

// ValidWorkspace refuses names that would escape $TQ_HOME/hub/.
func ValidWorkspace(name string) error {
	if name == "" || name == "." || name == ".." || strings.TrimSpace(name) != name || strings.ContainsAny(name, `/\:`) {
		return fmt.Errorf("invalid workspace name %q", name)
	}
	return nil
}

// Change is one store mutation, fanned out to subscribers (the web page's SSE stream).
type Change struct {
	Workspace string `json:"workspace"`
	Kind      string `json:"kind"` // card | comment | run | decision | activity | external
	CardID    string `json:"card,omitempty"`
	RunID     string `json:"run,omitempty"`
}

type Store struct {
	DB        *sql.DB
	Workspace string
	classes   []string
	readOnly  bool

	mu      sync.Mutex
	subs    map[int]chan Change
	nextSub int
	logMu   sync.Mutex // serializes activity.jsonl appends from this process
}

func dsn(path string) string {
	return path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
}

// fileURI is the SQLite URI form of an absolute path (needed for mode=ro).
func fileURI(path string) string {
	p := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + p
	}
	return "file://" + p
}

// Open opens (creating when needed) the workspace's database and migrates it.
func Open(workspace string, classes []string) (*Store, error) {
	if err := ValidWorkspace(workspace); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(RunsDir(workspace), 0o700); err != nil {
		return nil, err
	}
	return open(workspace, classes)
}

// OpenExisting is Open for a database that must already exist.
func OpenExisting(workspace string, classes []string) (*Store, error) {
	if err := ValidWorkspace(workspace); err != nil {
		return nil, err
	}
	if _, err := os.Stat(DBPath(workspace)); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoHub
		}
		return nil, err
	}
	return open(workspace, classes)
}

// OpenReadOnly opens an existing database without migrating or writing (tq doctor).
func OpenReadOnly(workspace string) (*Store, error) {
	if err := ValidWorkspace(workspace); err != nil {
		return nil, err
	}
	if _, err := os.Stat(DBPath(workspace)); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoHub
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", fileURI(DBPath(workspace))+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, err
	}
	s := &Store{DB: db, Workspace: workspace, classes: []string{"all"}, readOnly: true, subs: map[int]chan Change{}}
	if _, err := s.SchemaVersion(); err != nil {
		db.Close()
		return nil, fmt.Errorf("hub database %s: %w", DBPath(workspace), err)
	}
	return s, nil
}

func open(workspace string, classes []string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn(DBPath(workspace)))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	s := &Store{DB: db, Workspace: workspace, classes: effectiveClasses(classes), subs: map[int]chan Change{}}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("hub database %s: %w", DBPath(workspace), err)
	}
	_ = os.Chmod(DBPath(workspace), 0o600)
	return s, nil
}

// effectiveClasses enforces the fail-closed rule: empty means all; otherwise secrets is always included.
func effectiveClasses(classes []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, c := range classes {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	if len(out) == 0 || seen["all"] {
		return []string{"all"}
	}
	if !seen["secrets"] {
		out = append(out, "secrets")
	}
	return out
}

func (s *Store) Close() error {
	s.mu.Lock()
	for id, ch := range s.subs {
		close(ch)
		delete(s.subs, id)
	}
	s.mu.Unlock()
	return s.DB.Close()
}

// Mask applies the workspace's data classes; every stored text goes through it.
func (s *Store) Mask(text string) string {
	if text == "" {
		return text
	}
	return redact.Redact(text, s.classes)
}

func (s *Store) Classes() []string { return append([]string(nil), s.classes...) }

func (s *Store) Subscribe() (<-chan Change, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSub
	s.nextSub++
	ch := make(chan Change, 64)
	s.subs[id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if c, ok := s.subs[id]; ok {
			close(c)
			delete(s.subs, id)
		}
	}
}

// Publish never blocks: a slow subscriber misses a change and refreshes on the next one.
func (s *Store) Publish(c Change) {
	c.Workspace = s.Workspace
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- c:
		default:
		}
	}
}

// ChangeStamp changes whenever any process (a CLI writing directly) changed cards, runs or activity.
func (s *Store) ChangeStamp() (string, error) {
	var v string
	err := s.DB.QueryRow(`SELECT (SELECT COALESCE(MAX(rowid), 0) FROM activity) || ':' ||
		(SELECT COALESCE(MAX(updated_at), '') FROM cards) || ':' || (SELECT COALESCE(MAX(updated_at), '') FROM runs)`).Scan(&v)
	return v, err
}

// Now is the timestamp format of every column: UTC RFC 3339.
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

func (s *Store) SchemaVersion() (int, error) {
	var v sql.NullInt64
	if err := s.DB.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		return 0, err
	}
	return int(v.Int64), nil
}

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	v, err := s.SchemaVersion()
	if err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf("hub.db is from a newer tq (schema %d, this tq knows %d); upgrade tq", v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.DB.Begin() // _txlock=immediate: a second process waits here
		if err != nil {
			return err
		}
		var cur sql.NullInt64
		if err := tx.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&cur); err != nil {
			tx.Rollback()
			return err
		}
		if int(cur.Int64) > i { // another process applied it meanwhile
			tx.Rollback()
			continue
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`DELETE FROM schema_version`); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_version(version) VALUES (?)`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// nullStr stores "" as NULL.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
