package bundle

import (
	"os"
	"path/filepath"
	"time"
)

// inUseWindow is how recently a session file must have been touched for the
// workspace to be considered actively in use.
const inUseWindow = 10 * time.Minute

// InUse reports whether <dir>/sessions contains any file modified within the
// last 10 minutes. A missing sessions directory yields false, not an error.
func InUse(dir string) bool {
	sessions := filepath.Join(dir, "sessions")
	if _, err := os.Stat(sessions); err != nil {
		return false
	}

	cutoff := time.Now().Add(-inUseWindow)
	found := false
	_ = filepath.Walk(sessions, func(path string, info os.FileInfo, err error) error {
		if err != nil || found {
			return nil
		}
		if !info.IsDir() && info.ModTime().After(cutoff) {
			found = true
		}
		return nil
	})
	return found
}
