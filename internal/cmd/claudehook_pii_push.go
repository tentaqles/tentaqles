package cmd

import (
	"context"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/commitscan"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

const maxPushCommits = 200

const pushTooLarge = "the unpushed commits are too large to scan for personal data; push smaller batches or confirm"

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

func gitLine(cwd string, args ...string) string {
	out, err := gitcfg.RunGitIn(cwd, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// pushSources derives which local refs a push would send. ask is non-empty
// when the push cannot be bounded.
func pushSources(cwd string, p pushPlan) (pairs []refPair, ask string) {
	if p.mirror {
		return nil, "this push is unbounded (--mirror); push named branches"
	}
	cur := gitLine(cwd, "symbolic-ref", "-q", "--short", "HEAD")
	allBranches := func() {
		for _, b := range strings.Split(gitLine(cwd, "for-each-ref", "--format=%(refname:short)", "refs/heads"), "\n") {
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

// gitBounded runs git with a deadline and reads at most limit bytes of stdout.
func gitBounded(cwd string, limit int, args ...string) (out string, tooBig bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "git", args...)
	c.Dir = cwd
	pipe, err := c.StdoutPipe()
	if err != nil {
		return "", false, err
	}
	if err := c.Start(); err != nil {
		return "", false, err
	}
	raw, _ := io.ReadAll(io.LimitReader(pipe, int64(limit)+1))
	if len(raw) > limit {
		cancel()
		_ = c.Wait()
		return "", true, nil
	}
	if err := c.Wait(); err != nil {
		return "", false, err
	}
	return string(raw), false, nil
}

// scanRange reads the commits in rng one by one (so values added and later
// removed inside the range are still seen). ok is false when rng does not resolve.
func scanRange(cwd, rng string, budget *int) (lines []commitscan.AddedLine, tooBig, ok bool) {
	cnt, err := gitcfg.RunGitIn(cwd, "rev-list", "--count", rng)
	if err != nil {
		return nil, false, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(cnt))
	if err != nil {
		return nil, false, false
	}
	if n == 0 {
		return nil, false, true
	}
	if n > maxPushCommits || *budget <= 0 {
		return nil, true, true
	}
	out, big, err := gitBounded(cwd, *budget, "log", "-p", "--no-color", "--no-ext-diff", "-U0", "--format=%H", rng)
	if big {
		return nil, true, true
	}
	if err != nil {
		return nil, false, false
	}
	*budget -= len(out)
	file := ""
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, "\r")
		switch {
		case strings.HasPrefix(l, "+++ "):
			file = strings.TrimPrefix(strings.Trim(strings.TrimSpace(strings.TrimPrefix(l, "+++ ")), `"`), "b/")
		case strings.HasPrefix(l, "+") && file != "/dev/null":
			lines = append(lines, commitscan.AddedLine{File: file, Text: l[1:]})
		}
	}
	return lines, false, true
}

func pushPIIDecision(ws *resolve.Workspace, call policy.ToolCall, cwd string, action policy.Action) (policy.Decision, bool) {
	if !call.IsShell() {
		return policy.Decision{}, false
	}
	budget := maxPushScan
	var lines []commitscan.AddedLine
	var ask string
	for _, inv := range guard.GitInvocations(guard.StripHeredocs(call.Command)) {
		if inv.Sub != "push" {
			continue
		}
		plan := parsePush(inv.Args)
		if plan.skipThis {
			continue
		}
		pairs, a := pushSources(cwd, plan)
		if a != "" && ask == "" {
			ask = a
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
				got, big, ok := scanRange(cwd, rng, &budget)
				if !ok {
					continue
				}
				if big && ask == "" {
					ask = pushTooLarge
				}
				lines = append(lines, got...)
				break
			}
		}
	}
	h, markerIgnored := hits(lines, piiClasses(ws))
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
