package sense

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
)

// maxRejectedLine is the longest transcript line Rejected reads; a longer line is skipped.
const maxRejectedLine = 1 << 20

// maxRejected is the most calls RejectedWithContext returns for one chunk.
const maxRejected = 200

// rejectedOpen opens the text of a result that Claude Code rejected before the call ran.
const rejectedOpen = "<tool_use_error>"

// rejectedClose closes that text.
const rejectedClose = "</tool_use_error>"

// toolUse is a tool_use block of the transcript: its tool name and its input.
type toolUse struct {
	name  string
	input json.RawMessage
}

// rejection is a counted rejected result, before its tool is looked up. end is the chunk offset just
// after the line that holds the result.
type rejection struct {
	callID string
	err    string
	end    int
}

// block holds the fields of a content element that Rejected reads. Each field stays raw, so a
// field of the wrong JSON type is found out by the reader, not by the decoder.
type block struct {
	Type      json.RawMessage `json:"type"`
	ID        json.RawMessage `json:"id"`
	Name      json.RawMessage `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	IsError   json.RawMessage `json:"is_error"`
	ToolUseID json.RawMessage `json:"tool_use_id"`
}

// Rejected returns the calls in a Claude Code transcript chunk that Claude Code rejected before
// they ran. It is RejectedWithContext with no look-back.
func Rejected(data []byte) []Call {
	calls, _ := RejectedWithContext(nil, data)
	return calls
}

// RejectedWithContext returns the calls in chunk that Claude Code rejected before they ran. Such a
// call fires no hook; its only trace is a tool_result with is_error true whose text starts with
// <tool_use_error>. chunk is JSON Lines. A line that is empty, longer than 1 MiB or not a JSON
// object is skipped.
//
// prev is the bytes just before chunk, up to 1 MiB, so a tool_use that sits before the chunk still
// names its call. prev is cut to start after its first line break, so only whole lines are read,
// and prev is used for tool_use blocks only: a rejected result in prev is never counted. Each call
// takes its tool and input from the tool_use with the same id in prev or chunk, or "unknown" and {}
// when there is none. A tool_use_id counts once, at its first rejected result. At most 200 calls are
// returned, in file order.
//
// resume is the chunk offset just after the line that holds the last returned call when more than
// 200 calls were found, so the next run can read the rest; it is -1 otherwise. A 201st call on the
// same line as the 200th is not reached by that offset.
func RejectedWithContext(prev, chunk []byte) (calls []Call, resume int) {
	uses := map[string]toolUse{}
	forEachBlock(cutToWholeLines(prev), func(_ int, b block) {
		recordUse(uses, b)
	})
	var found []rejection
	seen := map[string]bool{}
	forEachBlock(chunk, func(end int, b block) {
		switch typ, _ := jsonString(b.Type); typ {
		case "tool_use":
			recordUse(uses, b)
		case "tool_result":
			if !isTrue(b.IsError) {
				return
			}
			id, ok := jsonString(b.ToolUseID)
			if !ok || id == "" || seen[id] {
				return
			}
			text, ok := resultText(b.Content)
			if !ok {
				return
			}
			errText, ok := rejectionText(text)
			if !ok {
				return
			}
			seen[id] = true
			found = append(found, rejection{callID: id, err: errText, end: end})
		}
	})
	resume = -1
	if len(found) > maxRejected {
		resume = found[maxRejected-1].end
		found = found[:maxRejected]
	}
	for _, r := range found {
		c := Call{Tool: unknownTool, CallID: r.callID, Input: json.RawMessage("{}"), Error: r.err}
		if u, ok := uses[r.callID]; ok {
			if u.name != "" {
				c.Tool = u.name
			}
			if len(bytes.TrimSpace(u.input)) > 0 {
				c.Input = u.input
			}
		}
		calls = append(calls, c)
	}
	return calls, resume
}

// cutToWholeLines returns data after its first line break, so a line cut at its start is dropped.
// Data with no line break is one partial line, and gives nil.
func cutToWholeLines(data []byte) []byte {
	i := bytes.IndexByte(data, '\n')
	if i < 0 {
		return nil
	}
	return data[i+1:]
}

// forEachBlock calls fn for every content block of every line of data, in order. end is the offset
// just after the line in data. A line that is empty, longer than 1 MiB or not a JSON object with a
// message.content list is skipped, and so is a block that does not decode.
func forEachBlock(data []byte, fn func(end int, b block)) {
	for pos := 0; pos < len(data); {
		line, next := data[pos:], len(data)
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line, next = line[:i], pos+i+1
		}
		pos = next
		if len(bytes.TrimSpace(line)) == 0 || len(line) > maxRejectedLine {
			continue
		}
		for _, raw := range contentOf(line) {
			var b block
			if json.Unmarshal(raw, &b) != nil {
				continue
			}
			fn(next, b)
		}
	}
}

// recordUse notes the tool_use block b, if it is one, under its id.
func recordUse(uses map[string]toolUse, b block) {
	if typ, _ := jsonString(b.Type); typ != "tool_use" {
		return
	}
	id, ok := jsonString(b.ID)
	if !ok || id == "" {
		return
	}
	name, _ := jsonString(b.Name)
	uses[id] = toolUse{name: name, input: b.Input}
}

// contentOf returns the elements of message.content when line is a JSON object whose content is
// a JSON array. Any other line gives nil.
func contentOf(line []byte) []json.RawMessage {
	t := bytes.TrimSpace(line)
	if len(t) == 0 || t[0] != '{' {
		return nil
	}
	var obj struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(t, &obj) != nil {
		return nil
	}
	c := bytes.TrimSpace(obj.Message.Content)
	if len(c) == 0 || c[0] != '[' {
		return nil
	}
	var parts []json.RawMessage
	if json.Unmarshal(c, &parts) != nil {
		return nil
	}
	return parts
}

// jsonString returns the value of raw when it is a JSON string.
func jsonString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// isTrue reports whether raw is the JSON literal true.
func isTrue(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("true"))
}

// resultText returns the text of a tool_result's content: the value when it is a JSON string, or
// the text parts of a list joined by line breaks. Any other content gives false.
func resultText(raw json.RawMessage) (string, bool) {
	c := bytes.TrimSpace(raw)
	if len(c) == 0 {
		return "", false
	}
	switch c[0] {
	case '"':
		return jsonString(c)
	case '[':
		var parts []json.RawMessage
		if json.Unmarshal(c, &parts) != nil {
			return "", false
		}
		var texts []string
		for _, p := range parts {
			var part struct {
				Text json.RawMessage `json:"text"`
			}
			if json.Unmarshal(p, &part) != nil {
				continue
			}
			if s, ok := jsonString(part.Text); ok {
				texts = append(texts, s)
			}
		}
		return strings.Join(texts, "\n"), true
	}
	return "", false
}

// rejectionText returns the error of a rejected result's text: the text must start, after leading
// space, with <tool_use_error>; the opening tag, one closing tag and the ends' space are removed.
// Text without the opening tag gives false.
func rejectionText(text string) (string, bool) {
	t := strings.TrimLeftFunc(text, unicode.IsSpace)
	if !strings.HasPrefix(t, rejectedOpen) {
		return "", false
	}
	t = strings.TrimPrefix(t, rejectedOpen)
	t = strings.TrimRightFunc(t, unicode.IsSpace)
	t = strings.TrimSuffix(t, rejectedClose)
	return strings.TrimSpace(t), true
}
