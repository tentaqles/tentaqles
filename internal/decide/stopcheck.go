package decide

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// Completion-evidence check ("Belay") for the Stop hook. When a turn edited
// files and the final message claims the work is verified (tests, build or
// lint pass), Jev is asked whether the transcript actually shows such a
// command succeeding after the last edit. In enforce mode a claim without
// evidence sends Claude back once to verify, or to say plainly that the
// change is unverified. It never approves anything.

// StopCommand is one shell command run after the last edit.
type StopCommand struct {
	Tool    string `json:"tool"`
	Command string `json:"command"`
	// Status is "ok", "error" (tool_result is_error) or "unknown" (no
	// result recorded in the transcript tail).
	Status string `json:"status"`
}

// StopState is the compact view of the last turn that Jev judges.
type StopState struct {
	Request  string        `json:"last_user_request"`
	Edited   []string      `json:"files_edited"`
	Commands []StopCommand `json:"commands_after_last_edit"`
	Final    string        `json:"final_assistant_message"`
}

// HasEdits reports whether the turn edited any file.
func (s StopState) HasEdits() bool { return len(s.Edited) > 0 }

const (
	// stopTailBytes bounds how much of the transcript is read.
	stopTailBytes   = 2 << 20
	maxStopRequest  = 2000
	maxStopFinal    = 4000
	maxStopCommand  = 400
	maxStopEdited   = 30
	maxStopCommands = 15
)

var stopEditTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

