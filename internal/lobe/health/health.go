// Package health reports the lobe's findings for `tentaqles doctor` (F-07 step 5). It returns its own
// Finding type; internal/cmd converts those to doctor.Finding and appends them to doctor's list. It stays
// out of internal/doctor, which the guard imports. It never creates a store: a store is opened read-only.
package health

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/settings"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// The codes of F-07 step 5 that this package reports, and lobe-hooks-untrusted (feature 010, the Codex
// handler). Level is "error" or "warn", as the table gives.
const (
	CodeStoreLinked     = "lobe-store-linked"
	CodeStoreForeign    = "lobe-store-foreign"
	CodeStoreMissing    = "lobe-store-missing"
	CodeStoreUnreadable = "lobe-store-unreadable"
	CodeStoreVersion    = "lobe-store-version"
	CodeHooksMissing    = "lobe-hooks-missing"
	CodeSettingInvalid  = "lobe-setting-invalid"
	CodeHookFailing     = "lobe-hook-failing"
	CodeHooksUntrusted  = "lobe-hooks-untrusted"
)

// HookFailWindow and HookFailMin are the thresholds of lobe-hook-failing: 5 or more lines of
// stats/hook-errors.jsonl from the last 24 hours (F-07 step 5, proposed there).
const (
	HookFailWindow = 24 * time.Hour
	HookFailMin    = 5
)

// hookErrorsFile is the store-relative file of the lobe hooks' error lines (F-01 step 1).
const hookErrorsFile = "stats/hook-errors.jsonl"

// lobeHookName is the plugin script that runs the lobe's hooks (plugin/hooks/hooks.json).
const lobeHookName = "lobe_hook.sh"

// Finding is one lobe finding. Client names the client it is about.
type Finding struct {
	Level  string
	Code   string
	Client string
	Msg    string
	Fix    string
}

// Client is what the checks need to know about one client.
type Client struct {
	// Name is the client's name; the store is found by it.
	Name string
	// WS is the client's workspace, read for the lobe key of its manifest. Nil skips the settings check.
	WS *resolve.Workspace
	// PluginFound says whether a tentaqles plugin is installed for the client's Claude Code, and HooksJSON
	// is that plugin's hooks/hooks.json (nil when it cannot be read). internal/cmd finds the plugin with
	// guardplugin.PluginRoot, which this package may not import.
	PluginFound bool
	HooksJSON   []byte
	// CodexHooks is the state of the lobe's Codex handler in the client's Codex config, as
	// codexconfig.Trust.String gives it ("trusted", "untrusted", "modified", "disabled", "missing"). It is ""
	// when the client has no codex in its scope. internal/cmd computes it (codexconfig may not be imported here).
	CodexHooks string
}

// Findings returns the lobe findings of the clients, in the order of the clients. now is the time the
// 24-hour window of lobe-hook-failing ends at.
func Findings(clients []Client, now time.Time) []Finding {
	var out []Finding
	for _, c := range clients {
		out = append(out, storeFindings(c, now)...)
		if c.PluginFound && !hasLobeEntry(c.HooksJSON) {
			out = append(out, Finding{
				Level:  "warn",
				Code:   CodeHooksMissing,
				Client: c.Name,
				Msg:    "the tentaqles plugin this client's Claude Code loads has no lobe hook entries",
				Fix:    "update the tentaqles plugin in Claude Code, so that its hooks name the lobe's hooks",
			})
		}
		if c.WS != nil {
			if f, ok := settingFinding(c); ok {
				out = append(out, f)
			}
		}
		if c.CodexHooks != "" && c.CodexHooks != "trusted" {
			out = append(out, Finding{
				Level:  "warn",
				Code:   CodeHooksUntrusted,
				Client: c.Name,
				Msg:    "the lobe's Codex hook in this client's Codex config is " + c.CodexHooks,
				Fix:    "tentaqles settings render " + c.Name,
			})
		}
	}
	return out
}

// storeFindings checks the store of one client, read-only. The hook-error count runs only on a store that
// opened, since a store that was refused is not read further.
func storeFindings(c Client, now time.Time) []Finding {
	st, err := store.Open(c.Name, store.ReadOnly)
	if err != nil {
		return []Finding{storeFinding(c.Name, err)}
	}
	n := recentHookErrors(st, now)
	if n >= HookFailMin {
		return []Finding{{
			Level:  "warn",
			Code:   CodeHookFailing,
			Client: c.Name,
			Msg:    fmt.Sprintf("%d lobe hook failures in the last 24 hours (stats/hook-errors.jsonl)", n),
			Fix:    "run tentaqles lobe status to see the store and the hook wiring; a failing hook writes nothing and prints nothing",
		}}
	}
	return nil
}

