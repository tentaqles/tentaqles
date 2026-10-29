package gate

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/plans"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Guard ids (spec D §16).
const (
	IDApplyEnv = "tq/iac-apply-env"
	IDDrift    = "tq/iac-identity-drift"
)

// Supersedes are the core rules the apply gate replaces for the calls it
// handles (§9.7).
var Supersedes = []string{"tq/iac-apply", "tq/iac-destroy", "tq/cloud-delete"}

// Events are the tools the guards read.
var Events = []string{"Bash", "PowerShell"}

var (
	coreOnce  sync.Once
	coreRules map[string]*policy.Set
)

// coreMatch reports which superseded core rules match one leaf command.
func coreMatch(raw string) (applyOrDestroy, del bool) {
	coreOnce.Do(func() {
		coreRules = map[string]*policy.Set{}
		for _, r := range policy.Builtin() {
			for _, id := range Supersedes {
				if r.ID == id {
					set, _ := policy.Build(policy.Layers{Builtin: []policy.Rule{r}})
					coreRules[id] = set
				}
			}
		}
	})
	call := policy.ToolCall{Tool: "Bash", Command: raw}
	m := func(id string) bool {
		s := coreRules[id]
		return s != nil && s.Evaluate(call).Action != policy.Allow
	}
	return m("tq/iac-apply") || m("tq/iac-destroy"), m("tq/cloud-delete")
}

// toolWordUnquoted reports an unquoted word that names a gate tool, also in
// a form tq does not unwrap (aws-cdk, cdk@2, x-terraform; review r0 M1).
func toolWordUnquoted(iv Invocation) bool {
	for _, w := range iv.Words {
		if !w.Quoted && looseTool(w.Text) {
			return true
		}
	}
	return false
}

// leaf kinds
const (
	kIgnore = iota
	kGated
	kUnreadable
	kForeign // a core rule matches a command the gate does not own
)

func classify(iv Invocation) (int, Op) {
	ad, del := coreMatch(iv.Raw)
	if iv.Tool != "" {
		switch iv.Op {
		case OpApply, OpDestroy, OpDry:
			return kGated, iv.Op
		}
		switch {
		case ad:
			_, d := destroyWord(iv.Raw)
			if d {
				return kUnreadable, OpDestroy
			}
			return kUnreadable, OpApply
		case del:
			return kForeign, OpNone
		}
		return kIgnore, OpNone
	}
	if ad {
		if iv.Opaque || toolWordUnquoted(iv) {
			_, d := destroyWord(iv.Raw)
			if d {
				return kUnreadable, OpDestroy
			}
			return kUnreadable, OpApply
		}
		return kForeign, OpNone
	}
	if del {
		return kForeign, OpNone
	}
	return kIgnore, OpNone
}

var destroyRe = regexp.MustCompile(`(?i)\b(destroy|delete-stack|delete|rm)\b`)

func destroyWord(raw string) (string, bool) {
	m := destroyRe.FindString(raw)
	return m, m != ""
}

func hostFor(g packs.GuardCtx) Host {
	home, _ := os.UserHomeDir()
	return Host{Getenv: Getenv, ReadLocal: g.ReadLocal, ListLocal: g.ListLocal, Home: home}
}

// baseDir is the folder the command starts in, relative to the repo root:
// the hook payload's cwd (GuardCtx.Cwd) when it is inside the repository, so
// a relative cd or -chdir resolves from where the session's shell is
// (review r0 m2). Otherwise "" (the repo root; a miss falls to the default
// environment).
func baseDir(g packs.GuardCtx) string {
	root := g.RepoRoot()
	wd := g.Cwd()
	if root == "" || wd == "" {
		return ""
	}
	inside := func(root string) (string, bool) {
		r, err := filepath.Rel(root, wd)
		if err != nil || filepath.IsAbs(r) || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return "", false
		}
		return r, true
	}
	r, ok := inside(root)
	if !ok {
		// Cwd is a final path (junctions, 8.3 names resolved); so is the root's
		fr, err := packs.FinalPath(root)
		if err != nil {
			return ""
		}
		if r, ok = inside(fr); !ok {
			return ""
		}
	}
	r = filepath.ToSlash(r)
	if r == "." {
		return ""
	}
	return r
}

