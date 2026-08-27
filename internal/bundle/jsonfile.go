package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// deepCopy recursively clones maps and slices so the result shares no
// mutable state with v. Scalars are returned as-is.
func deepCopy(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			out[k] = deepCopy(vv)
		}
		return out
	case MCPServer:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			out[k] = deepCopy(vv)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, vv := range val {
			out[i] = deepCopy(vv)
		}
		return out
	default:
		return v
	}
}

// ReadJSONMap reads path as a JSON object. A missing file yields an empty
// map, not an error.
func ReadJSONMap(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// WriteJSONAtomic writes m to path as indented JSON via a temp file + rename,
// so readers never observe a partially-written file.
func WriteJSONAtomic(path string, m map[string]any) error {
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}

	tmp := fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}

	if err := os.Rename(tmp, path); err != nil {
		if errors.Is(err, os.ErrExist) || os.IsExist(err) {
			if rmErr := os.Remove(path); rmErr != nil {
				os.Remove(tmp)
				return rmErr
			}
			if err := os.Rename(tmp, path); err != nil {
				os.Remove(tmp)
				return err
			}
			return nil
		}
		os.Remove(tmp)
		return err
	}
	return nil
}

// normalizeJSON round-trips v through JSON so that values originating from
// YAML (int, int64, map[string]any with typed scalars) compare equal via
// reflect.DeepEqual to the same values read back from a JSON file (float64,
// map[string]any). Returns v unchanged if it can't be round-tripped.
func normalizeJSON(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return out
}
