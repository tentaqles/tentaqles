// Package codexconfig manages the part of an OpenAI Codex CLI config file
// ($CODEX_HOME/config.toml) that tentaqles owns, and reports whether Codex
// trusts the tentaqles hooks.
//
// Ownership:
//   - A hook handler is tentaqles' when its type is "command" and its command is
//     "<program> codex-hook pre-tool-use" or "<program> codex-hook session-start".
//     Render removes every such handler and appends one group per event after the
//     user's groups, so the group indexes Codex keys trust on stay stable.
//   - developer_instructions is written when it is absent, or when its value equals
//     the value tentaqles last wrote. That value is recorded in tentaqles-owned.json
//     next to config.toml. A value the user wrote is never replaced or removed.
//
// Trust: Codex stores trust as [hooks.state."<key>"] trusted_hash = "sha256:...",
// where the key is "<config path>:<event label>:<group index>:<handler index>" and
// the hash is over the normalized hook (see hookHash). HookTrust only reads trust;
// the user accepts the hooks once in Codex's /hooks screen. The rules were taken
// from codex-rs at rust-v0.154.0 and checked against Codex 0.154.0 (see the golden
// tests).
package codexconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	configFile       = "config.toml"
	ownedFile        = "tentaqles-owned.json"
	devInstrKey      = "developer_instructions"
	hookTimeoutSec   = 15
	codexHookTimeout = 600  // Codex's timeout for a command hook without one
	codexContextLim  = 2500 // Codex's additionalContext token limit; the default is not hashed
)

// Managed is what tentaqles owns in config.toml.
type Managed struct {
	Command               string // the hook program, e.g. "tentaqles"
	DeveloperInstructions string // e.g. the language line; "" removes tentaqles' value
}

// Trust is the state of the tentaqles hooks as Codex sees them.
type Trust int

const (
	Missing   Trust = iota // our hooks are not in the file
	Disabled               // our hooks are off: enabled = false, or hooks/codex_hooks = false under [features]
	Untrusted              // no trusted_hash for our hooks
	Modified               // trusted_hash differs from the current hash
	Trusted                // trusted_hash matches the current hash
)

// String returns the lowercase name of the trust state.
func (t Trust) String() string {
	switch t {
	case Missing:
		return "missing"
	case Disabled:
		return "disabled"
	case Untrusted:
		return "untrusted"
	case Modified:
		return "modified"
	case Trusted:
		return "trusted"
	}
	return fmt.Sprintf("Trust(%d)", int(t))
}

// hookEvent describes one of the two hooks tentaqles writes.
type hookEvent struct {
	table   string // the TOML table under [hooks], e.g. "PreToolUse"
	label   string // Codex's snake_case event label, used in keys and hashes
	sub     string // the codex-hook subcommand
	matcher string // "" writes no matcher
}

var hookEvents = []hookEvent{
	{table: "PreToolUse", label: "pre_tool_use", sub: "pre-tool-use", matcher: "^Bash$"},
	{table: "SessionStart", label: "session_start", sub: "session-start"},
}

// ownedRecord is what tentaqles-owned.json holds.
type ownedRecord struct {
	DeveloperInstructions string `json:"developer_instructions,omitempty"`
}