// storeFinding maps a refusal of store.Open (read-only) to its code. Any other error reads as unreadable.
func storeFinding(client string, err error) Finding {
	switch {
	case errors.Is(err, store.ErrLinked):
		return Finding{"error", CodeStoreLinked, client,
			"the lobe folder of this client, or a folder on its way, is a link or junction; the lobe is off for this client",
			"replace the link with a real folder, then run tentaqles settings render " + client}
	case errors.Is(err, store.ErrForeign):
		return Finding{"error", CodeStoreForeign, client,
			"the lobe's store.json names another client",
			"move store.json aside, then run tentaqles settings render " + client}
	case errors.Is(err, store.ErrNoStore):
		return Finding{"warn", CodeStoreMissing, client,
			"the lobe store of this client is missing",
			"run tentaqles settings render " + client + " to create it"}
	case errors.Is(err, store.ErrNewer):
		return Finding{"warn", CodeStoreVersion, client,
			"the lobe's store.json is from a newer version of tentaqles",
			"upgrade tentaqles to a version that reads this store"}
	default:
		return Finding{"warn", CodeStoreUnreadable, client,
			"the lobe's store.json does not parse",
			"move store.json aside, then run tentaqles settings render " + client}
	}
}

// settingFinding reports the lobe settings of the client when one holds a value outside its list: one
// finding for the client, however many keys are wrong.
func settingFinding(c Client) (Finding, bool) {
	s := settings.From(c.WS)
	if !s.Invalid() {
		return Finding{}, false
	}
	var keys []string
	for _, x := range s {
		if x.Source == settings.SourceInvalid {
			keys = append(keys, x.Key)
		}
	}
	fix := "set each value with tentaqles lobe set <key> <value>; tentaqles lobe settings shows them"
	if settings.NotAMapping(c.WS.Manifest) {
		// `lobe set` cannot write into a lobe value that is not a mapping, so the fix is by hand.
		fix = "edit the lobe key of the manifest " + c.WS.ManifestPath + " by hand into a mapping of the settings " +
			"(or delete the key); tentaqles lobe settings shows them"
	}
	return Finding{
		Level:  "warn",
		Code:   CodeSettingInvalid,
		Client: c.Name,
		Msg: "the lobe setting " + strings.Join(keys, ", ") + " holds a value outside its list; " +
			"the safe value is used until it is fixed",
		Fix: fix,
	}, true
}

// hookErrorsTail is the most of stats/hook-errors.jsonl that is read: its last 1 MiB. A line that starts
// before that window is not counted.
const hookErrorsTail = 1 << 20

// readHookErrorsTail returns the whole lines of the last hookErrorsTail bytes of the hook-error file. When
// the file is longer, the line the window starts in is dropped. The file is read with os, not Store.Read,
// so that a large file is never read whole; the store is already open read-only.
func readHookErrorsTail(st *store.Store) ([]byte, error) {
	f, err := os.Open(filepath.Join(st.Dir(), filepath.FromSlash(hookErrorsFile)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()
	n := min(size, hookErrorsTail)
	buf := make([]byte, n)
	if _, err := io.ReadFull(io.NewSectionReader(f, size-n, n), buf); err != nil {
		return nil, err
	}
	if size > n {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return nil, nil
		}
		buf = buf[i+1:]
	}
	return buf, nil
}

// recentHookErrors counts the lines of stats/hook-errors.jsonl whose "at" time is within the window that
// ends at now. It reads at most the last 1 MiB of the file (readHookErrorsTail). A line that does not parse
// is not counted. A missing file counts 0.
func recentHookErrors(st *store.Store, now time.Time) int {
	data, err := readHookErrorsTail(st)
	if err != nil {
		return 0
	}
	since := now.Add(-HookFailWindow)
	n := 0
	// The scanner's buffer holds two windows: a line of the whole window and its break is one token.
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), 2*hookErrorsTail)
	for sc.Scan() {
		var e struct {
			At string `json:"at"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339, e.At)
		if err != nil {
			continue
		}
		if !at.Before(since) {
			n++
		}
	}
	return n
}

// hasLobeEntry reports whether a hooks.json names the lobe's script in some hook command. Bytes that do not
// parse have no lobe entry.
func hasLobeEntry(data []byte) bool {
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	for _, groups := range doc.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if strings.Contains(h.Command, lobeHookName) {
					return true
				}
			}
		}
	}
	return false
}
