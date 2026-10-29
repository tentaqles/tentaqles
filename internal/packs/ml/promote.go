package ml

// The model promotion gate (spec C §4.4): request (anyone) asks a human to
// approve a promotion bound to evidence hashes; apply (human only) checks the
// approval against the evidence recomputed now, shows the declared promotion
// command, asks for a typed yes, runs it, marks the approval used and records
// the promotion in the model card.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// ReproCheckIDs are the reproducibility checks a promotion's repro item runs.
var ReproCheckIDs = []string{"ml/seeds", "ml/env-lock", "ml/data-snapshot"}

// lookupCheck finds a registered check (tests replace it).
var lookupCheck = func(id string) (packs.Check, bool) {
	for _, c := range packs.Checks("ml") {
		if c.ID == id {
			return c, true
		}
	}
	return packs.Check{}, false
}

// resolveWorkspace maps cwd to its trusted workspace, or nil.
var resolveWorkspace = func(cwd string) *resolve.Workspace {
	cfg, err := registry.Load()
	if err != nil {
		return nil
	}
	return resolve.Resolve(cwd, cfg).Workspace
}

// promoteEnv is what the promotion and drift commands work on.
type promoteEnv struct {
	WS *resolve.Workspace // nil outside a trusted workspace
	In *packs.Input       // bound to the ml pack
	St Settings
}

// newPromoteEnv builds the Input for cwd: the workspace's manifest merged
// with tq-packs.yaml, git, and the tool runner.
func newPromoteEnv(cwd string, ws *resolve.Workspace) (*promoteEnv, error) {
	root := cwd
	if r, err := packs.RepoRoot(context.Background(), cwd); err == nil {
		root = r
	}
	in := &packs.Input{Root: root, Git: packs.GitRepo{Root: root}}
	if ws != nil && ws.Manifest != nil {
		in.Manifest = ws.Manifest
		in.SetWorkspace(ws.Name, ws.Root)
		packs.SetEventWorkspace(ws.Name)
	}
	cfg, err := packs.LoadConfig(in.Manifest, root)
	if err != nil {
		return nil, err
	}
	in.Config = cfg
	in.Tools = packs.NewToolRunner(root, cfg)
	in = in.ForPack("ml")
	if !cfg.IsEnabled("ml") {
		return nil, errors.New("the ml pack is not enabled here (packs.ml in the manifest or tq-packs.yaml)")
	}
	return &promoteEnv{WS: ws, In: in, St: loadIn(in)}, nil
}

func (e *promoteEnv) workspace() (string, error) {
	if e.WS == nil || e.WS.Name == "" {
		return "", errors.New("not in a trusted workspace: promotion approvals belong to a workspace (tq allow)")
	}
	return e.WS.Name, nil
}