// verdict is one leaf's outcome.
type verdict struct {
	action  policy.Action
	reason  string
	consume string
	counter string
	// readName is the stored plan (plans/<id>.<ext>) this leaf's program
	// reads, hashed with HashState in this call; on a handled allow it is
	// declared with GuardCtx.ReadArgs so the core state-dir rule lets the
	// one path through.
	readName string
	readSpec packs.ReadSpec
}

// ReadSpecs are how the gate tools read a stored plan (F's ReadArgs): the
// program (the segment's first word), the flag whose value the plan may be,
// and the boolean flags that may stand before a positional plan.
var ReadSpecs = map[string]packs.ReadSpec{
	"terraform": {Program: "terraform", BoolFlags: tfBoolFlags},
	"tofu":      {Program: "tofu", BoolFlags: tfBoolFlags},
	"pulumi":    {Program: "pulumi", Flag: "--plan", BoolFlags: []string{"--yes", "-y", "--skip-preview", "--diff", "--refresh", "--non-interactive"}},
}

var tfBoolFlags = []string{"-auto-approve", "--auto-approve", "-no-color", "--no-color", "-compact-warnings", "--compact-warnings"}

// readArgFor hashes the stored plan a terraform, tofu or pulumi leaf names
// (when it is the record's own plan file) and returns its name for
// ReadArgs; "" when the leaf names none or the hash fails (then the core
// state-dir rule decides on the path as usual).
func readArgFor(iv Invocation, rec *plans.Record, g packs.GuardCtx) (string, packs.ReadSpec) {
	spec, ok := ReadSpecs[iv.Tool]
	if !ok || rec == nil {
		return "", packs.ReadSpec{}
	}
	_, name, _ := savedPlan(iv, g.StateDir())
	if name == "" || name != rec.PlanFile {
		return "", packs.ReadSpec{}
	}
	if _, err := g.HashState(name); err != nil {
		return "", packs.ReadSpec{}
	}
	return name, spec
}

// ApplyEnv is the tq/iac-apply-env guard (§9.3, §9.4, §9.5): every apply or
// destroy in the command resolves to an environment and gets its policy.
// It handles the call (superseding the core ask rules) unless some command
// in it matches a superseded rule the gate does not own (kubectl, helm,
// an aws s3 rm).
func ApplyEnv(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	if !call.IsShell() || strings.TrimSpace(call.Command) == "" {
		return policy.Allow, "", false
	}
	gt := envgate.FromManifest(envgate.Cloud, g.Manifest(), g.Config())
	if len(gt.Envs()) == 0 {
		// No environment declared: nothing to gate per environment, so the
		// core ask rules (tq/iac-apply, tq/iac-destroy, tq/cloud-delete) stay.
		return policy.Allow, "", false
	}
	leaves := AnalyzeShellAt(call.Command, call.Tool == "PowerShell", baseDir(g))
	h := hostFor(g)
	l := &lazyLedger{g: g}
	handled, any := true, false
	var vs []verdict
	for _, iv := range leaves {
		kind, op := classify(iv)
		switch kind {
		case kIgnore:
			continue
		case kForeign:
			handled = false
			continue
		}
		any = true
		if op == OpDry {
			continue
		}
		vs = append(vs, evaluate(iv, op, kind == kUnreadable, gt, h, g, l))
	}
	if !any {
		return policy.Allow, "", false
	}
	act, reasons, consumed := policy.Allow, []string{}, []string{}
	for _, v := range vs {
		if rank(v.action) > rank(act) {
			act = v.action
		}
		if v.reason != "" {
			reasons = append(reasons, v.reason)
		}
		if v.consume != "" {
			consumed = append(consumed, v.consume)
		}
	}
	seen := map[string]bool{}
	for _, id := range consumed {
		if seen[id] {
			act = policy.Deny
			reasons = append(reasons, "one approval allows one apply: approval "+id+" is used twice in this command")
		}
		seen[id] = true
	}
	if len(consumed) > 0 && (act != policy.Allow || !handled) {
		// With an ask, or a command the core rules still ask about (handled
		// false), the approval would be used up although the human may
		// decline the ask (review r0 m3).
		act = policy.Deny
		reasons = append(reasons, "run the approved apply in a command of its own")
		handled = true
	}
	if act == policy.Allow && handled {
		for _, v := range vs {
			if v.readName == "" {
				continue
			}
			if err := g.ReadArgs(v.readSpec, v.readName); err != nil {
				act = policy.Deny
				reasons = append(reasons, IDApplyEnv+": the stored plan "+v.readName+" cannot be declared as a read argument ("+err.Error()+")")
			}
		}
	}
	for _, v := range vs {
		if v.counter != "" {
			g.Count(v.counter, 1)
		}
	}
	if act == policy.Allow {
		for _, id := range consumed {
			g.Consume(id)
		}
	}
	return act, strings.Join(reasons, "; "), handled
}

