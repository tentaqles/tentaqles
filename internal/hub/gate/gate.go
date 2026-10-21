// Package gate is the hub's client for the plugin's build gate: it reads
// `build-gate state <slug> --json` and, for a human's answer only, runs
// `build-gate answer` with a hub actor token minted exactly as gate.py does.
// The hub never writes gate state and never calls approve/finish/judge/dod.
package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CheckpointID string

func (c *CheckpointID) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	switch {
	case s == "null":
		*c = ""
	case strings.HasPrefix(s, `"`):
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*c = CheckpointID(v)
	default:
		var n json.Number
		if err := json.Unmarshal(b, &n); err != nil {
			return err
		}
		*c = CheckpointID(n.String())
	}
	return nil
}

type Halted struct {
	Reason     string       `json:"reason"`
	Checkpoint CheckpointID `json:"checkpoint"`
}

func (h *Halted) UnmarshalJSON(b []byte) error {
	if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, `"`) {
		return json.Unmarshal(b, &h.Reason)
	}
	type plain Halted
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*h = Halted(p)
	return nil
}

type External struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URL      string `json:"url"`
}

type Task struct {
	Hub       string    `json:"hub"`
	Workspace string    `json:"workspace"`
	Kind      string    `json:"kind"`
	External  *External `json:"external"`
}

type Checkpoint struct {
	ID       CheckpointID      `json:"id"`
	Title    string            `json:"title"`
	Status   string            `json:"status"`
	Gates    map[string]string `json:"gates"`
	Evidence string            `json:"evidence"`
	Reviews  map[string]string `json:"reviews"`
}

type DoD struct {
	OK       bool    `json:"ok"`
	Tree     string  `json:"tree"`
	Override *string `json:"override"`
}

type Decision struct {
	ID         string       `json:"id"`
	Kind       string       `json:"kind"`
	Checkpoint CheckpointID `json:"checkpoint"`
	Reviewer   string       `json:"reviewer"`
	Text       string       `json:"text"`
	Options    []string     `json:"options"`
	Status     string       `json:"status"`
	Choice     string       `json:"choice"`
	Note       string       `json:"note"`
	Actor      string       `json:"actor"`
	AskedAt    string       `json:"asked_at"`
	AnsweredAt string       `json:"answered_at"`
}

type PlanTest struct {
	File  string `json:"file"`
	Layer string `json:"layer"`
}

type PlanCheckpoint struct {
	ID       CheckpointID `json:"id"`
	Title    string       `json:"title"`
	Approved bool         `json:"approved"`
	Tests    []PlanTest   `json:"tests"`
}

type PlanRecord struct {
	Summary      string           `json:"summary"`
	PlainSummary string           `json:"plain_summary"`
	Spec         string           `json:"spec"`
	Checkpoints  []PlanCheckpoint `json:"checkpoints"`
}

type VerifyResult struct {
	Cmd      string  `json:"cmd"`
	Exit     int     `json:"exit"`
	Seconds  float64 `json:"seconds"`
	TimedOut bool    `json:"timed_out"`
}

type Evidence struct {
	Checkpoint CheckpointID   `json:"checkpoint"`
	TS         string         `json:"ts"`
	Tree       string         `json:"tree"`
	OK         bool           `json:"ok"`
	Current    bool           `json:"current"`
	Summary    string         `json:"summary"`
	Results    []VerifyResult `json:"results"`
}

type Review struct {
	Checkpoint CheckpointID `json:"checkpoint"`
	Reviewer   string       `json:"reviewer"`
	Result     string       `json:"result"`
	Tree       string       `json:"tree"`
	TS         string       `json:"ts"`
	Human      bool         `json:"human"`
	Blockers   int          `json:"blockers"`
	Majors     int          `json:"majors"`
	Minors     int          `json:"minors"`
	Unjudged   int          `json:"unjudged"`
}

type DoDCheck struct {
	ID     string `json:"id"`
	OK     *bool  `json:"ok"`
	Detail string `json:"detail"`
}

type DoDRecord struct {
	OK       bool       `json:"ok"`
	Tree     string     `json:"tree"`
	TS       string     `json:"ts"`
	Override *string    `json:"override"`
	Checks   []DoDCheck `json:"checks"`
}

type Records struct {
	Plan     *PlanRecord `json:"plan"`
	Evidence []Evidence  `json:"evidence"`
	Reviews  []Review    `json:"reviews"`
	DoD      *DoDRecord  `json:"dod"`
}