// Render writes the managed keys into codexHome/config.toml (creating the folder and
// file if needed), keeping every other key the user has. It returns whether the file
// changed. Writing is atomic (temp file + rename). Rendering the same Managed again
// returns changed=false and identical bytes.
//
// A developer_instructions value the user wrote is left alone; Render does not error
// on it, and the managed value is not applied while it is there.
func Render(codexHome string, m Managed) (changed bool, err error) {
	if strings.TrimSpace(m.Command) == "" {
		return false, errors.New("codexconfig: Managed.Command is empty")
	}
	if codexHome == "" {
		return false, errors.New("codexconfig: codexHome is empty")
	}
	path := filepath.Join(codexHome, configFile)
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	root, err := parseConfig(old)
	if err != nil {
		return false, fmt.Errorf("codexconfig: %s: %w", path, err)
	}
	prev, err := readOwned(codexHome)
	if err != nil {
		return false, err
	}

	if err := renderHooks(root, m.Command); err != nil {
		return false, fmt.Errorf("codexconfig: %s: %w", path, err)
	}
	next := renderDeveloperInstructions(root, prev, m.DeveloperInstructions)

	out, err := toml.Marshal(root)
	if err != nil {
		return false, fmt.Errorf("codexconfig: encode %s: %w", path, err)
	}
	if !bytes.Equal(old, out) {
		if err := os.MkdirAll(codexHome, 0o700); err != nil {
			return false, err
		}
		if err := writeAtomic(path, out); err != nil {
			return false, err
		}
		changed = true
	}
	if err := writeOwned(codexHome, next); err != nil {
		return changed, err
	}
	// Trust comes last: the key of each hook is the config path and the group index, and the
	// file must exist (and hold the groups just rendered) before Codex's key can be computed.
	trusted, err := trustHooks(codexHome)
	if err != nil {
		return changed, err
	}
	return changed || trusted, nil
}

// TrustHooks records that Codex trusts tentaqles' hook handlers in codexHome/config.toml: each
// owned handler gets [hooks.state."<key>"] with trusted_hash set to its current hash and
// enabled = true. No other state entry and no other key is touched, and a file without owned
// handlers is left as it is. Render calls it, so rendering the hooks trusts them.
func TrustHooks(codexHome string) error {
	_, err := trustHooks(codexHome)
	return err
}

// trustHooks is TrustHooks, reporting whether it changed the file.
func trustHooks(codexHome string) (bool, error) {
	path := filepath.Join(codexHome, configFile)
	old, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	root, err := parseConfig(old)
	if err != nil {
		return false, fmt.Errorf("codexconfig: %s: %w", path, err)
	}
	keyPath, err := configKeyPath(path)
	if err != nil {
		return false, err
	}
	var stateErr error
	owned := 0
	err = ownedHandlers(root, keyPath, func(ev hookEvent, key string, g, h map[string]any) {
		owned++
		if stateErr != nil {
			return
		}
		state, err := stateTable(root)
		if err != nil {
			stateErr = err
			return
		}
		entry, _ := state[key].(map[string]any)
		if entry == nil {
			entry = map[string]any{}
			state[key] = entry
		}
		entry["trusted_hash"] = hookHash(ev.label, g, h)
		entry["enabled"] = true
	})
	if err == nil {
		err = stateErr
	}
	if err != nil {
		return false, fmt.Errorf("codexconfig: %s: %w", path, err)
	}
	if owned == 0 {
		// Nothing of ours to trust: the file stays byte for byte as the user wrote it.
		return false, nil
	}
	out, err := toml.Marshal(root)
	if err != nil {
		return false, fmt.Errorf("codexconfig: encode %s: %w", path, err)
	}
	if bytes.Equal(old, out) {
		return false, nil
	}
	if err := writeAtomic(path, out); err != nil {
		return false, err
	}
	return true, nil
}

// HookTrust reports the state of the tentaqles hooks in codexHome/config.toml. Both the
// PreToolUse and the SessionStart hook must be trusted for Trusted; when they differ, the
// worst wins, in the order Missing, Disabled, Untrusted, Modified, Trusted. A hook is Disabled
// when its state entry has enabled = false, or when [features] sets hooks or codex_hooks to
// false, which turns off every hook in the file.
func HookTrust(codexHome string) (Trust, error) {
	path := filepath.Join(codexHome, configFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Missing, nil
	}
	if err != nil {
		return Missing, err
	}
	root, err := parseConfig(data)
	if err != nil {
		return Missing, fmt.Errorf("codexconfig: %s: %w", path, err)
	}
	keyPath, err := configKeyPath(path)
	if err != nil {
		return Missing, err
	}
	state := hookState(root)
	off := featuresOff(root)

	worst := Trusted
	found := map[string]bool{}
	err = ownedHandlers(root, keyPath, func(ev hookEvent, key string, g, h map[string]any) {
		found[ev.label] = true
		t := trustOf(state, key, hookHash(ev.label, g, h))
		if off || entryDisabled(state, key) {
			t = Disabled
		}
		if t < worst {
			worst = t
		}
	})
	if err != nil {
		return Missing, fmt.Errorf("codexconfig: %s: %w", path, err)
	}
	for _, ev := range hookEvents {
		if !found[ev.label] {
			return Missing, nil
		}
	}
	return worst, nil
}

