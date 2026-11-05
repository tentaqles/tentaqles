package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
)

// HashFile is the hex sha256 of path's bytes.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// irregular is the hash recorded for a covered path that exists but is not
// a regular file (a symlink, a directory): it differs from every file hash,
// so a file swapped for a link is a change, and so is the reverse.
const irregular = "!irregular"

// blockSuffix marks the virtual covered path of the tq-owned block of a
// Codex config.toml: "<dir>/config.toml#tq". The rest of the file is the
// user's and stays out of the record.
const blockSuffix = "#tq"

// memoThreshold: a regular file of at least this size (the tq binary) keeps
// its hash in an in-process memo keyed by size and mtime, so the several
// trust checks one command makes hash it once. Smaller files are hashed on
// every call. Nothing is cached on disk.
const memoThreshold = 1 << 20

type memoEntry struct {
	size  int64
	mtime time.Time
	hash  string
}

var memo sync.Map

// currentHash is the hash of a covered path as the record stores it: ""
// when it is absent, irregular when it exists but is not a regular file,
// the hex sha256 of its bytes otherwise. For a "#tq" path it is the hash of
// the managed block of that config.toml ("" when the file or the block is
// missing). A file that exists but cannot be read is an error, which every
// caller treats as untrusted.
func currentHash(path string) (string, error) {
	if file, ok := strings.CutSuffix(path, blockSuffix); ok {
		return blockHash(file)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		if absent(err) {
			return "", nil
		}
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return irregular, nil
	}
	big := fi.Size() >= memoThreshold
	if big {
		if v, ok := memo.Load(path); ok {
			e := v.(memoEntry)
			if e.size == fi.Size() && e.mtime.Equal(fi.ModTime()) {
				return e.hash, nil
			}
		}
	}
	h, err := HashFile(path)
	if err != nil {
		return "", err
	}
	if big {
		memo.Store(path, memoEntry{fi.Size(), fi.ModTime(), h})
	}
	return h, nil
}

// absent reports a stat error that means "nothing there": the file, or a
// directory on its path, does not exist (or a path component is a file).
func absent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

func blockHash(file string) (string, error) {
	fi, err := os.Lstat(file)
	if err != nil {
		if absent(err) {
			return "", nil
		}
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return irregular, nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	block, ok := codexcfg.ManagedBlock(string(raw))
	if !ok {
		return "", nil
	}
	sum := sha256.Sum256([]byte(block))
	return hex.EncodeToString(sum[:]), nil
}