type State struct {
	Slug             string            `json:"slug"`
	Feature          string            `json:"feature"`
	Workflow         string            `json:"workflow"`
	Hotfix           bool              `json:"hotfix"`
	Task             *Task             `json:"task"`
	Approved         bool              `json:"approved"`
	RolledBack       bool              `json:"rolled_back"`
	Finished         bool              `json:"finished"`
	Cleaned          bool              `json:"cleaned"`
	Halted           *Halted           `json:"halted"`
	Checkpoints      []Checkpoint      `json:"checkpoints"`
	DoD              *DoD              `json:"dod"`
	DoDFresh         bool              `json:"dod_fresh"`
	PendingDecisions []Decision        `json:"pending_decisions"`
	Decisions        []Decision        `json:"decisions"`
	Bases            map[string]string `json:"bases"`
	Records          *Records          `json:"records,omitempty"`
	Raw              json.RawMessage   `json:"-"`
}

type API interface {
	State(ctx context.Context, dir, slug string, records bool) (*State, error)
	Answer(ctx context.Context, dir, slug, id, choice, note, actor string) (string, error)
	Slugs(dir string, since time.Time) []string
}

// GateError is a non-zero exit of build-gate.py with its message.
type GateError struct {
	Code int
	Msg  string
}

func (e *GateError) Error() string { return fmt.Sprintf("build-gate exited %d: %s", e.Code, e.Msg) }

func gateMsg(err error) (string, bool) {
	var ge *GateError
	if errors.As(err, &ge) {
		return ge.Msg, true
	}
	return "", false
}

func IsNoPlan(err error) bool {
	m, ok := gateMsg(err)
	return ok && strings.Contains(m, "no plan at")
}

func IsNotPending(err error) bool {
	m, ok := gateMsg(err)
	return ok && strings.Contains(m, "no pending decision")
}

func IsHumanRefusal(err error) bool {
	var ge *GateError
	return errors.As(err, &ge) && ge.Code == 2
}

type Client struct {
	Command []string      // argv prefix that runs build-gate.py
	Env     []string      // the workspace environment (hub credentials are stripped)
	KeyPath string        // "" → KeyPath()
	Timeout time.Duration // per call; 0 → 60 s
}

func scrub(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN":
			continue
		}
		out = append(out, kv)
	}
	return out
}

func (c *Client) run(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	if len(c.Command) == 0 {
		return "", errors.New("build gate: no command")
	}
	t := c.Timeout
	if t <= 0 {
		t = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, t)
	defer cancel()
	argv := append(append([]string(nil), c.Command...), append([]string{"--root", dir}, args...)...)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(scrub(c.Env), extraEnv...)
	cmd.WaitDelay = 2 * time.Second
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("build gate timed out after %s", t)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return "", &GateError{Code: ee.ExitCode(), Msg: strings.TrimSpace(se.String() + "\n" + so.String())}
	}
	if err != nil {
		return "", err
	}
	return so.String(), nil
}

func (c *Client) State(ctx context.Context, dir, slug string, records bool) (*State, error) {
	args := []string{"state", slug, "--json"}
	if records {
		args = append(args, "--records")
	}
	out, err := c.run(ctx, dir, nil, args...)
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		return nil, fmt.Errorf("build-gate state %s: unreadable JSON: %w", slug, err)
	}
	st.Raw = json.RawMessage(strings.TrimSpace(out))
	return &st, nil
}

// Answer reads the key, mints the token for this one call and runs build-gate answer.
func (c *Client) Answer(ctx context.Context, dir, slug, id, choice, note, actor string) (string, error) {
	kp := c.KeyPath
	if kp == "" {
		kp = KeyPath()
	}
	key, err := os.ReadFile(kp)
	if err != nil {
		return "", fmt.Errorf("the gate key %s is not readable (%v); answer this decision in your terminal with build-gate answer", kp, err)
	}
	tok := MintToken(key, "answer", slug, actor)
	for i := range key {
		key[i] = 0
	}
	out, err := c.run(ctx, dir, []string{"TQ_BUILD_ACTOR=" + actor, "TQ_BUILD_TOKEN=" + tok},
		"answer", slug, id, "--choice", choice, "--note", note, "--actor", actor)
	return strings.TrimSpace(out), err
}

func (c *Client) Slugs(dir string, since time.Time) []string { return Slugs(dir, since) }

// Slugs lists <dir>/.claude/build/<slug>/ directories modified at or after since, newest first.
func Slugs(dir string, since time.Time) []string {
	ents, err := os.ReadDir(filepath.Join(dir, ".claude", "build"))
	if err != nil {
		return nil
	}
	type item struct {
		name string
		t    time.Time
	}
	var items []item
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.ModTime().Before(since) {
			continue
		}
		items = append(items, item{e.Name(), fi.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].t.After(items[j].t) })
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.name
	}
	return out
}

// Unavailable stands in when the plugin cannot be found: runs still start; state reads report Err.
type Unavailable struct{ Err error }

func (u Unavailable) State(context.Context, string, string, bool) (*State, error) { return nil, u.Err }
func (u Unavailable) Answer(context.Context, string, string, string, string, string, string) (string, error) {
	return "", u.Err
}
func (u Unavailable) Slugs(string, time.Time) []string { return nil }
