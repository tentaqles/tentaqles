package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// maxPayload is the most the hook reads from stdin: 1 MiB. A larger payload is not recorded.
const maxPayload = 1 << 20

// payload is what the hook takes from a host's payload. Each key is read leniently: a key that is
// missing or has the wrong JSON type counts as empty.
type payload struct {
	session    string
	cwd        string
	transcript string
	tool       string
	toolUseID  string
	errText    string
	toolInput  json.RawMessage
	// toolResponse is the payload's tool_response as it arrived, read raw: a PostToolUse success
	// reads it (S-03) and drops it; a failure never reads it.
	toolResponse json.RawMessage
	interrupt    bool
}

// readPayload reads one JSON object from in. It returns false, with nothing read into the result, when
// the input is larger than maxPayload, is not valid UTF-8, or is not exactly one JSON object.
func readPayload(in io.Reader) (payload, bool) {
	if in == nil {
		return payload{}, false
	}
	raw, err := io.ReadAll(io.LimitReader(in, maxPayload+1))
	if err != nil || len(raw) > maxPayload || !utf8.Valid(raw) {
		return payload{}, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var m map[string]json.RawMessage
	if err := dec.Decode(&m); err != nil || m == nil {
		return payload{}, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return payload{}, false
	}
	return payload{
		session:      textKey(m, "session_id"),
		cwd:          textKey(m, "cwd"),
		transcript:   textKey(m, "transcript_path"),
		tool:         textKey(m, "tool_name"),
		toolUseID:    textKey(m, "tool_use_id"),
		errText:      textKey(m, "error"),
		toolInput:    rawKey(m, "tool_input"),
		toolResponse: rawKey(m, "tool_response"),
		interrupt:    boolKey(m, "is_interrupt"),
	}, true
}

// textKey is the string under key, or "" when the key is missing or is not a string.
func textKey(m map[string]json.RawMessage, key string) string {
	var s string
	if err := json.Unmarshal(m[key], &s); err != nil {
		return ""
	}
	return s
}

// boolKey is the boolean under key, or false when the key is missing or is not a boolean.
func boolKey(m map[string]json.RawMessage, key string) bool {
	var b bool
	if err := json.Unmarshal(m[key], &b); err != nil {
		return false
	}
	return b
}

// rawKey is the JSON under key as it arrived, or nil when the key is missing or null.
func rawKey(m map[string]json.RawMessage, key string) json.RawMessage {
	v := bytes.TrimSpace(m[key])
	if len(v) == 0 || string(v) == "null" {
		return nil
	}
	return json.RawMessage(v)
}