func rank(a policy.Action) int {
	switch a {
	case policy.Deny:
		return 2
	case policy.Ask:
		return 1
	}
	return 0
}

func describe(iv Invocation, op Op) string {
	if iv.Tool == "" {
		return "this command"
	}
	return iv.Tool + " " + strings.Join(iv.Verb, " ")
}

func evaluate(iv Invocation, op Op, unreadable bool, gt envgate.Gate, h Host, g packs.GuardCtx, l *lazyLedger) verdict {
	var res envgate.Resolution
	action := envgate.Apply
	if op == OpDestroy {
		action = envgate.Destroy
	}
	switch {
	case unreadable:
		res = envgate.Resolution{Env: gt.Default()}
	case iv.Unknown:
		res = gt.ResolveFloor(action, Signals(iv, h))
	default:
		res = gt.Resolve(action, Signals(iv, h))
	}
	env := res.Env
	what := describe(iv, op)
	if unreadable {
		what = "a command tq cannot read (" + firstWords(iv.Raw) + ")"
	}
	// An apply of a saved plan whose record destroys is a destroy too.
	var rec *plans.Record
	destroys := op == OpDestroy || iv.AlsoDestroys
	if !unreadable && (iv.Tool == "terraform" || iv.Tool == "tofu" || iv.Tool == "pulumi") && op == OpApply {
		if r, ok := savedPlanRecord(iv, g); ok {
			rec = &r
			if r.Destroys() {
				destroys = true
			}
		}
	}
	pol := envgate.Auto
	if op == OpApply {
		pol = gt.Policy(env, envgate.Apply)
	}
	if destroys {
		pol = envgate.Stricter(pol, gt.Policy(env, envgate.Destroy))
		action = envgate.Destroy
	}
	if unreadable {
		pol = envgate.Stricter(pol, envgate.Ask)
	}
	where := "environment " + env
	switch {
	case iv.Unknown && env != envgate.NoEnv:
		where += " (an earlier statement on the line changes the environment in a way tq cannot read, such as a sourced file, declare -x, unset or a write to the Env: drive; it counts as at least the default environment, and the identity is unknown)"
	case !res.Resolved && env != envgate.NoEnv:
		where += " (no signal named the target; it counts as the default environment)"
	}
	denied := func(reason, why string) verdict {
		if !res.Resolved && env != envgate.NoEnv {
			why = "unresolved-env"
		}
		return verdict{action: policy.Deny, reason: reason, counter: "apply_denied:" + why}
	}
	switch pol {
	case envgate.Auto:
		v := verdict{action: policy.Allow, counter: "apply_allowed:auto"}
		if !unreadable {
			// An auto apply of a stored plan: the plan path is read, not
			// written, so it is declared to the core state-dir rule.
			v.readName, v.readSpec = readArgFor(iv, rec, g)
		}
		return v
	case envgate.Ask:
		return verdict{action: policy.Ask, reason: fmt.Sprintf("%s: %s to %s (policy %s: ask); confirm the target", IDApplyEnv, what, where, action)}
	case envgate.Approve:
		if unreadable {
			return denied(fmt.Sprintf("%s: %s targets %s, which needs an approved plan; tq cannot read the command, so it cannot check the approval. Run the tool directly (no wrapper tq does not know)", IDApplyEnv, what, where), "no-approval")
		}
		return approve(iv, action, env, where, rec, gt, h, g, l, res)
	default:
		return denied(fmt.Sprintf("%s: %s to %s is %s: deny. A human runs it in their own terminal, outside Claude", IDApplyEnv, what, where, action), "policy")
	}
}

