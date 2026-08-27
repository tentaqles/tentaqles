package bundle

import (
	"fmt"
	"path/filepath"
	"sort"
)

// SyncMCP rewrites <dir>/.claude.json's mcpServers map so that it contains
// exactly the servers in st.MCP replaced by d.MCP: entries st previously
// owned but which are no longer desired are removed, desired entries are
// (re)written as deep copies, and every other key in the file (and every
// other mcpServers entry not owned by st) is left untouched.
func SyncMCP(dir string, d Desired, st *State) ([]string, error) {
	path := filepath.Join(dir, ".claude.json")

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

	for _, name := range st.MCP {
		if _, ok := d.MCP[name]; !ok {
			delete(servers, name)
		}
	}

	changed := make([]string, 0, len(d.MCP))
	for name, srv := range d.MCP {
		servers[name] = deepCopy(srv)
		changed = append(changed, name)
	}
	sort.Strings(changed)

	root["mcpServers"] = servers

	if err := WriteJSONAtomic(path, root); err != nil {
		return nil, err
	}

	st.MCP = changed

	return changed, nil
}
