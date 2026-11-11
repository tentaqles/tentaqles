package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strconv"

	"github.com/tentaqles/tentaqles/internal/lobe/pathcmp"
)

// codexSessionTail is the most bytes at the end of a Codex session file that codexExit reads.
const codexSessionTail = 1 << 20

// codexExit returns the exit code of the shell call callID, read from Codex's session file at path.
// The path must pass the same checks a Claude transcript passes (transcriptAllowed, with the login's
// "sessions" folder as the place the file must lie in), and the file opened must be the one those checks
// saw. Only the last 1 MiB of the file is read. The code is the exit_code of the last line that is a JSON
// object of type event_msg whose payload is an item_completed of a CommandExecution item with id callID,
// and whose exit_code is a whole number. ok is false when callID is empty, the path is not allowed, the
// file cannot be read, or no such line is in the window. It never retries, never waits and reads nothing
// but this one file. It returns only the code.
func codexExit(login, path, callID string) (code int, ok bool) {
	if callID == "" {
		return 0, false
	}
	want, allowed := transcriptAllowed(login, "sessions", path)
	if !allowed {
		return 0, false
	}
	f, err := os.Open(pathcmp.AbsPath(path))
	if err != nil {
		return 0, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !os.SameFile(want, fi) {
		// The file opened is not the one the path check saw: it changed between the two.
		return 0, false
	}
	size := fi.Size()
	n := min(size, codexSessionTail)
	start := size - n
	buf := make([]byte, n)
	if _, err := io.ReadFull(io.NewSectionReader(f, start, n), buf); err != nil {
		return 0, false
	}
	lines := bytes.Split(buf, []byte{'\n'})
	if start > 0 {
		// The window starts in the middle of a line unless the byte before it is a line break. Then the
		// first piece is the tail of a line, and it is dropped.
		before := make([]byte, 1)
		if _, err := f.ReadAt(before, start-1); err != nil {
			return 0, false
		}
		if before[0] != '\n' {
			lines = lines[1:]
		}
	}
	// The last qualifying line wins, so the scan runs from the end and stops at the first match.
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSuffix(lines[i], []byte{'\r'})
		if !bytes.Contains(line, []byte(callID)) {
			continue
		}
		if c, found := codexLineExit(line, callID); found {
			return c, true
		}
	}
	return 0, false
}

// codexLineExit reads one line of a Codex session file. It gives the line's exit_code when the line is
// the shell item of callID (see codexExit) and its exit_code is a whole number: a JSON number with no
// fraction or exponent that fits in an int. A string, a fraction, null or a missing exit_code gives
// false, as does a line that is not a JSON object or any other item.
func codexLineExit(line []byte, callID string) (int, bool) {
	var rec struct {
		Type    string `json:"type"`
		Payload struct {
			Type string `json:"type"`
			Item struct {
				Type     string          `json:"type"`
				ID       string          `json:"id"`
				ExitCode json.RawMessage `json:"exit_code"`
			} `json:"item"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &rec) != nil {
		return 0, false
	}
	if rec.Type != "event_msg" || rec.Payload.Type != "item_completed" ||
		rec.Payload.Item.Type != "CommandExecution" || rec.Payload.Item.ID != callID {
		return 0, false
	}
	// strconv.Atoi takes only an unquoted base-10 integer that fits in an int; the raw bytes of a JSON
	// string, fraction, exponent or null all fail it.
	code, err := strconv.Atoi(string(rec.Payload.Item.ExitCode))
	if err != nil {
		return 0, false
	}
	return code, true
}