func firstWords(raw string) string {
	f := strings.Fields(raw)
	if len(f) > 4 {
		f = f[:4]
	}
	return strings.Join(f, " ")
}

// lazyLedger reads the cloud ledger once per guard call.
type lazyLedger struct {
	g         packs.GuardCtx
	done      bool
	entries   []ledger.Entry
	err       error
	trustDone bool
	t         ledger.Trust
}

// boundFlags are the flags an approved apply may carry in an approve
// environment (review r0 M2): anything else can change what is deployed
// after the evidence was checked (pulumi --config, --config-file,
// --replace, --target, --refresh; cdk --parameters, --hotswap, --method,
// --role-arn), so it is denied.
var boundFlags = map[string]map[string]bool{
	"pulumi": set("stack", "s", "yes", "y", "cwd", "C", "skip-preview", "non-interactive", "diff", "color", "message", "m", "plan"),
	"cdk":    set("app", "a", "profile", "require-approval", "progress", "outputs-file", "O", "ci", "verbose", "v", "debug", "no-color", "json", "j", "concurrency", "exclusively", "e", "context", "c", "quiet", "q"),
}

// unboundFlags names the flags of iv outside its tool's allow-list, or "".
func unboundFlags(iv Invocation) string {
	allow, ok := boundFlags[family(iv.Tool)]
	if !ok {
		return ""
	}
	var bad []string
	for f := range iv.Flags {
		if !allow[f] {
			bad = append(bad, "--"+f)
		}
	}
	if len(bad) == 0 {
		return ""
	}
	sort.Strings(bad)
	return fmt.Sprintf("%s %s cannot be bound to the approved plan (it can change what is deployed after the check); apply without it, or plan again with it", strings.Join(bad, ", "), pluralFlag(len(bad)))
}

func pluralFlag(n int) string {
	if n == 1 {
		return "is a flag that"
	}
	return "are flags that"
}

func (l *lazyLedger) read() ([]ledger.Entry, error) {
	if !l.done {
		l.done = true
		raw, err := l.g.ReadState(ledger.FileName)
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			l.err = err
		default:
			l.entries, l.err = ledger.Parse(raw)
		}
	}
	return l.entries, l.err
}

// trust is the workspace's public answer key and the cloud pack's
// high-water mark from the OS keychain, read once; empty when it cannot be
// read, and then no answer counts (fail closed). Answers at or before the
// mark count as used.
func (l *lazyLedger) trust() ledger.Trust {
	if !l.trustDone {
		l.trustDone = true
		if t, err := LoadTrust(l.g.Workspace(), "cloud"); err == nil {
			l.t = t
		}
	}
	return l.t
}

// LoadTrust reads the ledger trust (tests replace it).
var LoadTrust = ledger.LoadTrust

func (l *lazyLedger) claimed(d string) bool {
	_, err := l.g.ReadState(ledger.ClaimName(d))
	return !errors.Is(err, os.ErrNotExist) // a claim, or an error: used (fail closed)
}

