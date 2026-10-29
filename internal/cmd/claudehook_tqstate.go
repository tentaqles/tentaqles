package cmd

import (
	"os"
	"path/filepath"
	"regexp"
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

func mentionsAny(n string, forms []string) bool {
	for _, f := range forms {
		if f != "" && strings.Contains(n, f) {
			return true
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

func underStateDir(canon string) bool {
	if canon == "" {
		return false
	}
	for _, f := range stateDirForms() {
		if strings.HasPrefix(f, "$") || strings.HasPrefix(f, "%") || strings.HasPrefix(f, "~") || !strings.Contains(f, "/") {
			continue
		}
		if canon == f || strings.HasPrefix(canon, f+"/") {
			return true
		}
	}
	return strings.Contains(canon, "/.tentaqles/") || strings.Contains(canon, "/tentaq~")
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
		if !bareTqCommand(n) && mentionsAny(n, stateDirForms()) {
			deny("tq/hub-store-write", "this command names tq's state directory ("+paths.Home()+"), which holds the hub store, the pack ledgers, approvals and plans; only tq itself touches it")
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
		if underStateDir(canon) {
			deny("tq/hub-store-write", "this file is under tq's state directory ("+paths.Home()+"); only tq writes there")
		}
		base := filepath.Base(canon)
		if base == "tq-packs.yaml" || base == "tq-packs.yml" {
			ask("tq/pack-config-change", "this changes the pack config (tq-packs.yaml); confirm it")
		}
	case "Read", "Grep", "NotebookRead", "Glob":
		if underStateDir(canon) {
			rel := canon[strings.LastIndex(canon, "/hub/")+1:]
			if strings.Contains(canon, "/hub/") && (strings.HasPrefix(filepath.Base(rel), "serve-") || strings.Contains(filepath.Base(rel), "token")) {
				deny("tq/human-answer", "the hub's serve info holds its local key; it is never read into the conversation")
			}
		}
	}
	return d
}