// ownedHandlers calls visit for every handler tentaqles owns, with its event, its Codex hook key
// (keyPath, label, group index, handler index), and its group and handler tables.
func ownedHandlers(root map[string]any, keyPath string, visit func(ev hookEvent, key string, g, h map[string]any)) error {
	raw, ok := root["hooks"]
	if !ok {
		return nil
	}
	hooks, ok := raw.(map[string]any)
	if !ok {
		return errors.New(`"hooks" is not a table`)
	}
	for _, ev := range hookEvents {
		groups, err := groupsOf(hooks[ev.table])
		if err != nil {
			return fmt.Errorf("hooks.%s: %w", ev.table, err)
		}
		for gi, g := range groups {
			handlers, err := handlersOf(g)
			if err != nil {
				return fmt.Errorf("hooks.%s: %w", ev.table, err)
			}
			for hi, h := range handlers {
				if isOwnedHandler(h) {
					visit(ev, hookKey(keyPath, ev.label, gi, hi), g, h)
				}
			}
		}
	}
	return nil
}

// stateTable returns root.hooks.state, creating it when absent.
func stateTable(root map[string]any) (map[string]any, error) {
	hooks, err := hooksTable(root)
	if err != nil {
		return nil, err
	}
	v, ok := hooks["state"]
	if !ok {
		t := map[string]any{}
		hooks["state"] = t
		return t, nil
	}
	t, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New(`"hooks.state" is not a table`)
	}
	return t, nil
}

// hookState returns root.hooks.state, or nil when there is none.
func hookState(root map[string]any) map[string]any {
	hooks, _ := root["hooks"].(map[string]any)
	state, _ := hooks["state"].(map[string]any)
	return state
}

// entryDisabled reports whether the state entry for key says enabled = false.
func entryDisabled(state map[string]any, key string) bool {
	entry, _ := state[key].(map[string]any)
	on, isBool := entry["enabled"].(bool)
	return isBool && !on
}

// featuresOff reports whether [features] turns the hooks off: hooks = false or codex_hooks = false.
func featuresOff(root map[string]any) bool {
	features, _ := root["features"].(map[string]any)
	for _, k := range []string{"hooks", "codex_hooks"} {
		if on, isBool := features[k].(bool); isBool && !on {
			return true
		}
	}
	return false
}

// renderHooks replaces tentaqles' handlers in hooks.PreToolUse and hooks.SessionStart
// and appends one group per event after the user's groups.
func renderHooks(root map[string]any, command string) error {
	hooks, err := hooksTable(root)
	if err != nil {
		return err
	}
	for _, ev := range hookEvents {
		groups, err := groupsOf(hooks[ev.table])
		if err != nil {
			return fmt.Errorf("hooks.%s: %w", ev.table, err)
		}
		kept := make([]map[string]any, 0, len(groups)+1)
		for _, g := range groups {
			ng, err := withoutOwned(g)
			if err != nil {
				return fmt.Errorf("hooks.%s: %w", ev.table, err)
			}
			if ng != nil {
				kept = append(kept, ng)
			}
		}
		ours := map[string]any{
			"hooks": []map[string]any{{
				"type":    "command",
				"command": command + " codex-hook " + ev.sub,
				"timeout": int64(hookTimeoutSec),
			}},
		}
		if ev.matcher != "" {
			ours["matcher"] = ev.matcher
		}
		hooks[ev.table] = append(kept, ours)
	}
	return nil
}

// withoutOwned returns the group with tentaqles' handlers removed, as a group whose
// hooks are typed for encoding. It returns nil when nothing is left of it.
func withoutOwned(g map[string]any) (map[string]any, error) {
	handlers, err := handlersOf(g)
	if err != nil {
		return nil, err
	}
	var rest []map[string]any
	for _, h := range handlers {
		if !isOwnedHandler(h) {
			rest = append(rest, h)
		}
	}
	if len(rest) == 0 && len(handlers) > 0 {
		return nil, nil
	}
	ng := maps.Clone(g)
	ng["hooks"] = rest
	if rest == nil {
		ng["hooks"] = []map[string]any{}
	}
	return ng, nil
}