// candidates are the plan ids of the approvals asked for an apply or a
// destroy to env, newest first, at most 8.
func (l *lazyLedger) candidates(env string) []string {
	entries, err := l.read()
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range ledger.Sorted(ledger.Resolve(entries)) {
		if s.Pack != "cloud" || (s.Action != envgate.Apply && s.Action != envgate.Destroy) || s.Target != env {
			continue
		}
		id := s.Evidence[plans.EvPlanID]
		if !plans.ValidID(id) || contains(out, id) {
			continue
		}
		out = append(out, id)
		if len(out) == 8 {
			break
		}
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// StateRel maps a path named in a command to its name under the cloud
// state directory ("plans/<id>.tfplan"), or "" when it is not inside it.
func StateRel(stateDir, p string) string {
	if stateDir == "" || p == "" {
		return ""
	}
	p = strings.Trim(p, `"'`)
	home := paths.Home()
	for _, v := range []string{"${TQ_HOME}", "$TQ_HOME", "$env:TQ_HOME", "%TQ_HOME%"} {
		if strings.HasPrefix(p, v) {
			p = home + p[len(v):]
		}
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[2:])
		}
	}
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == '/' {
		p = p[1:2] + ":" + p[2:] // Git Bash /c/Users -> c:/Users
	}
	if !filepath.IsAbs(p) {
		return ""
	}
	p = filepath.Clean(p)
	sd := filepath.Clean(stateDir)
	np, ns := p, sd
	if runtime.GOOS == "windows" {
		np, ns = strings.ToLower(np), strings.ToLower(ns)
	}
	if !strings.HasPrefix(np, ns+string(filepath.Separator)) {
		return ""
	}
	r := filepath.ToSlash(p[len(sd)+1:])
	if strings.Contains(r, "..") {
		return ""
	}
	return r
}

var planNameRe = regexp.MustCompile(`^plans/(p[0-9]{14}-[0-9a-f]{6})\.(tfplan|pulumi-plan\.json)$`)

// savedPlan returns the plan id and file name a terraform apply or pulumi
// up --plan names, if it is a stored plan.
func savedPlan(iv Invocation, stateDir string) (id, name string, named bool) {
	var p string
	switch iv.Tool {
	case "terraform", "tofu":
		if len(iv.Pos) == 0 {
			return "", "", false
		}
		p = iv.Pos[0]
	case "pulumi":
		v, ok := iv.Flag("plan")
		if !ok {
			return "", "", false
		}
		p = v
	}
	if p == "" {
		return "", "", false
	}
	r := StateRel(stateDir, p)
	m := planNameRe.FindStringSubmatch(r)
	if m == nil {
		return "", "", true
	}
	return m[1], r, true
}

func savedPlanRecord(iv Invocation, g packs.GuardCtx) (plans.Record, bool) {
	id, _, _ := savedPlan(iv, g.StateDir())
	if id == "" {
		return plans.Record{}, false
	}
	return readRecord(g, id)
}

func readRecord(g packs.GuardCtx, id string) (plans.Record, bool) {
	raw, err := g.ReadState(plans.RecordName(id))
	if err != nil {
		return plans.Record{}, false
	}
	r, err := plans.Parse(raw)
	return r, err == nil && r.ID == id
}

func family(t string) string {
	switch t {
	case "tofu":
		return "terraform"
	case "cdktf":
		return "cdk"
	}
	return t
}

const howTo = "record the plan with `tq pack cloud plan --env %s -- <plan command>`, ask with `tq pack cloud approve <plan-id>`, and after the human answers apply, run the apply form `tq pack cloud show <plan-id>` prints"