// projectDir is the repo root relative to the workspace root ("" for the
// workspace root itself or a repo outside it).
func (e *promoteEnv) projectDir() string {
	if e.WS == nil || e.WS.Root == "" {
		return ""
	}
	rel, err := filepath.Rel(e.WS.Root, e.In.Root)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// RequestResult is what promote request did.
type RequestResult struct {
	Model, Version, Target string
	Protected              bool
	Decision, Card         string
	Evidence               map[string]string
	Drift                  *DriftResult
	Require                []string // the promotion.require items that ran
	Clamped                []string // tq-packs.yaml values load tightened
}

func checkNames(model, version, target string) error {
	if !modelNameRe.MatchString(model) {
		return fmt.Errorf("model %q: use [A-Za-z0-9._-]", model)
	}
	for k, v := range map[string]string{"version": version, "target": target} {
		if !targetRe.MatchString(v) || strings.HasPrefix(v, "-") {
			return fmt.Errorf("%s %q: only [A-Za-z0-9._@-] is allowed, not starting with -", k, v)
		}
	}
	return nil
}

// gitHead is the commit at HEAD.
func gitHead(ctx context.Context, root string) (string, error) {
	out, err := packs.Git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// dirtyPaths lists changed and untracked paths, ignoring tq's own drift
// output under .tq/drift/ (gitignore it).
func dirtyPaths(ctx context.Context, root string) ([]string, error) {
	out, err := packs.Git(ctx, root, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("git status: %v", err)
	}
	var dirty []string
	parts := strings.Split(string(out), "\x00")
	for i := 0; i < len(parts); i++ {
		e := parts[i]
		if len(e) < 4 {
			continue
		}
		st, p := e[:2], e[3:]
		if st[0] == 'R' || st[0] == 'C' {
			i++ // the original path follows
		}
		if strings.HasPrefix(p, resultDir+"/") {
			continue
		}
		dirty = append(dirty, p)
	}
	return dirty, nil
}

func hashFile(in *packs.Input, p string) (string, error) {
	b, err := readWork(in, p, driftMaxBytes)
	if err != nil {
		return "", err
	}
	return packs.HashBytes(b), nil
}

// checkOutcome is the deterministic part of a check result (for the checks
// evidence hash).
type checkOutcome struct {
	ID       string          `json:"id"`
	Status   packs.Status    `json:"status"`
	Findings []packs.Finding `json:"findings"`
}

// requirements runs the card and repro items; drift runs separately (it may
// produce files). It returns the outcomes and the failure lines.
func requirements(ctx context.Context, e *promoteEnv, m Model) ([]checkOutcome, []string) {
	var outs []checkOutcome
	var fails []string
	if e.St.Requires("model_card") {
		fs := cardFindings(e.In, e.St, m)
		if fs == nil {
			fs = []packs.Finding{}
		}
		o := checkOutcome{ID: "ml/model-card", Status: packs.OK, Findings: fs}
		for _, f := range fs {
			if f.Level == packs.LevelError {
				o.Status = packs.Fail
				fails = append(fails, "model_card: "+f.String())
			}
		}
		outs = append(outs, o)
	}
	if e.St.Requires("repro") {
		rin := *e.In
		rin.All, rin.Base, rin.Staged = true, "", false
		rin.Changed = nil
		files, err := e.In.Git.ListFiles()
		if err != nil {
			fails = append(fails, "repro: cannot list the tracked files: "+err.Error())
		}
		train := e.St.Paths.Train
		for _, f := range files {
			if len(train) == 0 || matchAny(train, f) {
				rin.Changed = append(rin.Changed, packs.ChangedFile{Path: f, Status: 'A'})
			}
		}
		for _, id := range ReproCheckIDs {
			c, ok := lookupCheck(id)
			if !ok {
				fails = append(fails, "repro: "+id+" is not available in this tq build (fail closed)")
				outs = append(outs, checkOutcome{ID: id, Status: packs.Fail, Findings: []packs.Finding{}})
				continue
			}
			r := runOne(ctx, c, rin.ForPack("ml"))
			if r.Findings == nil {
				r.Findings = []packs.Finding{}
			}
			errs, _ := r.Counts()
			if errs > 0 {
				r.Status = packs.Fail
			}
			outs = append(outs, checkOutcome{ID: id, Status: r.Status, Findings: r.Findings})
			if r.Status == packs.Fail {
				line := "repro: " + id + " failed"
				if r.Detail != "" {
					line += ": " + r.Detail
				}
				fails = append(fails, line)
				for i, f := range r.Findings {
					if i == 5 {
						break
					}
					fails = append(fails, "  "+f.String())
				}
			}
		}
	}
	return outs, fails
}

func matchAny(globs []string, p string) bool {
	for _, g := range globs {
		if packs.MatchGlob(g, p) {
			return true
		}
	}
	return false
}

// runOne runs a check with the built-in timeout; a panic or a timeout fails.
func runOne(ctx context.Context, c packs.Check, in *packs.Input) (r packs.Result) {
	if c.Applies != nil && !c.Applies(in) {
		return packs.Result{ID: c.ID, Status: packs.Skip, Detail: "does not apply"}
	}
	ctx, cancel := context.WithTimeout(ctx, packs.BuiltinTimeout)
	defer cancel()
	done := make(chan packs.Result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- packs.Result{ID: c.ID, Status: packs.Fail, Detail: fmt.Sprintf("panicked: %v", p)}
			}
		}()
		done <- c.Run(ctx, in)
	}()
	select {
	case r = <-done:
	case <-ctx.Done():
		r = packs.Result{ID: c.ID, Status: packs.Fail, Detail: "timed out"}
	}
	return r
}

// promotionArgv fills promotion.command for one promotion.
func promotionArgv(st Settings, model, version, target string) ([]string, error) {
	if strings.TrimSpace(st.Promotion.Command) == "" {
		return nil, errors.New("packs.ml.promotion.command is not set: the human could not apply the promotion (add it to the manifest)")
	}
	return FillCommand(st.Promotion.Command, map[string]string{"model": model, "version": version, "target": target})
}

