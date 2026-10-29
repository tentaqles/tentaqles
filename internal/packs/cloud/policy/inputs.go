package policy

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/readers"
)

// Plan is one recorded plan, as the apply gate's plan store (D-CG) exposes
// it to the policy checks. Only the redacted JSON copy is ever read here.
type Plan struct {
	ID string
	// Env is the environment the plan was recorded for; Policy is that
	// environment's apply policy (auto | ask | approve | deny), "" to derive
	// it from packs.cloud.environments.<env>.apply and the spec defaults.
	Env, Policy string
	// Kind is the reader format of the stored copy.
	Kind model.Kind
	// Name is the file name findings show (for example "plan:<id>").
	Name string
	// Path is the absolute path of the redacted JSON copy, under the cloud
	// pack's StateDir().
	Path string
}

// RecordedPlans lists the plans recorded for the current IaC tree (their
// inputs hash matches the working tree). The apply gate's register file
// sets it; nil means no plan store, so only IaC source is read. An error
// fails the checks that read plans (fail closed).
var RecordedPlans func(in *packs.Input) ([]Plan, error)

// Doc is one input of the policy checks: an IaC source file or a recorded
// plan, read into resources.
type Doc struct {
	Name      string // the file findings show
	Path      string // where the bytes are on disk (absolute; "" for staged blobs)
	Kind      model.Kind
	Entry     Entry // the IaC entry a source file sits under (zero for plans)
	Plan      *Plan // set for a recorded plan
	Env       string
	Policy    string
	Raw       []byte
	Resources []model.Resource
}

// Approve reports whether the document targets an environment whose apply
// policy is approve or deny (where IAC001 fails).
func (d Doc) Approve() bool { return d.Policy == "approve" || d.Policy == "deny" }

// Prod reports whether the document targets production: an environment
// named prod or production, or one whose apply policy is approve or deny.
func (d Doc) Prod() bool {
	e := strings.ToLower(d.Env)
	return e == "prod" || e == "production" || d.Approve()
}

// Bounds of the source scan.
const (
	MaxSourceFiles = 2000
	MaxSourceBytes = 16 << 20
	MaxPlanBytes   = 64 << 20
)

// sourceExt are the file types read as templates.
var sourceExt = map[string]bool{".json": true, ".yaml": true, ".yml": true, ".template": true}

// skipDir are directories never read as source.
var skipDir = map[string]bool{".terraform": true, "node_modules": true, ".git": true, ".venv": true}

func skipped(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if skipDir[seg] {
			return true
		}
	}
	return false
}

// PolicyFor derives the apply policy of env: an explicit policy wins, then
// packs.cloud.environments.<env>.apply, then the spec defaults (prod:
// approve; any other: ask).
func PolicyFor(s packs.Settings, env, explicit string) string {
	if explicit != "" {
		return strings.ToLower(explicit)
	}
	if env == "" {
		return ""
	}
	if v := s.String("environments." + env + ".apply"); v != "" {
		return strings.ToLower(v)
	}
	if strings.EqualFold(env, "prod") {
		return "approve"
	}
	return "ask"
}

// Plans returns the recorded plans, or nil without a plan store.
func Plans(in *packs.Input) ([]Plan, error) {
	if RecordedPlans == nil {
		return nil, nil
	}
	return RecordedPlans(in)
}

// HasPlans reports whether a plan is recorded for the current tree. An
// error counts as true, so the check runs and reports it.
func HasPlans(in *packs.Input) bool {
	ps, err := Plans(in)
	return err != nil || len(ps) > 0
}

// SourceKinds are the entry kinds whose files the readers understand.
var SourceKinds = []string{KindARM, KindBicep, KindCloudFormation, KindCDK}

// SourceFiles returns the repo-relative template candidates under the
// changed entries of the given kinds: the changed files (every tracked file
// with --all) with a template extension. Paths under .terraform/ and
// similar are skipped.
func SourceFiles(in *packs.Input, cfg Config, kinds ...string) ([]string, []Entry, error) {
	entries := cfg.ChangedEntries(in, kinds...)
	if len(entries) == 0 {
		return nil, nil, nil
	}
	var cands []string
	if in.All {
		if in.Git == nil {
			return nil, nil, errors.New("no git reader")
		}
		files, err := in.Git.ListFiles()
		if err != nil {
			return nil, nil, fmt.Errorf("cannot list the tracked files: %w", err)
		}
		cands = files
	} else {
		cands = in.ChangedPaths()
	}
	seen := map[string]bool{}
	var out []string
	var owners []Entry
	for _, p := range cands {
		p = CleanPath(p)
		if seen[p] || skipped(p) || !sourceExt[strings.ToLower(path.Ext(p))] {
			continue
		}
		for _, e := range entries {
			if Under(e.Path, p) {
				seen[p] = true
				out = append(out, p)
				owners = append(owners, e)
				break
			}
		}
	}
	return out, owners, nil
}

