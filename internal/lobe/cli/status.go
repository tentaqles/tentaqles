package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/pathcmp"
	"github.com/tentaqles/tentaqles/internal/lobe/settings"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// storeVersion is the store.json version this binary writes and reads (store.storeVersion).
const storeVersion = 1

// lobeHookName is the script of the plugin's hooks that run the lobe (plugin/hooks/hooks.json).
const lobeHookName = "lobe_hook.sh"

// eventCounts are the event lines of a store: all of them, by kind, the day files and the broken lines.
type eventCounts struct {
	Total, Fail, OK, HiddenFail, Days, Skipped int
}

// statusDoc is the `lobe status --json` object. Later features add keys; none is renamed. Settings is the
// object `lobe settings --json` prints (settingsJSON), so both commands give the same value types.
type statusDoc struct {
	Client   string            `json:"client"`
	Store    storeDoc          `json:"store"`
	Settings json.RawMessage   `json:"settings"`
	Events   eventsDoc         `json:"events"`
	Hooks    map[string]string `json:"hooks"`
}

type storeDoc struct {
	State string `json:"state"`
	V     int    `json:"v"`
}

type eventsDoc struct {
	Total      int `json:"total"`
	Fail       int `json:"fail"`
	OK         int `json:"ok"`
	HiddenFail int `json:"hidden_fail"`
	Days       int `json:"days"`
	Skipped    int `json:"skipped"`
}

// Status prints what the lobe of the client holds: the client, the store's state, the nine settings, the
// event counts, and whether the plugin this client's Claude Code loads has the lobe's hooks. hooksJSON is
// the bytes of that plugin's hooks/hooks.json and hooksFound says whether a plugin is installed at all
// (internal/cmd finds it with guardplugin.PluginRoot; this package cannot import guardplugin). A refused
// store is an error, so the command exits 1 with the line that names the file at fault.
func Status(d Deps, clientFlag string, asJSON bool, hooksJSON []byte, hooksFound bool, out io.Writer) error {
	ws, err := Target(d, clientFlag)
	if err != nil {
		return err
	}
	if err := CheckLogin(d, ws); err != nil {
		return err
	}
	st, err := openStore(ws.Name)
	if err != nil {
		return err
	}
	counts, err := countEvents(st)
	if err != nil {
		return err
	}
	set := settings.From(ws)
	hooks := hooksState(hooksJSON, hooksFound)

	if asJSON {
		doc := statusDoc{
			Client:   ws.Name,
			Store:    storeDoc{State: "ok", V: storeVersion},
			Settings: json.RawMessage(settingsJSON(set)),
			Events: eventsDoc{
				Total: counts.Total, Fail: counts.Fail, OK: counts.OK, HiddenFail: counts.HiddenFail,
				Days: counts.Days, Skipped: counts.Skipped,
			},
			Hooks: map[string]string{"claude": hooks},
		}
		return json.NewEncoder(out).Encode(doc)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "client: %s\n", ws.Name)
	fmt.Fprintln(&b, "store: ok")
	fmt.Fprintln(&b, "settings:")
	for _, line := range set.Lines() {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	fmt.Fprintf(&b, "events: total %d (fail %d, ok %d, hidden_fail %d), %d day file(s), skipped %d\n",
		counts.Total, counts.Fail, counts.OK, counts.HiddenFail, counts.Days, counts.Skipped)
	fmt.Fprintf(&b, "hooks: claude %s\n", hooks)
	if line := loginLine(d, ws.Name); line != "" {
		fmt.Fprintln(&b, line)
	}
	_, err = io.WriteString(out, b.String())
	return err
}

// countEvents counts the stored event lines by kind. Lines that are not JSON objects are skipped and
// counted by eventlog.Scan; a line whose kind is none of the three is counted in the total only.
func countEvents(st *store.Store) (eventCounts, error) {
	var c eventCounts
	files, skipped, err := eventlog.Scan(st, func(line []byte) bool {
		c.Total++
		var e struct {
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal(line, &e)
		switch e.Kind {
		case "fail":
			c.Fail++
		case "ok":
			c.OK++
		case "hidden-fail":
			c.HiddenFail++
		}
		return true
	})
	if err != nil {
		return eventCounts{}, err
	}
	c.Days = files
	c.Skipped = skipped
	return c, nil
}

// hooksState is "ok" when the plugin's hooks name the lobe's script, "missing" when a plugin is installed
// without it, and "none" when no plugin is installed.
func hooksState(data []byte, found bool) string {
	if !found {
		return "none"
	}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(data, &doc) == nil {
		for _, groups := range doc.Hooks {
			for _, g := range groups {
				for _, h := range g.Hooks {
					if strings.Contains(h.Command, lobeHookName) {
						return "ok"
					}
				}
			}
		}
	}
	return "missing"
}

// loginLine is the line that says the lobe's hooks do nothing in this terminal, when CLAUDE_CONFIG_DIR is
// unset or is not the client's own Claude login folder. It is "" otherwise.
func loginLine(d Deps, client string) string {
	want := paths.IdentityDir(client, "claude")
	v := d.Getenv("CLAUDE_CONFIG_DIR")
	if v != "" && pathcmp.SamePath(v, want) {
		return ""
	}
	state := "is unset"
	if v != "" {
		state = "is " + v
	}
	return fmt.Sprintf("CLAUDE_CONFIG_DIR %s: the lobe's hooks do nothing in this terminal. Start Claude Code with CLAUDE_CONFIG_DIR=%s", state, want)
}
