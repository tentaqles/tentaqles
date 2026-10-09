// Package statusline renders the one-line Claude Code status bar tq installs:
// client, git identity, model, context size and session cost. Context size
// gets a colour band so a session that is about to become expensive is
// visible before it happens.
package statusline

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Input is the subset of the JSON Claude Code pipes to a status line command.
// Every field is optional; older Claude Code versions send fewer of them.
type Input struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	Model          struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	Cost struct {
		TotalCostUSD float64 `json:"total_cost_usd"`
	} `json:"cost"`
	ContextWindow *struct {
		ContextWindowSize int      `json:"context_window_size"`
		UsedPercentage    *float64 `json:"used_percentage"`
		CurrentUsage      *usage   `json:"current_usage"`
	} `json:"context_window"`
}

type usage struct {
	InputTokens              int `json:"input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

func (u usage) total() int {
	return u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}

// Facts are what the renderer needs beyond the input.
type Facts struct {
	Client   string // workspace client, "" when neutral
	GitEmail string
}

// Bands are the context sizes (tokens) where the colour changes.
var (
	WarnTokens = 300_000
	HotTokens  = 400_000
)

const (
	reset  = "\x1b[0m"
	dim    = "\x1b[2m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	red    = "\x1b[31m"
	cyan   = "\x1b[36m"
)

// ContextTokens returns the tokens in the current context window: from the
// input when Claude Code provides it, otherwise from the last assistant
// usage in the transcript. 0 means unknown.
func ContextTokens(in Input) int {
	if in.ContextWindow != nil && in.ContextWindow.CurrentUsage != nil {
		if n := in.ContextWindow.CurrentUsage.total(); n > 0 {
			return n
		}
	}
	if in.TranscriptPath != "" {
		return lastUsage(in.TranscriptPath)
	}
	return 0
}

// lastUsage reads at most the last 2 MiB of a transcript and returns the
// context size of the newest assistant message that reports usage.
func lastUsage(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	const tail = 2 << 20
	if fi, err := f.Stat(); err == nil && fi.Size() > tail {
		_, _ = f.Seek(-tail, io.SeekEnd)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	last := 0
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"usage"`) {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				Usage *usage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.Type != "assistant" || rec.Message.Usage == nil {
			continue
		}
		if n := rec.Message.Usage.total(); n > 0 {
			last = n
		}
	}
	return last
}

// Render builds the status line.
func Render(in Input, f Facts, color bool) string {
	c := func(code, s string) string {
		if !color {
			return s
		}
		return code + s + reset
	}
	var parts []string
	client := f.Client
	if client == "" {
		client = "no workspace"
	}
	parts = append(parts, c(cyan, client))
	if f.GitEmail != "" {
		parts = append(parts, c(dim, f.GitEmail))
	}
	if m := in.Model.DisplayName; m != "" {
		parts = append(parts, m)
	} else if in.Model.ID != "" {
		parts = append(parts, in.Model.ID)
	}
	if n := ContextTokens(in); n > 0 {
		label := fmt.Sprintf("ctx %dk", (n+500)/1000)
		if in.ContextWindow != nil && in.ContextWindow.ContextWindowSize > 0 {
			label += fmt.Sprintf(" (%d%%)", n*100/in.ContextWindow.ContextWindowSize)
		}
		switch {
		case n >= HotTokens:
			parts = append(parts, c(red, label+" — compact or wrap up"))
		case n >= WarnTokens:
			parts = append(parts, c(yellow, label))
		default:
			parts = append(parts, c(green, label))
		}
	}
	if in.Cost.TotalCostUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", in.Cost.TotalCostUSD))
	}
	return strings.Join(parts, c(dim, " · "))
}