// CDKTemplates returns the cdk.out/*.template.json files on disk under the
// changed cdk entries (cdk.out is a build output, normally untracked).
func CDKTemplates(in *packs.Input, cfg Config) ([]string, []Entry) {
	var out []string
	var owners []Entry
	for _, e := range cfg.ChangedEntries(in, KindCDK) {
		dir := path.Join(e.Path, "cdk.out")
		ents, err := os.ReadDir(filepath.Join(in.Root, filepath.FromSlash(dir)))
		if err != nil {
			continue
		}
		var names []string
		for _, de := range ents {
			if !de.IsDir() && de.Type().IsRegular() && strings.HasSuffix(de.Name(), ".template.json") {
				names = append(names, de.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			if len(out) >= MaxSourceFiles {
				break
			}
			out = append(out, path.Join(dir, n))
			owners = append(owners, e)
		}
	}
	return out, owners
}

func readDisk(root, rel string, max int64) ([]byte, error) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", rel)
	}
	if fi.Size() > max {
		return nil, fmt.Errorf("%s is larger than %d bytes", rel, max)
	}
	return os.ReadFile(p)
}

// Collect reads every input of the policy checks: the template files under
// the changed source entries, the cdk.out templates, and the recorded
// plans. Unreadable or unparsable source files become warn findings
// (IAC000; the external linters own syntax); a plan store error or an
// unreadable plan is returned as an error (fail closed).
func Collect(in *packs.Input, cfg Config) ([]Doc, []packs.Finding, error) {
	var docs []Doc
	var notes []packs.Finding
	files, owners, err := SourceFiles(in, cfg, SourceKinds...)
	if err != nil {
		return nil, nil, err
	}
	if len(files) > MaxSourceFiles {
		return nil, nil, fmt.Errorf("%d template files changed, more than the %d tq reads", len(files), MaxSourceFiles)
	}
	add := func(name, disk string, e Entry, raw []byte) {
		k := readers.Detect(name, raw)
		if k == "" {
			return // not a template (package.json, parameters files, ...)
		}
		rs, err := readers.Read(k, name, raw, readers.Options{})
		if err != nil {
			notes = append(notes, packs.Finding{File: name, Level: packs.LevelWarn, Rule: "IAC000",
				Message: "cannot read this " + string(k) + " document: " + readErr(err)})
			return
		}
		docs = append(docs, Doc{Name: name, Path: disk, Kind: k, Entry: e, Raw: raw, Resources: rs})
	}
	for i, f := range files {
		raw, err := in.ReadNew(f, MaxSourceBytes)
		if err != nil {
			notes = append(notes, packs.Finding{File: f, Level: packs.LevelWarn, Rule: "IAC000", Message: "cannot read this file: " + readErr(err)})
			continue
		}
		disk := ""
		if !in.Staged {
			disk = filepath.Join(in.Root, filepath.FromSlash(f))
		}
		add(f, disk, owners[i], raw)
	}
	cdk, cdkOwners := CDKTemplates(in, cfg)
	for i, f := range cdk {
		raw, err := readDisk(in.Root, f, MaxSourceBytes)
		if err != nil {
			notes = append(notes, packs.Finding{File: f, Level: packs.LevelWarn, Rule: "IAC000", Message: "cannot read this file: " + readErr(err)})
			continue
		}
		add(f, filepath.Join(in.Root, filepath.FromSlash(f)), cdkOwners[i], raw)
	}
	plans, err := Plans(in)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot list the recorded plans: %s", readErr(err))
	}
	s := in.Settings()
	for i := range plans {
		p := plans[i]
		name := p.Name
		if name == "" {
			name = "plan:" + p.ID
		}
		fi, err := os.Lstat(p.Path)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > MaxPlanBytes {
			return nil, nil, fmt.Errorf("cannot read the recorded plan %s", name)
		}
		raw, err := os.ReadFile(p.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read the recorded plan %s", name)
		}
		rs, err := readers.Read(p.Kind, name, raw, readers.Options{Env: p.Env})
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read the recorded plan %s: %s", name, readErr(err))
		}
		docs = append(docs, Doc{Name: name, Path: p.Path, Kind: p.Kind, Plan: &plans[i], Env: p.Env,
			Policy: PolicyFor(s, p.Env, p.Policy), Raw: raw, Resources: rs})
	}
	return docs, notes, nil
}

// readErr is an error text safe for a finding: the readers never repeat
// document values, and the text is cut short.
func readErr(err error) string {
	s := err.Error()
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
