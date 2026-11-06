package trust

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
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
// "<dir>/settings.json#tq" the enforcing keys of a Claude settings.json,
// and "<dir>/installed_plugins.json#tq" where Claude Code loads the
// tentaqles plugin from.
const blockSuffix = "#tq"

// enforcingSettingsKeys are the keys of a Claude settings.json that decide
// what runs or with which credentials: the hooks and the switch that turns
// them all off, the plugins and MCP servers that are on, the environment of
// every tool call, the status line, the login method and the credential
// helpers. Any other key whose name ends in "Helper" or "Command" names a
// program Claude Code runs and is covered too (enforcingSettingsKey). The
// rest (theme, model, and permissions, which tq never touches) is not.
var enforcingSettingsKeys = []string{
	"hooks", "disableAllHooks", "enabledPlugins", "enabledMcpjsonServers", "disableAllMcpServers",
	"env", "statusLine", "apiKeyHelper", "otelHeadersHelper", "awsAuthRefresh", "awsCredentialExport", "forceLoginMethod",
}

func enforcingSettingsKey(k string) bool {
	for _, e := range enforcingSettingsKeys {
		if k == e {
			return true
		}
	}
	return strings.HasSuffix(k, "Helper") || strings.HasSuffix(k, "Command")
}

// currentHash is the hash of a covered key as the record stores it: ""
// when it is absent, irregular when it exists but is not a regular file,
// the hex sha256 of its bytes otherwise. A "#tq" key hashes the part tq
// covers (settingsHash, installedHash). A file that exists but
// cannot be read is an error, which every caller treats as untrusted.
func currentHash(key string) (string, error) {
	if file, ok := strings.CutSuffix(key, blockSuffix); ok {
		switch strings.ToLower(filepath.Base(file)) {
		case "settings.json":
			return settingsHash(file)
		case "installed_plugins.json":
			return installedHash(file)
		}
		// A key tq no longer covers, from a record signed by an older tq, the block it kept in
		// Codex's config.toml: it hashes as absent, so one that held anything reads as removed
		// until a human signs again.
		return "", nil
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
		for k, v := range doc {
			if !enforcingSettingsKey(k) {
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

// installedEntry is what installed_plugins.json#tq keeps of one install of
// the tentaqles plugin: where Claude Code loads it from, and its version.
type installedEntry struct {
	Plugin      string `json:"plugin"`
	InstallPath string `json:"installPath"`
	Version     string `json:"version"`
	// Scope picks the entry PluginRoot uses (user first); not hashed.
	Scope string `json:"-"`
}

// readInstalled reads the tentaqles entries of a Claude config dir's
// plugins/installed_plugins.json, sorted. ok is false when the file is not
// a JSON object with a plugins map.
func readInstalled(file string) (entries []installedEntry, ok bool, err error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		if absent(err) {
			return nil, true, nil
		}
		return nil, false, err
	}
	var doc struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
			Version     string `json:"version"`
			Scope       string `json:"scope"`
		} `json:"plugins"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil, false, nil
	}
	for name, insts := range doc.Plugins {
		if !strings.HasPrefix(name, "tentaqles@") {
			continue
		}
		for _, in := range insts {
			entries = append(entries, installedEntry{name, in.InstallPath, in.Version, in.Scope})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Plugin != entries[j].Plugin {
			return entries[i].Plugin < entries[j].Plugin
		}
		return entries[i].InstallPath < entries[j].InstallPath
	})
	return entries, true, nil
}

// installedHash is the sha256 of the tentaqles entries of
// installed_plugins.json, installPath and version only: an absent file and
// one without the plugin project to [], a file that is not JSON is
// invalidJSON. The timestamps Claude Code rewrites are not covered.
func installedHash(file string) (string, error) {
	fi, err := os.Lstat(file)
	switch {
	case err != nil && !absent(err):
		return "", err
	case err == nil && !fi.Mode().IsRegular():
		return irregular, nil
	}
	entries, ok, err := readInstalled(file)
	if err != nil {
		return "", err
	}
	if !ok {
		return invalidJSON, nil
	}
	if entries == nil {
		entries = []installedEntry{}
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// hashKeys hashes every key (currentHash), a few at a time: the files are
// small and many, and opening them is what costs on Windows.
func hashKeys(keys []string) (map[string]string, error) {
	type result struct {
		key, hash string
		err       error
	}
	out := make(map[string]string, len(keys))
	jobs := make(chan string)
	results := make(chan result)
	workers := 8
	if len(keys) < workers {
		workers = len(keys)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range jobs {
				h, err := currentHash(k)
				results <- result{k, h, err}
			}
		}()
	}
	go func() {
		for _, k := range keys {
			jobs <- k
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	var first error
	for r := range results {
		if r.err != nil && first == nil {
			first = fmt.Errorf("trust: hashing %s: %w", r.key, r.err)
		}
		out[r.key] = r.hash
	}
	return out, first
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