// approve checks the approval of an apply or destroy in an approve
// environment (§9.4, §9.5).
func approve(iv Invocation, action, env, where string, rec *plans.Record, gt envgate.Gate, h Host, g packs.GuardCtx, l *lazyLedger, res envgate.Resolution) verdict {
	what := describe(iv, OpApply)
	deny := func(why, counter string) verdict {
		if !res.Resolved && env != envgate.NoEnv && counter == "no-approval" {
			counter = "unresolved-env"
		}
		return verdict{action: policy.Deny, counter: "apply_denied:" + counter,
			reason: fmt.Sprintf("%s: %s to %s needs a human approval of the exact plan: %s. To apply: %s", IDApplyEnv, what, where, why, fmt.Sprintf(howTo, env))}
	}
	ident := ResolveIdentity(iv, h, len(gt.Match(env)["subscriptions"]) > 0)
	if ident.Unknown {
		return deny("the identity is unknown: an earlier statement on the line changes the environment in a way tq cannot read; run the apply in a command of its own, with the identity set by a flag, an inline NAME=value prefix or an export tq can read", "identity-drift")
	}
	if bad := unboundFlags(iv); bad != "" {
		return deny(bad, "hash-mismatch")
	}
	type cand struct {
		rec    plans.Record
		ev     map[string]string
		stack  string
		action string
	}
	var cands []cand
	var lastErr error
	build := func(r plans.Record) {
		if family(r.Tool) != family(iv.Tool) {
			lastErr = fmt.Errorf("plan %s is a %s plan, not %s", r.ID, r.Tool, iv.Tool)
			return
		}
		stack := ""
		ev := r.Evidence("")
		switch family(iv.Tool) {
		case "cdk":
			stack = iv.Pos[0]
			st, ok := r.StackFor(stack)
			if !ok {
				lastErr = fmt.Errorf("plan %s has no stack %s", r.ID, stack)
				return
			}
			ev = r.Evidence(stack)
			app, _ := iv.Flag("app", "a")
			if path.Clean(rel(iv.Dir, app)) != path.Clean(r.App) {
				lastErr = fmt.Errorf("the deploy must use --app %s (the synthesised assembly), not %s", r.App, app)
				return
			}
			hs, err := CDKStackHash(h, r.App, st)
			if err != nil {
				lastErr = err
				return
			}
			ev[plans.EvPlanSHA] = hs
		case "az":
			if AzScope(iv) != r.Scope {
				lastErr = fmt.Errorf("the deployment scope %s is not the plan's %s", AzScope(iv), r.Scope)
				return
			}
			t, p, err := AzBinding(iv, h)
			if err != nil {
				lastErr = err
				return
			}
			ev[plans.EvTemplate], ev[plans.EvParams], ev[plans.EvScope] = t, p, AzScope(iv)
		case "aws":
			cs, _ := iv.Flag("change-set-name")
			if cs != r.ChangeSet {
				lastErr = fmt.Errorf("the change set must be named by its ARN %s", r.ChangeSet)
				return
			}
			if sn, ok := iv.Flag("stack-name"); ok && !envgate.Matches("stacks", r.Stack, sn) && sn != r.Stack {
				lastErr = fmt.Errorf("stack %s is not the plan's %s", sn, r.Stack)
				return
			}
			ev[plans.EvChangeSet] = cs
		case "pulumi":
			if r.Weak {
				s, _ := iv.Flag("stack", "s")
				if s == "" || (s != r.Stack && !envgate.Matches("stacks", r.Stack, s)) {
					lastErr = fmt.Errorf("pulumi up must name the plan's stack (--stack %s)", r.Stack)
					return
				}
				if !iv.Has("yes", "y") {
					lastErr = errors.New("pulumi up must run with --yes against the recorded preview")
					return
				}
				dir := WorkDir(iv)
				if dir == "?" || path.Clean(dir) != path.Clean(r.Dir) {
					lastErr = fmt.Errorf("pulumi up must run in the program folder %s (use --cwd %s from the repository root)", r.Dir, r.Dir)
					return
				}
				proj, cfg, err := PulumiBinding(h, r.Dir, r.Stack)
				if err != nil {
					lastErr = err
					return
				}
				ev[plans.EvProject], ev[plans.EvStackConfig] = proj, cfg
				// §9.5: an unchanged tree (review r0 M2)
				if len(r.Tree) == 0 {
					lastErr = fmt.Errorf("plan %s has no recorded program tree; re-plan with this tq", r.ID)
					return
				}
				ts, err := TreeHash(h, r.Tree)
				if err != nil {
					lastErr = err
					return
				}
				ev[plans.EvTree] = ts
				// ... and no file added next to them since (GuardCtx.ListLocal)
				if r.ListingSHA == "" {
					lastErr = fmt.Errorf("plan %s has no recorded program listing; re-plan with this tq", r.ID)
					return
				}
				ls, err := ListingHash(h, r.Dir, r.Tree)
				if err != nil {
					lastErr = err
					return
				}
				ev[plans.EvListing] = ls
			}
		}
		hp, err := g.HashState(planFileFor(r, stack))
		if err != nil {
			lastErr = fmt.Errorf("cannot hash the stored plan (%v); re-plan into a smaller plan or split the stack", err)
			return
		}
		if family(iv.Tool) != "cdk" {
			ev[plans.EvPlanSHA] = hp
		} else if hp != r.PlanSHA256 {
			lastErr = errors.New("the stored assembly record changed")
			return
		}
		ev[plans.EvEnv] = env
		ev[plans.EvIdentity] = ident.String()
		act := action
		if r.Destroys() {
			// A recorded plan that destroys is always a destroy binding (what
			// tq pack cloud approve --destroy asks for), and needs the destroy
			// policy too (review r0 M3).
			if envgate.Stricter(gt.Policy(env, envgate.Apply), gt.Policy(env, envgate.Destroy)) != envgate.Approve {
				lastErr = fmt.Errorf("plan %s destroys, and environment %s's destroy policy (%s) does not allow an approved apply", r.ID, env, gt.Policy(env, envgate.Destroy))
				return
			}
			act = envgate.Destroy
		}
		cands = append(cands, cand{r, ev, stack, act})
	}
	switch family(iv.Tool) {
	case "terraform":
		if iv.Op == OpDestroy {
			return deny("a bare destroy cannot be bound to a plan; plan it with `-destroy -out=-` and apply the saved plan", "no-approval")
		}
		id, _, named := savedPlan(iv, g.StateDir())
		if !named {
			return deny("a bare apply is not allowed here; apply the saved plan file", "no-approval")
		}
		if id == "" || rec == nil {
			return deny("the plan file is not a plan tq recorded (it must be the stored plan path under the cloud pack's state directory)", "no-approval")
		}
		build(*rec)
	case "pulumi":
		if iv.Op == OpDestroy {
			return deny("pulumi destroy has no plan binding", "no-approval")
		}
		if id, _, named := savedPlan(iv, g.StateDir()); named {
			if id == "" || rec == nil {
				return deny("--plan must name a plan tq recorded", "no-approval")
			}
			build(*rec)
		} else {
			for _, id := range l.candidates(env) {
				if r, ok := readRecord(g, id); ok && r.Tool == "pulumi" && r.Weak {
					build(r)
				}
			}
		}
	case "cdk":
		if iv.Op == OpDestroy {
			return deny("cdk destroy has no plan binding", "no-approval")
		}
		if iv.Has("all") || len(iv.Pos) != 1 || strings.ContainsAny(iv.Pos[0], "*?") {
			return deny("deploy exactly one pre-synthesised stack per command (`cdk deploy --app <cdk.out> <stack>`); --all and wildcards need one approval per stack", "no-approval")
		}
		if _, ok := iv.Flag("app", "a"); !ok {
			return deny("deploy the pre-synthesised assembly with --app <cdk.out dir>", "no-approval")
		}
		for _, id := range l.candidates(env) {
			if r, ok := readRecord(g, id); ok {
				build(r)
			}
		}
	case "az":
		if iv.Op == OpDestroy || len(iv.Verb) < 1 || iv.Verb[0] != "deployment" {
			return deny("this az command has no plan binding", "no-approval")
		}
		for _, id := range l.candidates(env) {
			if r, ok := readRecord(g, id); ok {
				build(r)
			}
		}
	case "aws":
		if len(iv.Verb) < 2 || iv.Verb[1] != "execute-change-set" {
			if iv.Op == OpDestroy {
				return deny("delete-stack has no plan binding", "no-approval")
			}
			return deny("deploy, create-stack and update-stack cannot be bound to a plan; create a change set (`tq pack cloud plan --env "+env+" -- aws cloudformation create-change-set …`) and execute it by its ARN", "no-approval")
		}
		cs, _ := iv.Flag("change-set-name")
		if !strings.Contains(cs, ":changeSet/") {
			return deny("execute the change set by its ARN (--change-set-name arn:aws:cloudformation:…:changeSet/…), which `tq pack cloud show` prints", "no-approval")
		}
		for _, id := range l.candidates(env) {
			if r, ok := readRecord(g, id); ok {
				build(r)
			}
		}
	default:
		return deny(iv.Tool+" has no plan binding in an approve environment; create a change set", "no-approval")
	}
	entries, err := l.read()
	if err != nil {
		return deny("the approval ledger cannot be read ("+err.Error()+")", "no-approval")
	}
	// Only answers signed with the workspace's keychain key count; without
	// the key nothing does (fail closed).
	trust := l.trust()
	for _, c := range cands {
		if r := Drift(gt, env, ident, c.rec.Account, g.Manifest()); r != "" {
			lastErr = errors.New("identity drift: " + r)
			continue
		}
		a, err := approval.Find(forPlan(entries, c.rec.ID), trust, approval.Binding{Pack: "cloud", Action: c.action, Subject: c.rec.SubjectFor(c.stack), Target: env}, c.ev, approval.Now(), l.claimed)
		if err == nil {
			v := verdict{action: policy.Allow, consume: a.Decision, counter: "apply_allowed:approve"}
			if spec, ok := ReadSpecs[iv.Tool]; ok {
				if _, name, _ := savedPlan(iv, g.StateDir()); name != "" && name == c.rec.PlanFile {
					// build hashed it with HashState in this call
					v.readName, v.readSpec = name, spec
				}
			}
			return v
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no approval was asked for %s to %s", action, env)
	}
	return deny(lastErr.Error(), deniedWhy(lastErr))
}

func planFileFor(r plans.Record, stack string) string { return r.PlanFile }

// forPlan keeps the ledger lines of the decisions asked for plan id, so
// the error reported is that approval's, not another plan's.
func forPlan(entries []ledger.Entry, id string) []ledger.Entry {
	keep := map[string]bool{}
	for _, e := range entries {
		if e.Event == ledger.Asked && e.Evidence[plans.EvPlanID] == id {
			keep[e.Decision] = true
		}
	}
	var out []ledger.Entry
	for _, e := range entries {
		if keep[e.Decision] {
			out = append(out, e)
		}
	}
	return out
}

func deniedWhy(err error) string {
	switch {
	case errors.Is(err, approval.ErrExpired):
		return "expired"
	case errors.Is(err, approval.ErrUsed):
		return "used"
	case errors.Is(err, approval.ErrEvidence):
		return "hash-mismatch"
	case strings.HasPrefix(err.Error(), "identity drift"):
		return "identity-drift"
	}
	return "no-approval"
}

// IdentityDrift is the tq/iac-identity-drift guard (§9.6): an apply or
// destroy whose subscription, profile or recorded AWS account disagrees
// with its environment or the manifest is denied.
func IdentityDrift(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	if !call.IsShell() || strings.TrimSpace(call.Command) == "" {
		return policy.Allow, "", false
	}
	gt := envgate.FromManifest(envgate.Cloud, g.Manifest(), g.Config())
	h := hostFor(g)
	for _, iv := range AnalyzeShellAt(call.Command, call.Tool == "PowerShell", baseDir(g)) {
		if iv.Tool == "" || (iv.Op != OpApply && iv.Op != OpDestroy) {
			continue
		}
		action := envgate.Apply
		if iv.Op == OpDestroy {
			action = envgate.Destroy
		}
		var env string
		if iv.Unknown {
			env = gt.ResolveFloor(action, Signals(iv, h)).Env
		} else {
			env = gt.Resolve(action, Signals(iv, h)).Env
		}
		id := ResolveIdentity(iv, h, len(gt.Match(env)["subscriptions"]) > 0)
		// An unknown identity is not drift by itself: tq/iac-apply-env floors
		// the environment and denies it in an approve environment.
		account := ""
		if r, ok := savedPlanRecord(iv, g); ok {
			account = r.Account
		}
		if r := Drift(gt, env, id, account, g.Manifest()); r != "" {
			g.Count("apply_denied:identity-drift", 1)
			return policy.Deny, fmt.Sprintf("%s: %s to environment %s: %s. Switch the identity (tq doctor --verify shows what is logged in) or target the right environment", IDDrift, describe(iv, iv.Op), env, r), false
		}
	}
	return policy.Allow, "", false
}
