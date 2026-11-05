package trust

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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

// invalidJSON is the hash recorded for a settings.json tq cannot parse: it
// differs from every projection, so breaking the file is a change.
const invalidJSON = "!invalid-json"

// blockSuffix marks a virtual covered path, the part of a file tq covers:
// "<dir>/config.toml#tq" is the block of a Codex config.toml tq owns, and
// "<dir>/settings.json#tq" the enforcing keys of a Claude settings.json.
const blockSuffix = "#tq"

// enforcingSettingsKeys are the keys of a Claude settings.json that decide
// what runs: the hooks, the plugins and MCP servers that are on, the
// environment of every tool call, the status line command. The rest
// (theme, model, permissions the user sets in Claude Code) is not covered.
var enforcingSettingsKeys = []string{"hooks", "enabledPlugins", "enabledMcpjsonServers", "disableAllMcpServers", "env", "statusLine"}

// currentHash is the hash of a covered key as the record stores it: ""
// when it is absent, irregular when it exists but is not a regular file,
// the hex sha256 of its bytes otherwise. A "#tq" key hashes the part tq
// covers (blockHash, settingsHash). A file that exists but cannot be read
// is an error, which every caller treats as untrusted.
func currentHash(key string) (string, error) {
	if file, ok := strings.CutSuffix(key, blockSuffix); ok {
		if strings.EqualFold(filepath.Base(file), "settings.json") {
			return settingsHash(file)
		}
		return blockHash(file)
	}
	fi, err := os.Lstat(key)
	if err != nil {
		if absent(err) {
			return "", nil
		}
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return irregular, nil
	}
	return HashFile(key)
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

// settingsHash is the sha256 of the canonical JSON of the enforcing keys of
// a Claude settings.json (encoding/json sorts object keys; numbers keep
// their text). An absent file projects to {} like a file without those
// keys: neither runs anything. A file that is not a JSON object is
// invalidJSON.
func settingsHash(file string) (string, error) {
	proj := map[string]json.RawMessage{}
	fi, err := os.Lstat(file)
	switch {
	case err != nil && !absent(err):
		return "", err
	case err == nil && !fi.Mode().IsRegular():
		return irregular, nil
	case err == nil:
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		var doc map[string]json.RawMessage
		if json.Unmarshal(raw, &doc) != nil || doc == nil {
			return invalidJSON, nil
		}
		for _, k := range enforcingSettingsKeys {
			v, ok := doc[k]
			if !ok {
				continue
			}
			c, err := canonicalJSON(v)
			if err != nil {
				return invalidJSON, nil
			}
			proj[k] = c
		}
	}
	b, err := json.Marshal(proj)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalJSON re-encodes a JSON value with object keys sorted and no
// whitespace, numbers kept as written.
func canonicalJSON(v json.RawMessage) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(v))
	dec.UseNumber()
	var x any
	if err := dec.Decode(&x); err != nil {
		return nil, err
	}
	return json.Marshal(x)
}
