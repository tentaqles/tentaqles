package cmd

import (
	"context"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/commitscan"
	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

const maxPushCommits = 200

const pushTooLarge = "the unpushed commits are too large to scan for personal data; push smaller batches or confirm"
const pushScanFailed = "could not scan the commits about to be pushed; retry or push smaller batches"
const pushTimedOut = "the push " + scanTimedOut + "; retry, or push smaller batches"

// pushGit runs git under ctx and reads at most limit bytes of stdout. The push
// and commit rules both go through it; tests replace it to simulate failures
// and slow git.
var pushGit = runGitBounded

type pushPlan struct {
	remote   string
	specs    []string
	all      bool
	mirror   bool
	tags     bool
	skipThis bool // --delete or --dry-run
}

var pushValueFlags = map[string]bool{"-o": true, "--push-option": true, "--repo": true, "--receive-pack": true, "--exec": true}

func parsePush(args []string) pushPlan {
	var p pushPlan
	var pos []string
	for i := 0; i < len(args); i++ {
		a := strings.Trim(args[i], `"'`)
		switch {
		case a == "--delete" || a == "-d" || a == "--dry-run" || a == "-n":
			p.skipThis = true
		case a == "--all" || a == "--branches":
			p.all = true
		case a == "--mirror":
			p.mirror = true
		case a == "--tags":
			p.tags = true
		case pushValueFlags[a]:
			i++
		case strings.HasPrefix(a, "-"):
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) > 0 {
		p.remote = pos[0]
		for i := 1; i < len(pos); i++ {
			if pos[i] == "tag" && i+1 < len(pos) {
				i++ // `tag <name>` pushes a tag
				continue
			}
			p.specs = append(p.specs, pos[i])
		}
	}
	return p
}

type refPair struct{ src, dst string }

// pushScanner carries the shared deadline for one push decision.
type pushScanner struct {
	ctx    context.Context
	cwd    string
	budget int
}

func (s *pushScanner) git(limit int, args ...string) (string, bool, error) {
	return pushGit(s.ctx, s.cwd, limit, args...)
}

// line returns the trimmed output of a small git query, or "".
func (s *pushScanner) line(args ...string) string {
	out, _, err := s.git(1<<20, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// sources derives which local refs a push would send. ask is non-empty when
// the push cannot be bounded.
func (s *pushScanner) sources(p pushPlan) (pairs []refPair, ask string) {
	if p.mirror {
		return nil, "this push is unbounded (--mirror); push named branches"
	}
	cur := s.line("symbolic-ref", "-q", "--short", "HEAD")
	allBranches := func() {
		for _, b := range strings.Split(s.line("for-each-ref", "--format=%(refname:short)", "refs/heads"), "\n") {
			if b = strings.TrimSpace(b); b != "" {
				pairs = append(pairs, refPair{b, b})
			}
		}
	}
	if p.all {
		allBranches()
		return pairs, ""
	}
	if len(p.specs) == 0 {
		if p.tags {
			return nil, "" // tags only: no new content beyond commits scanned when their branch is pushed
		}
		if cur == "" {
			return []refPair{{"HEAD", "HEAD"}}, ""
		}
		return []refPair{{cur, cur}}, ""
	}
	for _, spec := range p.specs {
		spec = strings.TrimPrefix(spec, "+")
		if strings.Contains(spec, "*") {
			allBranches()
			continue
		}
		src, dst := spec, spec
		if i := strings.Index(spec, ":"); i >= 0 {
			src, dst = spec[:i], spec[i+1:]
		}
		if src == "" || strings.HasPrefix(src, "refs/tags/") {
			continue // deletion or tag
		}
		if src == "HEAD" && (dst == "HEAD" || dst == "") && cur != "" {
			dst = cur
		}
		src = strings.TrimPrefix(src, "refs/heads/")
		dst = strings.TrimPrefix(dst, "refs/heads/")
		pairs = append(pairs, refPair{src, dst})
	}
	return pairs, ""
}

type rangeStatus int

const (
	rangeUnresolved rangeStatus = iota // no such range: no opinion
	rangeScanned
	rangeTooBig
	rangeFailed // resolved but could not be scanned: ask
)

// scanRange reads the commits in rng one by one (so values added and later
// removed inside the range are still seen).
func (s *pushScanner) scanRange(rng string) ([]commitscan.AddedLine, rangeStatus) {
	cnt, _, err := s.git(1<<16, "rev-list", "--count", rng)
	if err != nil {
		if s.ctx.Err() != nil {
			return nil, rangeFailed
		}
		return nil, rangeUnresolved
	}
	n, err := strconv.Atoi(strings.TrimSpace(cnt))
	if err != nil {
		return nil, rangeFailed
	}
	if n == 0 {
		return nil, rangeScanned
	}
	if n > maxPushCommits || s.budget <= 0 {
		return nil, rangeTooBig
	}
	out, big, err := s.git(s.budget, "log", "-p", "--no-color", "--no-ext-diff", "--no-textconv", "-U0", "--format=%H", rng)
	if big {
		return nil, rangeTooBig
	}
	if err != nil {
		return nil, rangeFailed
	}
	s.budget -= len(out)
	return commitscan.ParseDiffAdded(out), rangeScanned
}

// pushPIIDecision runs under ctx, the invocation's shared scan budget.
func pushPIIDecision(ctx context.Context, ws *resolve.Workspace, call policy.ToolCall, cwd string, action policy.Action) (policy.Decision, bool) {
	if !call.IsShell() {
		return policy.Decision{}, false
	}
	sc := &pushScanner{ctx: ctx, cwd: cwd, budget: maxPushScan}
	var lines []commitscan.AddedLine
	var ask string
	setAsk := func(m string) {
		if ask == "" {
			ask = m
		}
	}
scan:
	for _, inv := range guard.GitInvocations(guard.StripHeredocs(call.Command)) {
		if inv.Sub != "push" {
			continue
		}
		plan := parsePush(inv.Args)
		if plan.skipThis {
			continue
		}
		pairs, a := sc.sources(plan)
		if a != "" {
			setAsk(a)
		}
		if ctx.Err() != nil {
			setAsk(pushScanFailed)
			break
		}
		remote := plan.remote
		if remote == "" {
			remote = "origin"
		}
		for _, pr := range pairs {
			for _, rng := range []string{
				remote + "/" + pr.dst + ".." + pr.src,
				pr.src + "@{push}.." + pr.src,
				pr.src + "@{upstream}.." + pr.src,
				"origin/HEAD.." + pr.src,
			} {
				got, st := sc.scanRange(rng)
				switch st {
				case rangeUnresolved:
					continue
				case rangeTooBig:
					setAsk(pushTooLarge)
				case rangeFailed:
					setAsk(pushScanFailed)
					break scan
				}
				lines = append(lines, got...)
				break
			}
		}
	}
	h, markerIgnored, herr := hits(ctx, lines, piiClasses(ws))
	switch {
	case herr != nil && ctx.Err() != nil:
		return ruleDecision(policy.Ask, "tq/push-pii", pushTimedOut), true
	case herr != nil:
		return ruleDecision(policy.Ask, "tq/push-pii", pushTooLarge), true
	}
	if ctx.Err() != nil {
		return ruleDecision(policy.Ask, "tq/push-pii", pushTimedOut), true
	}
	if len(h) > 0 {
		reason := "the commits about to be pushed record personal or financial data: " + strings.Join(h, ", ") +
			". Rewrite those commits locally (remove the values, amend or rebase) before pushing"
		if markerIgnored {
			reason += ". " + markerScopeNote
		}
		return ruleDecision(action, "tq/push-pii", reason), true
	}
	if ask != "" {
		return ruleDecision(policy.Ask, "tq/push-pii", ask), true
	}
	return policy.Decision{}, false
}
