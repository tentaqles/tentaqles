package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/insights"
)

// Claude runs `claude -p <prompt> --output-format json` headless. Args is tq run's
// permission-mode wrapper (claudeArgs): the manifest's mode, bypass only after
// `tq allow --bypass`; the hub never adds a permission flag of its own.
type Claude struct {
	Bin   []string
	Args  func([]string) []string
	Grace time.Duration
}

func (c Claude) Argv(req RunRequest) []string {
	args := []string{"-p", req.Prompt, "--output-format", "json"}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	if len(req.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(req.AllowedTools, ","))
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
	cmd := command(ctx, c.Argv(req), req, c.Grace)
	out := &capped{max: 16 << 20}
	lw := &locked{w: log}
	cmd.Stdout = io.MultiWriter(out, lw)
	cmd.Stderr = lw
	code, err := exitCode(ctx, cmd.Run())
	res := ParseResult(out.b.Bytes())
	res.ExitCode = code
	return res, err
}

type resultDoc struct {
	Type      string   `json:"type"`
	Subtype   string   `json:"subtype"`
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

// ToolSpec is the --allowedTools entry that allows exactly what was refused.
func ToolSpec(d PermissionDenial) string {
	if d.Tool == "Bash" {
		if cmd, ok := d.Input["command"].(string); ok && strings.TrimSpace(cmd) != "" {
			return fmt.Sprintf("Bash(%s)", strings.TrimSpace(cmd))
		}
	}
	return d.Tool
}
