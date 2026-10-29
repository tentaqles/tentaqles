package cmd

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// The hook-time halves of tq/hub-store-write, tq/human-answer and
// tq/pack-config-change (the static halves are in
// internal/policy/builtin_framework.go). They are built from the real state
// directory at hook time (paths.Home(), its final path and its 8.3 short
// form), so a custom TQ_HOME, a short name, a junction or a quoting trick
// does not slip past a fixed regex. All three ids are never-disable.

// normShell folds a command for mention matching: lower case, forward
// slashes, quotes, backticks and glob brackets removed, /./ and // collapsed.
func normShell(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, `\`, "/")
	s = strings.NewReplacer(`"`, "", `'`, "", "`", "", "[", "", "]", "").Replace(s)
	for {
		n := strings.ReplaceAll(strings.ReplaceAll(s, "/./", "/"), "//", "/")
		if n == s {
			break
		}
		s = n
	}
	return s
}

// normPathForm is a path in the normShell form.
func normPathForm(p string) string {
	if p == "" {
		return ""
	}
	return strings.TrimSuffix(normShell(filepath.Clean(p)), "/")
}

// stateDirForms are the spellings of tq's state directory a command may use.
func stateDirForms() []string {
	home := paths.Home()
	set := map[string]bool{".tentaqles": true, "tentaq~": true, "$tq_home": true, "${tq_home}": true, "%tq_home%": true, "$env:tq_home": true}
	var dirs []string
	add := func(p string) {
		if p != "" {
			dirs = append(dirs, p)
		}
	}
	add(home)
	if abs, err := filepath.Abs(home); err == nil {
		add(abs)
		if real, err := packs.FinalPath(abs); err == nil {
			add(real)
		}
		add(shortPath(abs))
	}
	uh, _ := os.UserHomeDir()
	for _, d := range dirs {
		n := normPathForm(d)
		if n == "" {
			continue
		}
		set[n] = true
		// Cygwin and git-bash spellings of a drive path: /c/users/..., /mnt/c/...
		if len(n) > 2 && n[1] == ':' {
			set["/"+n[:1]+n[2:]] = true
			set["/mnt/"+n[:1]+n[2:]] = true
		}
		if uh != "" {
			if rel, err := filepath.Rel(uh, d); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
				r := normShell(filepath.ToSlash(rel))
				for _, h := range []string{"~/", "$home/", "${home}/", "$env:userprofile/", "%userprofile%/", "$env:home/"} {
					set[h+r] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

var bareTq = regexp.MustCompile(`^\s*(\S*/)?tq(\.exe)?\s`)

// bareTqCommand: one tq invocation and nothing else (no chain, pipe,
// redirect or substitution).
func bareTqCommand(n string) bool {
	return bareTq.MatchString(n) && !strings.ContainsAny(n, ";&|<>\n`") && !strings.Contains(n, "$(")
}

// protectedSubs are the parts of tq's state directory whose integrity a
// gate relies on: the hub store and its serve info (hub/), the pack
// ledgers, claims, plans and state dirs (packs/), the build gate's records
// and HMAC key (build-gate/), the manifest trust store (trust/) and the
// global guard settings (guard.yaml). The rest (identities/<id>/claude/...,
// bundles, logs) is the agents' normal working area and stays open.
var protectedSubs = []string{"hub", "packs", "build-gate", "trust", "guard.yaml"}

func isProtectedSeg(seg string) bool {
	for _, p := range protectedSubs {
		if seg == p {
			return true
		}
	}
	return false
}

// rootRegex matches any spelling of the state directory root.
func rootRegex() *regexp.Regexp {
	forms := stateDirForms()
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	alts := []string{`tentaq~[0-9]+`}
	for _, f := range forms {
		if f != "" && f != "tentaq~" {
			alts = append(alts, regexp.QuoteMeta(f))
		}
	}
	return regexp.MustCompile("(" + strings.Join(alts, "|") + ")")
}

var (
	protectedWord = regexp.MustCompile(`(^|[/,\s(=:;])(hub|packs|build-gate|trust|guard\.yaml)([/,\s);]|$)`)
	cdRe          = regexp.MustCompile(`(^|[;&|(\s])(cd|pushd|chdir|set-location|sl)\s+(\S+)`)
	linkVerb      = regexp.MustCompile(`\b(ln|mklink|new-item\b.*-itemtype\s+(junction|symboliclink|hardlink)|cmd\s+/c\s+mklink)\b`)
	wordSplit     = regexp.MustCompile(`[\s;&|()<>=,]+`)
)

func firstSeg(s string) string {
	if i := strings.IndexAny(s, "/ \t;&|)<>,"); i >= 0 {
		return s[:i]
	}
	return s
}

// protectedMention reports whether a folded command can reach a protected
// part of the state directory: it names one (any spelling of the root
// followed by a protected or a glob segment); it names the bare root and a
// protected name elsewhere (a variable or a link: D=~/.tentaqles; …
// $D/packs); it creates a link to the root; or it cds under the root and
// then uses a relative path that resolves into a protected part.
func protectedMention(n string) bool {
	re := rootRegex()
	bare := false
	for _, l := range re.FindAllStringIndex(n, -1) {
		rest := n[l[1]:]
		if strings.HasPrefix(rest, "/") {
			seg := firstSeg(rest[1:])
			if seg == "" || isProtectedSeg(seg) || strings.ContainsAny(seg, "*?{") {
				if seg == "" {
					bare = true
					continue
				}
				return true
			}
			continue
		}
		bare = true
	}
	if bare && (protectedWord.MatchString(n) || linkVerb.MatchString(n)) {
		return true
	}
	for _, m := range cdRe.FindAllStringSubmatchIndex(n, -1) {
		target := n[m[6]:m[7]]
		loc := re.FindStringIndex(target)
		if loc == nil || loc[0] != 0 {
			continue
		}
		rel := strings.Trim(target[loc[1]:], "/")
		for _, w := range wordSplit.Split(n[m[1]:], -1) {
			w = strings.TrimPrefix(w, "./")
			if w == "" || strings.HasPrefix(w, "-") || strings.Contains(w, "://") {
				continue
			}
			full := strings.TrimPrefix(path.Join(rel, w), "/")
			if path.IsAbs(w) || strings.HasPrefix(w, "~") || strings.HasPrefix(w, "$") {
				continue
			}
			if isProtectedSeg(firstSeg(full)) {
				return true
			}
		}
	}
	return false
}

// canonicalPath resolves a written path: the nearest existing parent through
// its open handle (packs.FinalPath: junctions, symlinks and 8.3 short names),
// plus the rest, in the normShell form.
func canonicalPath(p, cwd string) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) && cwd != "" {
		p = filepath.Join(cwd, p)
	}
	p = filepath.Clean(p)
	rest := []string{}
	cur := p
	for i := 0; i < 64; i++ {
		if real, err := packs.FinalPath(cur); err == nil {
			return normPathForm(filepath.Join(append([]string{real}, rest...)...))
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
	return normPathForm(p)
}

// underProtected reports whether a canonical path is inside a protected part
// of the state directory (any absolute spelling of the root).
func underProtected(canon string) bool {
	if canon == "" {
		return false
	}
	var roots []string
	for _, f := range stateDirForms() {
		if strings.Contains(f, "/") && !strings.HasPrefix(f, "$") && !strings.HasPrefix(f, "%") && !strings.HasPrefix(f, "~") {
			roots = append(roots, f)
		}
	}
	for _, r := range roots {
		if strings.HasPrefix(canon, r+"/") && isProtectedSeg(firstSeg(canon[len(r)+1:])) {
			return true
		}
	}
	for _, marker := range []string{"/.tentaqles/"} {
		if i := strings.Index(canon, marker); i >= 0 && isProtectedSeg(firstSeg(canon[i+len(marker):])) {
			return true
		}
	}
	if m := regexp.MustCompile(`/tentaq~[0-9]+/`).FindStringIndex(canon); m != nil && isProtectedSeg(firstSeg(canon[m[1]:])) {
		return true
	}
	return false
}

var readOnlyVerb = regexp.MustCompile(`^\s*(cat|type|more|less|head|tail|bat|nl|grep|rg|findstr|gc|get-content|select-string|sls|git\s+(diff|log|show|status|blame))\b`)

// stateDirDecision is the hook-time check of the three never-disable ids.
func stateDirDecision(call policy.ToolCall, cwd string) policy.Decision {
	var d policy.Decision
	deny := func(id, reason string) { d = policy.Merge(d, ruleDecision(policy.Deny, id, reason)) }
	ask := func(id, reason string) { d = policy.Merge(d, ruleDecision(policy.Ask, id, reason)) }
	if call.IsShell() {
		cmd := call.Command
		if call.Tool == "Bash" {
			cmd = guard.BlankDataHeredocs(cmd)
		}
		n := normShell(cmd)
		if !bareTqCommand(n) && protectedMention(n) {
			deny("tq/hub-store-write", "this command reaches a protected part of tq's state directory ("+paths.Home()+": hub, packs, build-gate, trust or guard.yaml), which holds the hub store, the pack ledgers, approvals, plans and keys; only tq itself touches it")
		}
		if (strings.Contains(n, "tq-packs.y") || strings.Contains(n, "tq-pac~")) && !(readOnlyVerb.MatchString(n) && !strings.ContainsAny(n, ">;&|")) {
			ask("tq/pack-config-change", "this command may change the pack config (tq-packs.yaml); confirm it")
		}
		return d
	}
	if call.Path == "" {
		return d
	}
	canon := canonicalPath(call.Path, cwd)
	switch call.Tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		if underProtected(canon) {
			deny("tq/hub-store-write", "this file is in a protected part of tq's state directory ("+paths.Home()+": hub, packs, build-gate, trust or guard.yaml); only tq writes there")
		}
		base := filepath.Base(canon)
		if base == "tq-packs.yaml" || base == "tq-packs.yml" {
			ask("tq/pack-config-change", "this changes the pack config (tq-packs.yaml); confirm it")
		}
	case "Read", "Grep", "NotebookRead", "Glob":
		if underProtected(canon) {
			rel := canon[strings.LastIndex(canon, "/hub/")+1:]
			if strings.Contains(canon, "/hub/") && (strings.HasPrefix(filepath.Base(rel), "serve-") || strings.Contains(filepath.Base(rel), "token")) {
				deny("tq/human-answer", "the hub's serve info holds its local key; it is never read into the conversation")
			}
		}
	}
	return d
}
