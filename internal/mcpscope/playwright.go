package mcpscope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// The Playwright MCP server keeps its own browser (ADR-0016): it cannot attach to a profile that
// is already open, so tq gives the identity a folder of its own, --user-data-dir under
// $TQ_HOME/identities/<ws>/browser, and sets it on every Playwright entry of the identity's
// Claude config, keeping the entry's other arguments.

// DataDirFlag is the Playwright MCP argument that names the browser's user data folder.
const DataDirFlag = "--user-data-dir"

// PlaywrightEdit is one Playwright MCP entry whose arguments tq set (or, on a dry run, would).
type PlaywrightEdit struct {
	Where string // the config file, and the project for a per-project entry
	Name  string
}

// IsPlaywright reports whether the MCP server name with its config entry is the Playwright MCP
// server: its name, its command or one of its arguments says so.
func IsPlaywright(name string, entry map[string]any) bool {
	if strings.Contains(strings.ToLower(name), "playwright") {
		return true
	}
	if c, ok := entry["command"].(string); ok && strings.Contains(strings.ToLower(filepath.Base(strings.ReplaceAll(c, `\`, "/"))), "playwright") {
		return true
	}
	if args, ok := entry["args"].([]any); ok {
		for _, a := range args {
			if s, ok := a.(string); ok {
				l := strings.ToLower(s)
				if strings.Contains(l, "playwright/mcp") || strings.Contains(l, "playwright-mcp") || strings.Contains(l, "mcp-server-playwright") {
					return true
				}
			}
		}
	}
	return false
}

// withDataDir is args with --user-data-dir set to dir: the first existing one replaced in
// place (either spelling), any other removed, one appended when there was none. The other
// arguments keep their order.
func withDataDir(args []any, dir string) []any {
	out := make([]any, 0, len(args)+2)
	done := false
	for i := 0; i < len(args); i++ {
		s, _ := args[i].(string)
		isFlag := s == DataDirFlag
		isEq := strings.HasPrefix(s, DataDirFlag+"=")
		if !isFlag && !isEq {
			out = append(out, args[i])
			continue
		}
		if isFlag && i+1 < len(args) {
			i++ // its value
		}
		if done {
			continue
		}
		done = true
		out = append(out, DataDirFlag, dir)
	}
	if !done {
		out = append(out, DataDirFlag, dir)
	}
	return out
}

func sameArgs(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if fmt.Sprint(a[i]) != fmt.Sprint(b[i]) {
			return false
		}
	}
	return true
}

// SetPlaywrightDataDir sets --user-data-dir to dir on every Playwright MCP entry of
// claudeDir/.claude.json (the top-level mcpServers and every project's). The file is edited
// member by member, as FilterClaude does: only the entry's args array is rewritten. It writes
// nothing on a dry run or when every entry already holds dir.
func SetPlaywrightDataDir(claudeDir, dir string, dryRun bool) ([]PlaywrightEdit, error) {
	p := filepath.Join(claudeDir, ".claude.json")
	raw, root, err := readClaude(p)
	if err != nil || root == nil {
		return nil, err
	}
	type cut struct {
		path []string
		args []any
	}
	var edits []PlaywrightEdit
	var cuts []cut
	sweep := func(servers map[string]any, where string, path []string) {
		names := make([]string, 0, len(servers))
		for n := range servers {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			e, _ := servers[n].(map[string]any)
			if e == nil || !IsPlaywright(n, e) {
				continue
			}
			if _, isURL := e["url"]; isURL {
				continue // a remote server: no arguments to set
			}
			cur, _ := e["args"].([]any)
			want := withDataDir(cur, dir)
			if sameArgs(cur, want) {
				continue
			}
			edits = append(edits, PlaywrightEdit{Where: where, Name: n})
			cuts = append(cuts, cut{append(append([]string{}, path...), n, "args"), want})
		}
	}
	if s, ok := root["mcpServers"].(map[string]any); ok {
		sweep(s, p, []string{"mcpServers"})
	}
	if projs, ok := root["projects"].(map[string]any); ok {
		keys := make([]string, 0, len(projs))
		for k := range projs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if pr, ok := projs[k].(map[string]any); ok {
				if s, ok := pr["mcpServers"].(map[string]any); ok {
					sweep(s, p+" (project "+k+")", []string{"projects", k, "mcpServers"})
				}
			}
		}
	}
	if len(edits) == 0 || dryRun {
		return edits, nil
	}
	out := raw
	for _, c := range cuts {
		val, err := compactJSON(c.args)
		if err != nil {
			return nil, err
		}
		next, err := setMember(out, c.path, val)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = next
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("%s: the edited file is not valid JSON; nothing written", p)
	}
	if err := writeAtomic(p, out); err != nil {
		return nil, err
	}
	return edits, nil
}

// compactJSON is v as one line of JSON with <, > and & as written.
func compactJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// DataDirOf is the --user-data-dir an entry's args carry, "" when it has none (a test reads it).
func DataDirOf(entry map[string]any) string {
	args, _ := entry["args"].([]any)
	for i, a := range args {
		s, _ := a.(string)
		if s == DataDirFlag && i+1 < len(args) {
			v, _ := args[i+1].(string)
			return v
		}
		if v, ok := strings.CutPrefix(s, DataDirFlag+"="); ok {
			return v
		}
	}
	return ""
}