// renderDeveloperInstructions sets or removes the top-level developer_instructions key,
// but only when it is absent or still holds the value tentaqles last wrote. It returns
// the record of what tentaqles now owns.
func renderDeveloperInstructions(root map[string]any, prev ownedRecord, want string) ownedRecord {
	cur, has := root[devInstrKey]
	curStr, isStr := cur.(string)
	ours := has && isStr && prev.DeveloperInstructions != "" && curStr == prev.DeveloperInstructions
	switch {
	case want != "":
		if !has || ours {
			root[devInstrKey] = want
			return ownedRecord{DeveloperInstructions: want}
		}
		return ownedRecord{}
	default:
		if ours {
			delete(root, devInstrKey)
		}
		return ownedRecord{}
	}
}

// isOwnedHandler reports whether a hook handler is one tentaqles writes.
func isOwnedHandler(h map[string]any) bool {
	t, _ := h["type"].(string)
	cmd, ok := h["command"].(string)
	return ok && t == "command" && ownedCommand(cmd)
}

// ownedCommand reports whether cmd is "<program> codex-hook <pre-tool-use|session-start>"
// with a non-empty program. The program may be quoted, as Render writes it when its path has
// spaces ("C:\Program Files\tentaqles\tentaqles.exe" codex-hook pre-tool-use); the quotes are not
// part of the program.
func ownedCommand(cmd string) bool {
	for _, ev := range hookEvents {
		suffix := " codex-hook " + ev.sub
		if !strings.HasSuffix(cmd, suffix) {
			continue
		}
		prog := strings.TrimSpace(strings.TrimSuffix(cmd, suffix))
		if strings.HasPrefix(prog, `"`) {
			if len(prog) > 2 && strings.HasSuffix(prog, `"`) {
				return true
			}
			continue
		}
		if prog != "" {
			return true
		}
	}
	return false
}

// trustOf compares the trusted_hash Codex has stored for key with the current hash.
func trustOf(state map[string]any, key, current string) Trust {
	entry, ok := state[key].(map[string]any)
	if !ok {
		return Untrusted
	}
	trusted, ok := entry["trusted_hash"].(string)
	if !ok {
		return Untrusted
	}
	if trusted == current {
		return Trusted
	}
	return Modified
}