// evidence computes the binding of a promotion now. A missing file is an
// error (the request refuses; apply reports the mismatch).
func evidence(ctx context.Context, e *promoteEnv, m Model, version, target string, outs []checkOutcome) (map[string]string, error) {
	ev := map[string]string{}
	var err error
	if ev["model_card"], err = hashFile(e.In, m.Card); err != nil {
		return nil, fmt.Errorf("model card: %v", err)
	}
	if m.Metrics != "" {
		if ev["metrics"], err = hashFile(e.In, m.Metrics); err != nil {
			return nil, fmt.Errorf("metrics file: %v", err)
		}
	}
	if e.St.Requires("drift") {
		if ev["drift"], err = hashFile(e.In, resultPath(m.Name)); err != nil {
			return nil, fmt.Errorf("drift result: %v (run tq pack ml drift %s)", err, m.Name)
		}
	}
	b, _ := json.Marshal(outs)
	ev["checks"] = packs.HashBytes(b)
	head, err := gitHead(ctx, e.In.Root)
	if err != nil {
		return nil, err
	}
	ev["git_head"] = "commit:" + head
	dirty, err := dirtyPaths(ctx, e.In.Root)
	if err != nil {
		return nil, err
	}
	ev["clean_tree"] = "clean"
	if len(dirty) > 0 {
		ev["clean_tree"] = "dirty"
	}
	argv, err := promotionArgv(e.St, m.Name, version, target)
	if err != nil {
		return nil, err
	}
	ev["command"] = packs.HashBytes([]byte(strings.Join(argv, "\x00")))
	return ev, nil
}

// promoteRequest runs the require items and asks for the approval.
func promoteRequest(ctx context.Context, e *promoteEnv, model, version, target string) (RequestResult, error) {
	res := RequestResult{Model: model, Version: version, Target: target, Require: e.St.Promotion.Require, Clamped: e.St.Clamped}
	if err := checkNames(model, version, target); err != nil {
		return res, err
	}
	m, ok := e.St.Model(model)
	if !ok {
		return res, fmt.Errorf("model %q is not declared in packs.ml.models", model)
	}
	if !e.St.Protected(target) {
		return res, nil
	}
	res.Protected = true
	ws, err := e.workspace()
	if err != nil {
		return res, err
	}
	if _, err := promotionArgv(e.St, model, version, target); err != nil {
		return res, err
	}
	dirty, err := dirtyPaths(ctx, e.In.Root)
	if err != nil {
		return res, err
	}
	if len(dirty) > 0 {
		sort.Strings(dirty)
		if len(dirty) > 10 {
			dirty = append(dirty[:10], "...")
		}
		return res, fmt.Errorf("a promotion needs a clean tree: commit or stash first (%s)", strings.Join(dirty, ", "))
	}
	outs, fails := requirements(ctx, e, m)
	if e.St.Requires("drift") {
		dr, err := runDrift(ctx, e.In, e.St, m, driftOptions{})
		switch {
		case err != nil:
			fails = append(fails, "drift: "+err.Error())
		case dr.Status == "fail":
			for _, r := range dr.Reasons {
				fails = append(fails, "drift: "+r)
			}
		}
		if err == nil {
			res.Drift = &dr
		}
	}
	if len(fails) > 0 {
		return res, fmt.Errorf("promotion of %s@%s to %s refused; fix these first:\n  %s", model, version, target, strings.Join(fails, "\n  "))
	}
	ev, err := evidence(ctx, e, m, version, target, outs)
	if err != nil {
		return res, err
	}
	if ev["clean_tree"] != "clean" {
		return res, errors.New("the tree changed while the checks ran: commit and request again")
	}
	res.Evidence = ev
	id, card, err := approval.Ask(ws, approval.Request{Pack: "ml", Action: "promote", Subject: model + "@" + version, Target: target,
		Evidence: ev, Summary: requestSummary(e, m, version, target, res.Drift), ValidFor: e.St.ValidFor(), ProjectDir: e.projectDir()})
	if err != nil {
		return res, err
	}
	res.Decision, res.Card = id, card
	packs.Count("ml", "promote_requested", 1)
	return res, nil
}

