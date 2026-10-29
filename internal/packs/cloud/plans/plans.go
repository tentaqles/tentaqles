// Package plans is the cloud pack's plan store (spec D §9.4): the raw plans
// under StateDir()/plans/ (0700 directory, 0600 files), a redacted JSON copy
// the checks read, one small record per plan (plans/<id>.json, which the
// guard reads within its 1 MiB bound), and the append-only plans.jsonl.
// Records hold names, hashes and counts only, never a plan value.
package plans

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// Schema of a record.
const Schema = "tq.cloud.plan/v1"

// File names under the cloud pack's StateDir().
const (
	Dir     = "plans"
	LogFile = "plans.jsonl"
)

// Evidence keys (§9.4), shared by tq pack cloud approve and the guard.
const (
	EvPlanID      = "plan_id"
	EvPlanSHA     = "plan_sha256"
	EvInputs      = "inputs_hash"
	EvEnv         = "env"
	EvIdentity    = "identity"
	EvStack       = "stack"
	EvChangeSet   = "change_set"
	EvTemplate    = "template_sha256"
	EvParams      = "params_sha256"
	EvScope       = "scope"
	EvProject     = "pulumi_project"
	EvStackConfig = "pulumi_stack_config"
)

// Check is one cloud/* result over the plan (names and counts only).
type Check struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
	Errors   int    `json:"errors"`
	Warns    int    `json:"warns"`
}

// Stack is one CDK stack of a synthesised assembly.
type Stack struct {
	Name     string   `json:"name"`              // the artifact id cdk deploy takes
	Aliases  []string `json:"aliases,omitempty"` // stackName, displayName
	Hash     string   `json:"hash"`              // sha256 over manifest.json, the template and the asset manifest
	Template string   `json:"template"`          // file name inside the assembly
	Assets   string   `json:"assets,omitempty"`  // asset manifest file name
}

// Record is one recorded plan.
type Record struct {
	Schema string `json:"schema"`
	ID     string `json:"id"`
	Env    string `json:"env"`
	Tool   string `json:"tool"`           // terraform | tofu | pulumi | az | cdk | aws
	Kind   string `json:"kind,omitempty"` // the model.Kind of the stored JSON
	// Destroy marks a destroy plan (terraform plan -destroy, az --mode Complete).
	Destroy   bool   `json:"destroy,omitempty"`
	Dir       string `json:"dir"`                 // repo-relative slash path of the IaC root
	Subject   string `json:"subject"`             // the approval subject: dir or stack
	Workspace string `json:"workspace,omitempty"` // terraform workspace
	Stack     string `json:"stack,omitempty"`     // pulumi / cfn stack
	// Weak marks Pulumi's fallback binding (no saved update plan).
	Weak       bool          `json:"weak,omitempty"`
	Stacks     []Stack       `json:"stacks,omitempty"` // cdk
	App        string        `json:"app,omitempty"`    // cdk assembly dir, repo-relative
	Scope      string        `json:"scope,omitempty"`  // az: group:<rg> | sub | mg:<id> | tenant
	Template   string        `json:"template,omitempty"`
	TemplateSH string        `json:"template_sha256,omitempty"`
	ParamsSHA  string        `json:"params_sha256,omitempty"`
	ChangeSet  string        `json:"change_set,omitempty"` // cfn change-set ARN
	Project    string        `json:"pulumi_project,omitempty"`
	StackCfg   string        `json:"pulumi_stack_config,omitempty"`
	PlanFile   string        `json:"plan_file"` // plans/<id>.<ext> under StateDir()
	PlanSHA256 string        `json:"plan_sha256"`
	PlanSize   int64         `json:"plan_size"`
	PlanMTime  string        `json:"plan_mtime"`
	Redacted   []string      `json:"redacted,omitempty"` // plans/<id>.redacted.json (cdk: one per stack)
	InputsHash string        `json:"inputs_hash"`
	Inputs     []string      `json:"inputs,omitempty"` // the repo paths inputs_hash covers
	VarFiles   []string      `json:"var_files,omitempty"`
	Identity   string        `json:"identity"`
	Account    string        `json:"account,omitempty"`
	Commit     string        `json:"commit,omitempty"`
	Clean      bool          `json:"clean"`
	Summary    model.Summary `json:"summary"`
	Checks     []Check       `json:"checks"`
	Apply      []string      `json:"apply,omitempty"` // the apply form(s) the gate accepts in an approve environment
	TS         string        `json:"ts"`
}

// Destroys reports a plan that destroys: a destroy plan, or a stateful
// delete, replace or unknown action in its summary (§9.1).
func (r Record) Destroys() bool { return r.Destroy || r.Summary.HasStatefulDestroy() }

// FailedChecks lists the required checks that failed.
func (r Record) FailedChecks() []string {
	var out []string
	for _, c := range r.Checks {
		if c.Required && c.Status == "fail" {
			out = append(out, c.ID)
		}
	}
	return out
}

// SubjectFor is the approval subject: the stack for CDK (one approval per
// stack), the record's subject otherwise.
func (r Record) SubjectFor(stack string) string {
	if s, ok := r.StackFor(stack); ok && (r.Tool == "cdk" || r.Tool == "cdktf") {
		return Subject(s.Name)
	}
	return r.Subject
}

// StackFor returns a CDK stack by name or alias.
func (r Record) StackFor(name string) (Stack, bool) {
	for _, s := range r.Stacks {
		if s.Name == name {
			return s, true
		}
		for _, a := range s.Aliases {
			if a == name {
				return s, true
			}
		}
	}
	return Stack{}, false
}

var idRe = regexp.MustCompile(`^p[0-9]{14}-[0-9a-f]{6}$`)