type transcriptEntry struct {
	Type        string `json:"type"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type transcriptBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
}

func (e transcriptEntry) blocks() []transcriptBlock {
	raw := bytes.TrimSpace(e.Message.Content)
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil
		}
		return []transcriptBlock{{Type: "text", Text: s}}
	}
	var bs []transcriptBlock
	_ = json.Unmarshal(raw, &bs)
	return bs
}

// isUserRequest reports whether a user entry is a message the user typed
// (as opposed to a tool result or injected meta content).
func (e transcriptEntry) isUserRequest() (string, bool) {
	if e.Type != "user" || e.IsMeta || e.IsSidechain {
		return "", false
	}
	var text []string
	for _, b := range e.blocks() {
		switch b.Type {
		case "tool_result":
			return "", false
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				text = append(text, t)
			}
		}
	}
	if len(text) == 0 {
		return "", false
	}
	return strings.Join(text, "\n"), true
}

// ReadStopState reads the transcript tail and builds the state for the turn
// that started with the last user message. A missing or unreadable
// transcript is an error; the caller then does nothing.
func ReadStopState(transcriptPath string) (StopState, error) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return StopState{}, err
	}
	defer f.Close()
	seeked := false
	if fi, err := f.Stat(); err == nil && fi.Size() > stopTailBytes {
		if _, err := f.Seek(-stopTailBytes, io.SeekEnd); err == nil {
			seeked = true
		}
	}
	raw, err := io.ReadAll(io.LimitReader(f, stopTailBytes))
	if err != nil {
		return StopState{}, err
	}
	lines := bytes.Split(raw, []byte("\n"))
	if seeked && len(lines) > 0 {
		lines = lines[1:] // the first line is cut mid-record
	}
	var entries []transcriptEntry
	for _, l := range lines {
		l = bytes.TrimSpace(l)
		if len(l) == 0 {
			continue
		}
		var e transcriptEntry
		if json.Unmarshal(l, &e) == nil && !e.IsSidechain {
			entries = append(entries, e)
		}
	}
	return buildStopState(entries), nil
}

func buildStopState(entries []transcriptEntry) StopState {
	var st StopState
	start := 0
	for i := len(entries) - 1; i >= 0; i-- {
		if text, ok := entries[i].isUserRequest(); ok {
			st.Request = clipHead(text, maxStopRequest)
			start = i + 1
			break
		}
	}
	type pending struct {
		id string
		StopCommand
	}
	var cmds []pending
	status := map[string]string{}
	seen := map[string]bool{}
	for _, e := range entries[start:] {
		for _, b := range e.blocks() {
			switch {
			case e.Type == "assistant" && b.Type == "tool_use" && stopEditTools[b.Name]:
				var in struct {
					FilePath     string `json:"file_path"`
					NotebookPath string `json:"notebook_path"`
				}
				_ = json.Unmarshal(b.Input, &in)
				p := in.FilePath
				if p == "" {
					p = in.NotebookPath
				}
				if p == "" {
					p = "(unknown file)"
				}
				if !seen[p] && len(st.Edited) < maxStopEdited {
					seen[p] = true
					st.Edited = append(st.Edited, p)
				}
				cmds = nil // only commands after the last edit count
			case e.Type == "assistant" && b.Type == "tool_use" && (b.Name == "Bash" || b.Name == "PowerShell"):
				var in struct {
					Command string `json:"command"`
				}
				_ = json.Unmarshal(b.Input, &in)
				cmds = append(cmds, pending{id: b.ID, StopCommand: StopCommand{Tool: b.Name, Command: clipHead(strings.TrimSpace(in.Command), maxStopCommand)}})
			case e.Type == "user" && b.Type == "tool_result" && b.ToolUseID != "":
				if b.IsError {
					status[b.ToolUseID] = "error"
				} else {
					status[b.ToolUseID] = "ok"
				}
			}
		}
		if e.Type == "assistant" {
			var text []string
			for _, b := range e.blocks() {
				if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
					text = append(text, strings.TrimSpace(b.Text))
				}
			}
			if len(text) > 0 {
				st.Final = clipTail(strings.Join(text, "\n"), maxStopFinal)
			}
		}
	}
	if len(cmds) > maxStopCommands {
		cmds = cmds[len(cmds)-maxStopCommands:]
	}
	for _, c := range cmds {
		c.Status = status[c.id]
		if c.Status == "" {
			c.Status = "unknown"
		}
		st.Commands = append(st.Commands, c.StopCommand)
	}
	return st
}

// StopQuestions are the noul questions asked about a StopState. The
// evidence question is skipped when no command ran after the last edit:
// there is nothing for it to find.
func StopQuestions(withEvidence bool) map[string]Question {
	qs := map[string]Question{
		"claims": Noul("Does the final assistant message claim that tests, a build, or a linter passed, or that the change is verified or confirmed working?"),
	}
	if withEvidence {
		qs["evidence"] = Noul("Do the commands run after the last edit include a test, build, lint or other verification command that completed successfully (status ok), supporting that claim?")
	}
	return qs
}

// StopVerdict is the outcome of the completion-evidence check.
type StopVerdict struct {
	Claims   float64
	Evidence float64
	// Would is true when the final message claims verification that the
	// transcript does not show; Apply is Would in enforce mode.
	Would bool
	Apply bool
	Err   error
}

// CheckStop asks Jev about st. It never returns an error: a failed call is
// a verdict with Err set that applies nothing.
//
// A claim counts at or above the block threshold; missing evidence means a
// yes-probability at or below 1-block (0.2 by default), or no command at
// all after the last edit. Both bars are high so that a block is rare.
func CheckStop(ctx context.Context, c *Client, p Policy, st StopState) StopVerdict {
	withEvidence := len(st.Commands) > 0
	resp, err := c.Ask(ctx, st, StopQuestions(withEvidence))
	var v StopVerdict
	if err != nil {
		v.Err = err
		return v
	}
	if v.Claims, err = resp.NoulOf("claims"); err != nil {
		v.Err = err
		return v
	}
	if withEvidence {
		if v.Evidence, err = resp.NoulOf("evidence"); err != nil {
			v.Err = err
			return v
		}
	}
	block, _ := p.Thresholds()
	v.Would = v.Claims >= block && v.Evidence <= 1-block+1e-9
	v.Apply = v.Would && p.Enforcing()
	return v
}

// clipHead keeps the first n bytes of s (on a rune boundary).
func clipHead(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// clipTail keeps the last n bytes of s (on a rune boundary): a final
// message's claims are usually at its end.
func clipTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := len(s) - n
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return "…" + s[cut:]
}