// requestSummary is the decision text: metrics (numbers only), the drift
// verdict and the card path. approval.Ask redacts it again.
func requestSummary(e *promoteEnv, m Model, version, target string, dr *DriftResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Promote model %s version %s to %s.\n\nModel card: %s\n", m.Name, version, target, m.Card)
	fmt.Fprintf(&b, "Checks run: %s.\n", strings.Join(e.St.Promotion.Require, ", "))
	if len(e.St.Clamped) > 0 {
		b.WriteString("\nClamped from tq-packs.yaml (the repo file cannot loosen the manifest):\n")
		for _, c := range e.St.Clamped {
			b.WriteString("- " + c + "\n")
		}
	}
	if m.Metrics != "" {
		if raw, err := readWork(e.In, m.Metrics, cardMaxBytes); err == nil {
			if ms, err := readMetrics(raw); err == nil && len(ms) > 0 {
				names := make([]string, 0, len(ms))
				for k := range ms {
					names = append(names, k)
				}
				sort.Strings(names)
				b.WriteString("\n| Metric | Value |\n|---|---|\n")
				for i, k := range names {
					if i == 20 {
						break
					}
					fmt.Fprintf(&b, "| %s | %g |\n", k, ms[k])
				}
			}
		}
	}
	if dr != nil {
		warned := 0
		for _, f := range dr.Features {
			if f.Status == "warn" {
				warned++
			}
		}
		fmt.Fprintf(&b, "\nDrift (%s): %s, %d of %d features warn.\n", dr.Metric, dr.Status, warned, len(dr.Features))
	}
	argv, _ := promotionArgv(e.St, m.Name, version, target)
	fmt.Fprintf(&b, "\nOn approval the human runs: tq pack ml promote apply <decision>\nwhich runs: %s\n", QuoteArgv(argv))
	return b.String()
}

