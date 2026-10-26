package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/insights"
)

// Claude runs `claude -p --output-format stream-json --verbose` headless and reads its events as
// they arrive (see stream). Args is tq run's permission-mode wrapper (claudeArgs): the manifest's
// mode, bypass only after `tq allow --bypass`; the hub never adds a permission flag of its own.
// ProgressEvery is the least time between two running cost lines (zero: DefaultProgressEvery).
type Claude struct {
	Bin           []string
	Args          func([]string) []string
	Grace         time.Duration
	ProgressEvery time.Duration
}

var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Argv is the command line. The prompt (stdin) and the allowed tools (a --settings file, see
// Start) never travel on it: on Windows claude is a .cmd shim run through cmd.exe, whose
// metacharacters Go's quoting does not neutralise.
func (c Claude) Argv(req RunRequest) []string { return c.argv(req, "") }

func (c Claude) argv(req RunRequest, settingsPath string) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose"}
	if sessionIDRe.MatchString(req.SessionID) {
		// one token: --resume takes an optional value, so a separate element could be read as a flag
		args = append(args, "--resume="+req.SessionID)
	}
	if settingsPath != "" {
		args = append(args, "--settings", settingsPath)
	}
	if c.Args != nil {
		args = c.Args(args)
	}
	bin := c.Bin
	if len(bin) == 0 {
		bin = []string{"claude"}
	}
	return append(append([]string(nil), bin...), args...)
}

// capped keeps at most max bytes (the JSON result); the log still gets everything.
type capped struct {
	max int
	b   bytes.Buffer
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}

func (c Claude) Start(ctx context.Context, req RunRequest, log io.Writer) (RunResult, error) {
	lw := &locked{w: log}
	if req.SessionID != "" && !sessionIDRe.MatchString(req.SessionID) {
		fmt.Fprintln(lw, "tq hub: ignored malformed session id")
	}
	settingsPath := ""
	if len(req.AllowedTools) > 0 {
		dir, err := os.MkdirTemp("", "tq-hub-claude-")
		if err != nil {
			return RunResult{}, err
		}
		defer os.RemoveAll(dir)
		raw, err := json.Marshal(map[string]any{"permissions": map[string]any{"allow": req.AllowedTools}})
		if err != nil {
			return RunResult{}, err
		}
		settingsPath = filepath.Join(dir, "settings.json")
		if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
			return RunResult{}, err
		}
	}
	// The round runs under its own context: reaching the budget cancels it, which terminates the
	// process tree the way a cancel does, while ctx still tells a hub stop or a human cancel apart.
	rctx, stopRound := context.WithCancel(ctx)
	defer stopRound()
	done := make(chan struct{})
	defer close(done)
	cmd := command(rctx, c.argv(req, settingsPath), req, c.Grace, done)
	cmd.Stdin = strings.NewReader(req.Prompt)
	s := newStream(lw, req, c.ProgressEvery, stopRound)
	cmd.Stdout = s
	cmd.Stderr = lw
	if m := req.Limit.Minutes; m > 0 {
		t := time.AfterFunc(time.Duration(m*float64(time.Minute)), func() { s.breachMinutes(m) })
		defer t.Stop()
	}
	runErr := cmd.Run()
	s.flush()
	res := s.outcome()
	code, err := exitCode(ctx, runErr)
	if res.OverBudget != "" && err != nil && ctx.Err() == nil && rctx.Err() != nil {
		code, err = -1, nil // stopped at the budget: a result, not a start failure
	}
	res.ExitCode = code
	return res, err
}

type resultDoc struct {
	Type      string   `json:"type"`
	Subtype   string   `json:"subtype"`
	IsError   bool     `json:"is_error"`
	Result    string   `json:"result"`
	SessionID string   `json:"session_id"`
	TotalCost *float64 `json:"total_cost_usd"`
	NumTurns  int      `json:"num_turns"`
	Model     string   `json:"model"`
	Usage     *struct {
		Input      int64 `json:"input_tokens"`
		Output     int64 `json:"output_tokens"`
		CacheRead  int64 `json:"cache_read_input_tokens"`
		CacheWrite int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	ModelUsage        map[string]json.RawMessage `json:"modelUsage"`
	PermissionDenials []PermissionDenial         `json:"permission_denials"`
}

// ParseResult reads the result document (the whole output, or its last JSON line). Missing cost
// is computed with insights.CostUSD from the usage and the model named in the output.
func ParseResult(out []byte) RunResult {
	var doc resultDoc
	ok := json.Unmarshal(bytes.TrimSpace(out), &doc) == nil && doc.Type == "result"
	if !ok {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			var d resultDoc
			if json.Unmarshal([]byte(strings.TrimSpace(lines[i])), &d) == nil && d.Type == "result" {
				doc, ok = d, true
				break
			}
		}
	}
	if !ok {
		t := string(out)
		if len(t) > 4000 {
			t = t[len(t)-4000:]
		}
		return RunResult{StopReason: "unparseable output", Transcript: t}
	}
	res := RunResult{SessionID: doc.SessionID, StopReason: doc.Subtype, Transcript: doc.Result, NumTurns: doc.NumTurns,
		PermissionDenials: doc.PermissionDenials}
	if doc.Usage != nil {
		res.Usage = Usage{doc.Usage.Input, doc.Usage.Output, doc.Usage.CacheRead, doc.Usage.CacheWrite}
		res.UsageKnown = true
	}
	res.Model = doc.Model
	if res.Model == "" && len(doc.ModelUsage) > 0 {
		models := make([]string, 0, len(doc.ModelUsage))
		for m := range doc.ModelUsage {
			models = append(models, m)
		}
		sort.Strings(models)
		res.Model = models[0] // several models (subagents): the whole usage is priced at the first; an estimate
	}
	if doc.IsError && res.StopReason == "success" {
		res.StopReason = "error"
	}
	switch {
	case doc.TotalCost != nil:
		res.CostUSD, res.CostKnown = *doc.TotalCost, true
	case res.UsageKnown && res.Model != "":
		if _, priced := insights.LookupPrice(res.Model); priced {
			res.CostUSD = insights.CostUSD(res.Model, insights.Usage{Input: res.Usage.Input, Output: res.Usage.Output,
				CacheRead: res.Usage.CacheRead, CacheWrite: res.Usage.CacheWrite})
			res.CostKnown = true
		}
	}
	return res
}

// ToolSpec is the permissions.allow entry (see Start's --settings file) that allows exactly what was refused.
func ToolSpec(d PermissionDenial) string {
	if d.Tool == "Bash" {
		if cmd, ok := d.Input["command"].(string); ok && strings.TrimSpace(cmd) != "" {
			return fmt.Sprintf("Bash(%s)", strings.TrimSpace(cmd))
		}
	}
	return d.Tool
}