// ValidID reports a plan id of the store's form.
func ValidID(id string) bool { return idRe.MatchString(id) }

// NewID returns a fresh plan id: p<yyyymmddhhmmss>-<6 hex>.
func NewID(now time.Time) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return "p" + now.UTC().Format("20060102150405") + "-" + hex.EncodeToString(b[:])
}

// RecordName is the per-plan record file name under StateDir().
func RecordName(id string) string { return Dir + "/" + id + ".json" }

// PlanName is the stored plan's name under StateDir() for an extension
// (tfplan, whatif.json, changeset.json, preview.json, pulumi-plan.json).
func PlanName(id, ext string) string { return Dir + "/" + id + "." + ext }

// RedactedName is the redacted JSON copy's name.
func RedactedName(id string) string { return Dir + "/" + id + ".redacted.json" }

// EnsureDir creates StateDir()/plans with 0700.
func EnsureDir(stateDir string) (string, error) {
	if stateDir == "" {
		return "", errors.New("plans: no workspace state directory (run inside a trusted workspace)")
	}
	d := filepath.Join(stateDir, Dir)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(d, 0o700)
	return d, nil
}

// WriteFile writes a store file with 0600 (atomic rename).
func WriteFile(stateDir, name string, data []byte) error {
	if _, err := EnsureDir(stateDir); err != nil {
		return err
	}
	p := filepath.Join(stateDir, filepath.FromSlash(name))
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Chmod(p, 0o600)
}

// Save writes the per-plan record (replacing it).
func Save(stateDir string, r Record) error {
	r.Schema = Schema
	if !ValidID(r.ID) {
		return fmt.Errorf("plans: bad plan id %q", r.ID)
	}
	if r.Checks == nil {
		r.Checks = []Check{}
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(stateDir, RecordName(r.ID), append(b, '\n'))
}

// Append adds the record to plans.jsonl (0600, fsync).
func Append(stateDir string, r Record) error {
	r.Schema = Schema
	if r.Checks == nil {
		r.Checks = []Check{}
	}
	if stateDir == "" {
		return errors.New("plans: no workspace state directory")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(stateDir, LogFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// Parse reads one record.
func Parse(raw []byte) (Record, error) {
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, fmt.Errorf("plans: corrupt record: %w", err)
	}
	if r.Schema != Schema || !ValidID(r.ID) {
		return r, errors.New("plans: not a plan record")
	}
	return r, nil
}

// Load reads the per-plan record of id.
func Load(stateDir, id string) (Record, error) {
	if !ValidID(id) {
		return Record{}, fmt.Errorf("plans: %q is not a plan id (p<timestamp>-<hex>)", id)
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, filepath.FromSlash(RecordName(id))))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, fmt.Errorf("plans: no plan %s in this workspace", id)
		}
		return Record{}, err
	}
	return Parse(raw)
}

// All reads plans.jsonl; the last line per id wins. A malformed line is
// skipped (the per-plan record is the guard's source of truth).
func All(stateDir string) ([]Record, error) {
	raw, err := os.ReadFile(filepath.Join(stateDir, LogFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	byID := map[string]Record{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		if r, err := Parse(sc.Bytes()); err == nil {
			byID[r.ID] = r
		}
	}
	out := make([]Record, 0, len(byID))
	for _, r := range byID {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// Prune deletes the stored plan files of plans older than age (by the id's
// timestamp, falling back to the file time). Records and ledger lines stay.
// It returns the ids pruned.
func Prune(stateDir string, age time.Duration, now time.Time) ([]string, error) {
	d := filepath.Join(stateDir, Dir)
	ents, err := os.ReadDir(d)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cut := now.Add(-age)
	pruned := map[string]bool{}
	for _, e := range ents {
		name := e.Name()
		id, rest, ok := strings.Cut(name, ".")
		if !ok || !ValidID(id) || rest == "json" || e.IsDir() {
			continue // keep the per-plan record
		}
		ts, err := time.Parse("20060102150405", id[1:15])
		if err != nil {
			continue
		}
		if ts.After(cut) {
			continue
		}
		if err := os.Remove(filepath.Join(d, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		pruned[id] = true
	}
	out := make([]string, 0, len(pruned))
	for id := range pruned {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// Evidence is the approval evidence of a record (§9.4): the plan id and
// hash, the inputs hash, the environment, the identity and the tool's own
// binding. stack names the CDK stack (one approval per stack).
func (r Record) Evidence(stack string) map[string]string {
	ev := map[string]string{
		EvPlanID:   r.ID,
		EvPlanSHA:  r.PlanSHA256,
		EvInputs:   nonEmpty(r.InputsHash),
		EvEnv:      r.Env,
		EvIdentity: nonEmpty(r.Identity),
	}
	switch r.Tool {
	case "cdk", "cdktf":
		if s, ok := r.StackFor(stack); ok {
			ev[EvStack] = s.Name
			ev[EvPlanSHA] = s.Hash
		}
	case "aws":
		ev[EvChangeSet] = r.ChangeSet
		ev[EvStack] = nonEmpty(r.Stack)
	case "az":
		ev[EvTemplate] = nonEmpty(r.TemplateSH)
		ev[EvParams] = nonEmpty(r.ParamsSHA)
		ev[EvScope] = nonEmpty(r.Scope)
	case "pulumi":
		if r.Weak {
			ev[EvStack] = nonEmpty(r.Stack)
			ev[EvProject] = nonEmpty(r.Project)
			ev[EvStackConfig] = nonEmpty(r.StackCfg)
		}
	}
	return ev
}

// None is the evidence value of something absent.
const None = "none"

func nonEmpty(s string) string {
	if s == "" {
		return None
	}
	return s
}