// configKeyPath is the config path Codex puts in its hook keys. Codex resolves the path
// (a CODEX_HOME that is a symlink or a junction keys on the target), so this does too. A
// path that does not resolve keeps its absolute form.
func configKeyPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := resolveFinal(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

// hookKey is Codex's persisted key for a handler: hook_key() in codex-rs/hooks/src/lib.rs.
func hookKey(configPath, label string, groupIndex, handlerIndex int) string {
	return fmt.Sprintf("%s:%s:%d:%d", configPath, label, groupIndex, handlerIndex)
}

// hookHash is the trust hash of one command hook: hook_hash() in
// codex-rs/hooks/src/engine/discovery.rs, over the normalized handler, as
// version_for_toml() in codex-rs/config/src/fingerprint.rs computes it: the compact
// JSON of the identity with object keys sorted, hashed with SHA-256.
//
// The identity is {event_name, matcher?, hooks:[handler]}. Codex normalizes the handler
// first: on Windows commandWindows replaces command, a missing timeout is 600 and a
// timeout below 1 is 1, async defaults to false (and is always hashed), statusMessage is
// hashed when set, and additionalContextLimit is hashed when set and not the 2500 default.
func hookHash(label string, group, handler map[string]any) string {
	command, _ := handler["command"].(string)
	if runtime.GOOS == "windows" {
		for _, k := range []string{"commandWindows", "command_windows"} {
			if cw, ok := handler[k].(string); ok {
				command = cw
				break
			}
		}
	}
	timeout := int64(codexHookTimeout)
	if t, ok := handler["timeout"].(int64); ok {
		timeout = max(t, 1)
	}
	async, _ := handler["async"].(bool)

	h := map[string]any{
		"type":    "command",
		"command": command,
		"timeout": timeout,
		"async":   async,
	}
	if s, ok := handler["statusMessage"].(string); ok {
		h["statusMessage"] = s
	}
	if n, ok := handler["additionalContextLimit"].(int64); ok && n != codexContextLim {
		h["additionalContextLimit"] = n
	}
	identity := map[string]any{
		"event_name": label,
		"hooks":      []any{h},
	}
	if m, ok := group["matcher"].(string); ok {
		identity["matcher"] = m
	}
	return sha256Canonical(identity)
}

// sha256Canonical returns "sha256:<hex>" over the canonical JSON of v.
func sha256Canonical(v map[string]any) string {
	var sb strings.Builder
	writeCanonical(&sb, v)
	sum := sha256.Sum256([]byte(sb.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// writeCanonical writes v as compact JSON with sorted object keys, escaping strings the
// way serde_json does. It handles the value types the TOML decoder and this package
// produce.
func writeCanonical(sb *strings.Builder, v any) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeJSONString(sb, k)
			sb.WriteByte(':')
			writeCanonical(sb, x[k])
		}
		sb.WriteByte('}')
	case []any:
		sb.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeCanonical(sb, e)
		}
		sb.WriteByte(']')
	case []map[string]any:
		sb.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeCanonical(sb, e)
		}
		sb.WriteByte(']')
	case string:
		writeJSONString(sb, x)
	case bool:
		sb.WriteString(strconv.FormatBool(x))
	case int64:
		sb.WriteString(strconv.FormatInt(x, 10))
	case int:
		sb.WriteString(strconv.Itoa(x))
	default:
		panic(fmt.Sprintf("codexconfig: unsupported hook identity value %T", v))
	}
}

// writeJSONString writes s as a JSON string with serde_json's escaping: quote, backslash,
// the short escapes, and \u00xx for other control characters. Everything else is literal.
func writeJSONString(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(sb, `\u%04x`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
}

// parseConfig decodes a config file into a generic table. Empty input is an empty table.
func parseConfig(data []byte) (map[string]any, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(data)) == 0 {
		return root, nil
	}
	if err := toml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return root, nil
}

// hooksTable returns root.hooks, creating it when absent.
func hooksTable(root map[string]any) (map[string]any, error) {
	v, ok := root["hooks"]
	if !ok {
		t := map[string]any{}
		root["hooks"] = t
		return t, nil
	}
	t, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New(`"hooks" is not a table`)
	}
	return t, nil
}

// groupsOf converts an event's value to its matcher groups.
func groupsOf(v any) ([]map[string]any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case []map[string]any:
		return x, nil
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, e := range x {
			m, ok := e.(map[string]any)
			if !ok {
				return nil, errors.New("expected an array of tables")
			}
			out = append(out, m)
		}
		return out, nil
	}
	return nil, errors.New("expected an array of tables")
}

// handlersOf converts a group's hooks to its handler tables.
func handlersOf(g map[string]any) ([]map[string]any, error) {
	return groupsOf(g["hooks"])
}

// readOwned reads tentaqles-owned.json. A missing file is an empty record.
func readOwned(codexHome string) (ownedRecord, error) {
	var rec ownedRecord
	data, err := os.ReadFile(filepath.Join(codexHome, ownedFile))
	if errors.Is(err, fs.ErrNotExist) {
		return rec, nil
	}
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return ownedRecord{}, fmt.Errorf("codexconfig: %s: %w", filepath.Join(codexHome, ownedFile), err)
	}
	return rec, nil
}

// writeOwned writes the ownership record, or removes the file when there is nothing owned.
func writeOwned(codexHome string, rec ownedRecord) error {
	path := filepath.Join(codexHome, ownedFile)
	if rec == (ownedRecord{}) {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return err
	}
	return writeAtomic(path, data)
}

// writeAtomic writes data to path through a temp file in the same folder and a rename.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