// splitSubject reads "model@version".
func splitSubject(s string) (model, version string, ok bool) {
	i := strings.Index(s, "@")
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// changedEvidence names the evidence entries that differ (names only).
func changedEvidence(was, now map[string]string) []string {
	var out []string
	keys := map[string]bool{}
	for k := range was {
		keys[k] = true
	}
	for k := range now {
		keys[k] = true
	}
	for k := range keys {
		if was[k] != now[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// identityEnv is the workspace's identity env as toolrun.Spec.Env pairs.
// toolrun lays Spec.Env over os.Environ() and the last value of a key wins,
// so this is envplan.Environ (the parent env cleaned of every other
// workspace's shell state and provider vars, with this workspace's vars
// applied) plus "KEY=" for each parent variable Environ dropped: another
// client's provider variable reaches the command blank, never with its
// value. toolrun has no env-replace mode, so a dropped key is blank rather
// than unset (a framework follow-up).
func identityEnv(ws *resolve.Workspace) []string {
	if ws == nil {
		return nil
	}
	parent := os.Environ()
	want := envplan.Environ(ws, parent)
	keep := map[string]bool{}
	for _, kv := range want {
		keep[envKeyOf(kv)] = true
	}
	var out []string
	for _, kv := range parent {
		if k := envKeyOf(kv); k != "" && !keep[k] {
			out = append(out, kv[:len(k)]+"=")
		}
	}
	sort.Strings(out)
	return append(out, want...)
}

// envKeyOf is the key of KEY=VALUE, upper-cased on Windows where the
// environment is case-insensitive.
func envKeyOf(kv string) string {
	i := strings.Index(kv, "=")
	if i == 0 { // Windows "=C:=C:\..." entries
		i = strings.Index(kv[1:], "=") + 1
	}
	if i <= 0 {
		return ""
	}
	if runtime.GOOS == "windows" {
		return strings.ToUpper(kv[:i])
	}
	return kv[:i]
}

// promoteApply is the human-only apply.
func promoteApply(ctx context.Context, e *promoteEnv, id string, stdin io.Reader, out io.Writer, isTTY bool) error {
	if err := packs.HumanOnly(); err != nil {
		return err
	}
	ws, err := e.workspace()
	if err != nil {
		return err
	}
	entries, err := ledger.Read(ws, "ml")
	if err != nil {
		return err
	}
	s, ok := ledger.Resolve(entries)[id]
	if !ok || s.Action != "promote" {
		return fmt.Errorf("%s: %w", id, approval.ErrUnknown)
	}
	model, version, ok := splitSubject(s.Subject)
	if !ok {
		return fmt.Errorf("%s: subject %q is not model@version", id, s.Subject)
	}
	if err := checkNames(model, version, s.Target); err != nil {
		return err
	}
	m, ok := e.St.Model(model)
	if !ok {
		return fmt.Errorf("model %q is no longer declared in packs.ml.models", model)
	}
	outs, _ := requirements(ctx, e, m)
	ev, err := evidence(ctx, e, m, version, s.Target, outs)
	if err != nil {
		return fmt.Errorf("cannot recompute the evidence: %v", err)
	}
	a, err := approval.Verify(ws, id, ev)
	if err != nil {
		if errors.Is(err, approval.ErrEvidence) {
			return fmt.Errorf("%w (changed: %s); request a new promotion", err, strings.Join(changedEvidence(s.Evidence, ev), ", "))
		}
		return err
	}
	argv, err := promotionArgv(e.St, model, version, s.Target)
	if err != nil {
		return err
	}
	shown := QuoteArgv(argv)
	if err := approval.Confirm(stdin, out, isTTY, shown); err != nil {
		return err
	}
	fmt.Fprintf(out, "running (timeout %s)...\n", PromotionTimeout)
	// The run gets its own 300 s from now: the request-wide context also
	// covered the wait at the yes prompt, which may have used it up.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), PromotionTimeout)
	defer cancel()
	res, err := e.In.Tools.Run(rctx, toolrun.Spec{Pack: "ml", Tool: argv[0], Args: argv[1:], Timeout: PromotionTimeout, Env: identityEnv(e.WS)})
	classes := classesFor(e.In)
	if err == nil {
		tail := strings.TrimSpace(tailBoth(res, 40))
		if tail != "" {
			fmt.Fprintln(out, tail)
		}
	}
	switch {
	case err != nil:
		_ = approval.Comment(ws, a.Card, fmt.Sprintf("promotion %s -> %s did not run: %v", s.Subject, s.Target, err))
		return fmt.Errorf("the promotion command did not run: %v", err)
	case res.TimedOut:
		_ = approval.Comment(ws, a.Card, fmt.Sprintf("promotion %s -> %s timed out after %s; the approval is still unused", s.Subject, s.Target, PromotionTimeout))
		return fmt.Errorf("the promotion command timed out after %s (the approval is still unused)", PromotionTimeout)
	case res.ExitCode != 0:
		_ = approval.Comment(ws, a.Card, fmt.Sprintf("promotion %s -> %s failed: exit %d\n\n%s", s.Subject, s.Target, res.ExitCode, res.Tail(20, classes)))
		return fmt.Errorf("the promotion command exited %d (the approval is still unused)", res.ExitCode)
	}
	if err := approval.MarkUsed(ws, id); err != nil {
		return fmt.Errorf("the promotion ran but the ledger write failed (%v): tell the team, the approval may look unused", err)
	}
	packs.Count("ml", "promotions_applied", 1)
	date := approval.Now().Format("2006-01-02")
	cardNote := ""
	cardPath := filepath.Join(e.In.Root, filepath.FromSlash(m.Card))
	if raw, err := os.ReadFile(cardPath); err != nil {
		cardNote = "; could not read the model card to record it: " + unwrapPath(err).Error()
	} else if updated, err := appendPromotionRow(string(raw), []string{date, version, s.Target, id, "human"}); err != nil {
		cardNote = "; " + err.Error()
	} else if err := os.WriteFile(cardPath, []byte(updated), 0o644); err != nil {
		cardNote = "; could not write the model card: " + unwrapPath(err).Error()
	} else {
		cardNote = "; recorded in " + m.Card + " (commit it)"
	}
	fmt.Fprintf(out, "promoted %s to %s (decision %s)%s\n", s.Subject, s.Target, id, cardNote)
	_ = approval.Comment(ws, a.Card, fmt.Sprintf("promoted %s to %s by a human with tq pack ml promote apply (decision %s)", s.Subject, s.Target, id))
	return nil
}

func tailBoth(o toolrun.Output, n int) string {
	var b bytes.Buffer
	b.Write(o.Stdout)
	if len(o.Stderr) > 0 {
		b.WriteByte('\n')
		b.Write(o.Stderr)
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\r\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// PromotionRow is one line of promote status.
type PromotionRow struct {
	approval.Status
	Model   string `json:"model"`
	Version string `json:"version"`
}

// promoteStatus lists the promotion approvals (model "" = all).
func promoteStatus(e *promoteEnv, model string) ([]PromotionRow, error) {
	ws, err := e.workspace()
	if err != nil {
		return nil, err
	}
	rows, err := approval.List(ws, "ml")
	if err != nil {
		return nil, err
	}
	out := []PromotionRow{}
	for _, r := range rows {
		if r.Action != "promote" {
			continue
		}
		mn, v, _ := splitSubject(r.Subject)
		if model != "" && mn != model {
			continue
		}
		out = append(out, PromotionRow{Status: r, Model: mn, Version: v})
	}
	return out, nil
}
