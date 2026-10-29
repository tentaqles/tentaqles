package gate

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
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

func toolWordUnquoted(iv Invocation) bool {
	for _, w := range iv.Words {
		if !w.Quoted && Tools[ToolName(w.Text)] {
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
	return Host{Getenv: Getenv, ReadLocal: g.ReadLocal, Home: home}
}

// verdict is one leaf's outcome.
type verdict struct {
	action  policy.Action
	reason  string
	consume string
	counter string
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
	leaves := AnalyzeShell(call.Command, call.Tool == "PowerShell")
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
	if len(consumed) > 0 && act != policy.Allow {
		act = policy.Deny
		reasons = append(reasons, "run the approved apply in a command of its own")
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
	if unreadable {
		res = envgate.Resolution{Env: gt.Default()}
	} else {
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
	if !res.Resolved && env != envgate.NoEnv {
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
		return verdict{action: policy.Allow, counter: "apply_allowed:auto"}
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
	g       packs.GuardCtx
	done    bool
	entries []ledger.Entry
	err     error
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
	type cand struct {
		rec    plans.Record
		ev     map[string]string
		stack  string
		action string
	}
	var cands []cand
	var lastErr error
	named := false // the command names a stored plan file
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
		if !named && r.Destroys() {
			// a recorded plan that destroys needs the destroy policy too
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
		named = true
		build(*rec)
	case "pulumi":
		if iv.Op == OpDestroy {
			return deny("pulumi destroy has no plan binding", "no-approval")
		}
		if id, _, named := savedPlan(iv, g.StateDir()); named {
			if id == "" || rec == nil {
				return deny("--plan must name a plan tq recorded", "no-approval")
			}
			named = true
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
	for _, c := range cands {
		if r := Drift(gt, env, ident, c.rec.Account, g.Manifest()); r != "" {
			lastErr = errors.New("identity drift: " + r)
			continue
		}
		a, err := approval.Find(forPlan(entries, c.rec.ID), approval.Binding{Pack: "cloud", Action: c.action, Subject: c.rec.SubjectFor(c.stack), Target: env}, c.ev, approval.Now(), l.claimed)
		if err == nil {
			return verdict{action: policy.Allow, consume: a.Decision, counter: "apply_allowed:approve"}
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
	for _, iv := range AnalyzeShell(call.Command, call.Tool == "PowerShell") {
		if iv.Tool == "" || (iv.Op != OpApply && iv.Op != OpDestroy) {
			continue
		}
		action := envgate.Apply
		if iv.Op == OpDestroy {
			action = envgate.Destroy
		}
		env := gt.Resolve(action, Signals(iv, h)).Env
		id := ResolveIdentity(iv, h, len(gt.Match(env)["subscriptions"]) > 0)
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
