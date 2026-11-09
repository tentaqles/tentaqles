// Package store manages a client's lobe memory folder, $TQ_HOME/identities/<client>/lobe.
// It holds plain files, takes one writer at a time through an operating-system lock, and
// accepts only scrubbed text (scrub.Clean) for everything it writes.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// Mode says whether Open may create and write the store.
type Mode int

const (
	ReadOnly Mode = iota
	ReadWrite
)

// Error is a sentinel error of this package. It is a string type so the sentinels can be constants.
type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrBusy       = Error("lobe store: busy")
	ErrNoStore    = Error("lobe store: no store")
	ErrLinked     = Error("lobe store: a folder on the way is a link")
	ErrUnreadable = Error("lobe store: store.json does not parse")
	ErrForeign    = Error("lobe store: store.json names another client")
	ErrNewer      = Error("lobe store: store.json is from a newer version")
	ErrPath       = Error("lobe store: path leaves the store")

	errLineBreak = Error("lobe store: a line holds a line break")
)

const (
	storeJSONName = "store.json"
	lockName      = "lock"
	storeVersion  = 1
)

// Store is an open client store: the client's name and the store folder.
type Store struct {
	client string
	dir    string
}

// Open opens the store of client. ReadWrite creates the store folder and its store.json when they are
// missing; ReadOnly never creates anything.
func Open(client string, mode Mode) (*Store, error) {
	root := paths.IdentitiesRoot()
	dir := paths.LobeDir(client)
	for _, p := range []string{root, filepath.Join(root, client), dir} {
		if err := rejectLink(p); err != nil {
			return nil, err
		}
	}
	storeJSON := filepath.Join(dir, storeJSONName)

	if mode == ReadOnly {
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoStore
		} else if err != nil {
			return nil, err
		}
		if _, err := os.Stat(storeJSON); errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoStore
		} else if err != nil {
			return nil, err
		}
	} else {
		// Mkdir, not MkdirAll: a client folder that is gone means the client was removed.
		if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if _, err := os.Lstat(storeJSON); errors.Is(err, fs.ErrNotExist) {
			line := newStoreJSON(client)
			if err := writeFile(storeJSON, line); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(storeJSON)
	if err != nil {
		return nil, err
	}
	if err := checkStoreJSON(data, client); err != nil {
		return nil, err
	}
	return &Store{client: client, dir: dir}, nil
}

// newStoreJSON is the first store.json of a client: one line of compact JSON.
func newStoreJSON(client string) string {
	created := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	b, _ := json.Marshal(struct {
		V       int    `json:"v"`
		Client  string `json:"client"`
		Created string `json:"created"`
	}{storeVersion, client, created})
	return string(b) + "\n"
}

// checkStoreJSON accepts a store.json that is a JSON object for this client, at version 1 or lower.
func checkStoreJSON(data []byte, client string) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		return ErrUnreadable
	}
	var name string
	if err := json.Unmarshal(m["client"], &name); err != nil || name != client {
		return ErrForeign
	}
	var v float64
	if err := json.Unmarshal(m["v"], &v); err != nil {
		return ErrUnreadable
	}
	if v > storeVersion {
		return ErrNewer
	}
	return nil
}

// rejectLink refuses a path that exists and is a symbolic link, a junction or not a folder.
// A path that does not exist is fine.
func rejectLink(p string) error {
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !fi.IsDir() {
		return ErrLinked
	}
	return nil
}

// Dir is the store folder.
func (s *Store) Dir() string { return s.dir }

// Client is the name of the client the store belongs to.
func (s *Store) Client() string { return s.client }

// Read returns the bytes of the store-relative file rel. An error other than "does not exist" is retried:
// up to 3 more attempts, 5 milliseconds apart. On Windows a reader can meet a sharing violation or access
// denied for a few milliseconds while another process renames a new version over the file. The last error
// is returned when every attempt fails.
func (s *Store) Read(rel string) ([]byte, error) {
	full, err := s.resolve(rel)
	if err != nil {
		return nil, err
	}
	const retries = 3
	for attempt := 0; ; attempt++ {
		b, err := os.ReadFile(full)
		if err == nil || errors.Is(err, fs.ErrNotExist) || attempt == retries {
			return b, err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// resolve checks rel and returns its path inside the store folder.
func (s *Store) resolve(rel string) (string, error) {
	if err := checkRel(rel); err != nil {
		return "", err
	}
	return filepath.Join(s.dir, filepath.FromSlash(rel)), nil
}

// checkRel refuses a store-relative path that is empty, absolute, has a backslash or a colon anywhere (on
// Windows a colon names an alternate data stream), has a control character or 0x7f, or leaves the store
// through a .. element.
func checkRel(rel string) error {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.ContainsRune(rel, '\\') || strings.ContainsRune(rel, ':') {
		return ErrPath
	}
	for i := 0; i < len(rel); i++ {
		if rel[i] < 0x20 || rel[i] == 0x7f {
			return ErrPath
		}
	}
	for _, el := range strings.Split(rel, "/") {
		if el == ".." {
			return ErrPath
		}
	}
	c := path.Clean(rel)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return ErrPath
	}
	return nil
}

// Lock takes the store lock, the file lock in the store folder, and returns the function that releases
// it. It retries every 5 milliseconds until ctx is done, then returns ErrBusy.
func (s *Store) Lock(ctx context.Context) (func(), error) {
	f, err := os.OpenFile(filepath.Join(s.dir, lockName), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	for {
		ok, err := tryLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if ok {
			return releaser(f), nil
		}
		t := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			t.Stop()
			f.Close()
			return nil, ErrBusy
		case <-t.C:
		}
	}
}

// releaser returns a function that unlocks f and closes it; calling it twice is harmless.
func releaser(f *os.File) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			unlockFile(f)
			f.Close()
		})
	}
}
