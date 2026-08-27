package bundle

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
)

// SyncMCP rewrites <dir>/.claude.json's mcpServers map so that it contains
// exactly the servers in d.MCP replacing what st previously owned: entries
// st owned but which are no longer desired are removed, desired entries
// whose current value differs from the desired deep copy are (re)written,
// and every other key in the file (and every other mcpServers entry not
// owned by st) is left untouched.
//
// The file is only rewritten (via WriteJSONAtomic) if something actually
// changed; an up-to-date file is left with its original mtime. changed
// contains desired names actually written, plus "-"+name for removed
// names, sorted.
func SyncMCP(dir string, d Desired, st *State) ([]string, error) {
	path := filepath.Join(dir, ".claude.json")

	for name := range d.MCP {
		if !validName(name) {
			return nil, fmt.Errorf("invalid mcp server name: %q", name)
		}
	}
	for _, name := range st.MCP {
		if !validName(name) {
			return nil, fmt.Errorf("invalid mcp server name: %q", name)
		}
	}

	root, err := ReadJSONMap(path)
	if err != nil {
		return nil, err
	}

	var servers map[string]any
	if raw, ok := root["mcpServers"]; ok {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("mcpServers is not a JSON object")
		}
		servers = m
	} else {
		servers = map[string]any{}
	}

	var changed []string

	for _, name := range st.MCP {
		if _, ok := d.MCP[name]; ok {
			continue
		}
		if _, exists := servers[name]; exists {
			delete(servers, name)
		}
		changed = append(changed, "-"+name)
	}

	names := make([]string, 0, len(d.MCP))
	for name := range d.MCP {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		desired := deepCopy(d.MCP[name])
		if reflect.DeepEqual(servers[name], desired) {
			continue
		}
		servers[name] = desired
		changed = append(changed, name)
	}
	sort.Strings(changed)

	if len(changed) > 0 {
		root["mcpServers"] = servers
		if err := WriteJSONAtomic(path, root); err != nil {
			return nil, err
		}
	}

	st.MCP = names

	return changed, nil
}
