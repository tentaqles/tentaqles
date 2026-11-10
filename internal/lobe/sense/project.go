package sense

import (
	"bytes"
	"encoding/json"
	"strings"
)

// projectCut is the length, in Unicode code points, that a string value of an unknown tool is cut to.
const projectCut = 200

// projectFields is the table of step 8 of the successes feature: the fields a known tool keeps. Each
// listed field the call carries is kept with its value unchanged; a field the call lacks is left out.
var projectFields = map[string][]string{
	"Bash":         {"command"},
	"PowerShell":   {"command"},
	"Edit":         {"file_path", "notebook_path"},
	"Write":        {"file_path", "notebook_path"},
	"MultiEdit":    {"file_path", "notebook_path"},
	"NotebookEdit": {"file_path", "notebook_path"},
	"Read":         {"file_path", "notebook_path"},
	"Grep":         {"pattern", "path"},
	"Glob":         {"pattern", "path"},
	"WebFetch":     {"url", "query"},
	"WebSearch":    {"url", "query"},
}

// Project returns the input of a successful tool call as the event keeps it: a compact JSON object with
// sorted keys. The input goes through NormalInput first; an input that is not a JSON object gives `{}`.
// A known tool keeps the fields of projectFields that the call carries. Any other tool, MCP tools
// included, and a known tool that carries none of its fields keep every top-level key: a string value is
// cut to projectCut code points, a nested object becomes `{}`, a nested array `[]`, and a number, a
// boolean or null is kept.
func Project(tool string, input json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(NormalInput(input), &m); err != nil || m == nil {
		return json.RawMessage("{}")
	}
	out := make(map[string]json.RawMessage)
	for _, k := range projectFields[tool] {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		for k, v := range m {
			out[k] = projectValue(v)
		}
	}
	raw, err := encodeNoEscape(out)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}

// projectValue is the value of one top-level key of an unknown tool's input, as the projection keeps it.
func projectValue(v json.RawMessage) json.RawMessage {
	t := bytes.TrimSpace(v)
	if len(t) == 0 {
		return json.RawMessage("null")
	}
	switch t[0] {
	case '"':
		var s string
		if err := json.Unmarshal(t, &s); err != nil {
			return json.RawMessage(`""`)
		}
		if r := []rune(s); len(r) > projectCut {
			s = string(r[:projectCut])
		}
		raw, err := encodeNoEscape(s)
		if err != nil {
			return json.RawMessage(`""`)
		}
		return raw
	case '{':
		return json.RawMessage("{}")
	case '[':
		return json.RawMessage("[]")
	}
	return v
}

// encodeNoEscape is the compact JSON of v with &, < and > written as they are, the form the event
// log uses for input. The encoder's trailing newline is dropped.
func encodeNoEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(strings.TrimSuffix(buf.String(), "\n")), nil
}
