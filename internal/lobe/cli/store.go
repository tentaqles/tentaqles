package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// openStore opens the store of client for a lobe verb. A lobe verb may create the store (F-01 step 3),
// so the store is opened read-write. A refused store gives an error that names the file or folder at
// fault, so the command exits 1 with that line.
func openStore(client string) (*store.Store, error) {
	st, err := store.Open(client, store.ReadWrite)
	if err != nil {
		return nil, storeRefusal(client, err)
	}
	return st, nil
}

// storeRefusal words the refusal of store.Open for a person: the line names store.json or the linked folder.
func storeRefusal(client string, err error) error {
	file := filepath.Join(paths.LobeDir(client), "store.json")
	switch {
	case errors.Is(err, store.ErrLinked):
		return fmt.Errorf("the lobe store of %s is refused: %s is a link or junction, and the lobe needs real folders. Replace the link with a folder", client, linkedFolder(client))
	case errors.Is(err, store.ErrForeign):
		return fmt.Errorf("the lobe store of %s is refused: %s names another client. Move that file aside, then run the command again: a missing store.json is created", client, file)
	case errors.Is(err, store.ErrUnreadable):
		return fmt.Errorf("the lobe store of %s is refused: %s does not parse", client, file)
	case errors.Is(err, store.ErrNewer):
		return fmt.Errorf("the lobe store of %s is refused: %s is from a newer version of tentaqles", client, file)
	}
	return fmt.Errorf("the lobe store of %s is refused: %w", client, err)
}

// linkedFolder names the first folder on the way to the store that is a link, a junction or not a folder.
// It falls back to the store folder itself when none is found.
func linkedFolder(client string) string {
	root := paths.IdentitiesRoot()
	for _, p := range []string{root, filepath.Join(root, client), paths.LobeDir(client)} {
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !fi.IsDir() {
			return p
		}
	}
	return paths.LobeDir(client)
}

// isDayFile reports whether name is a day file of the event log: events-YYYYMMDD.jsonl. It is the name
// rule of eventlog's readers, which do not export it.
func isDayFile(name string) bool {
	const pre, suf = "events-", ".jsonl"
	if len(name) != len(pre)+8+len(suf) || name[:len(pre)] != pre || name[len(name)-len(suf):] != suf {
		return false
	}
	for _, c := range name[len(pre) : len(pre)+8] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
