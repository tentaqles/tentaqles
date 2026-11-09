package sense

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
)

// maxCallRunes is the longest call text a rule is matched against.
const maxCallRunes = 4000

// CallText is the text of a call that rules are matched against: for a shell tool the command
// string, for any other tool the compact JSON of the tool input with sorted keys. It is scrubbed
// and at most 4000 characters long.
func CallText(tool string, input json.RawMessage) string {
	var text string
	if ShellTool(tool) {
		text = scrub.Text(shellCommand(NormalInput(input))).String()
	} else {
		text = scrub.Input(NormalInput(input)).String()
	}
	if utf8.RuneCountInString(text) > maxCallRunes {
		text = string([]rune(text)[:maxCallRunes])
	}
	return text
}

// shellCommand is the "command" string of a shell tool's input, or "" when it has none.
func shellCommand(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	var cmd string
	if err := json.Unmarshal(m["command"], &cmd); err != nil {
		return ""
	}
	return cmd
}
