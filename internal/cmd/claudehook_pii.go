package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/commitscan"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/redact"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Exit guards for personal data (spec §11.2, revised): reading and processing
// personal data locally is allowed; recording it in git or publishing it in
// PR and issue text is not. Values never appear in reasons.

const maxPushScan = 4 << 20
const maxTextFile = 256 << 10

func piiClasses(ws *resolve.Workspace) []string {
	out := []string{"checksummed"}
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Data.Enabled() {
		return out
	}
	pii, fin := false, false
	for _, c := range ws.Manifest.Data.Classes {
		switch c {
		case "pii", "sensitive", "children":
			pii = true
		case "financial":
			fin = true
		}
	}
	if pii {
		out = append(out, "pii")
	}
	if fin {
		out = append(out, "financial")
	}
	return out
}

// hits formats findings as "file:line (detector)" without values.
func hits(lines []commitscan.AddedLine, classes []string) []string {
	var out []string
	for _, l := range lines {
		for name := range redact.Count(l.Text, classes) {
			if l.Line > 0 {
				out = append(out, fmt.Sprintf("%s:%d (%s)", l.File, l.Line, name))
			} else {
				out = append(out, fmt.Sprintf("%s (%s)", l.File, name))
			}
		}
	}
	sort.Strings(out)
	out = dedupeSorted(out)
	if len(out) > 10 {
		out = append(out[:10], fmt.Sprintf("... and %d more", len(out)-10))
	}
	return out
}

func dedupeSorted(s []string) []string {
	var out []string
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func commitPIIDecision(ws *resolve.Workspace, call policy.ToolCall, cwd string, action policy.Action) (policy.Decision, bool) {
	if !call.IsShell() || !commitscan.PlanFor(call.Command).Commits {
		return policy.Decision{}, false
	}
	h := hits(commitscan.Added(cwd, call.Command, gitcfg.RunGitIn), piiClasses(ws))
	if len(h) == 0 {
		return policy.Decision{}, false
	}
	return ruleDecision(action, "tq/commit-pii", "this commit would record personal or financial data: "+strings.Join(h, ", ")+
		". Keep real values out of the repository (synthetic fixtures, environment variables, external storage); mark a known-synthetic line with `tq:allow-pii`"), true
}

func pushPIIDecision(ws *resolve.Workspace, call policy.ToolCall, cwd string, action policy.Action) (policy.Decision, bool) {
	if !call.IsShell() {
		return policy.Decision{}, false
	}
	isPush := false
	for _, inv := range guard.GitInvocations(guard.StripHeredocs(call.Command)) {
		if inv.Sub != "push" {
			continue
		}
		skip := false
		for _, a := range inv.Args {
			if a == "--delete" || a == "-d" || a == "--dry-run" || a == "-n" {
				skip = true
			}
		}
		if !skip {
			isPush = true
		}
	}
	if !isPush {
		return policy.Decision{}, false
	}
	var diff string
	resolved := false
	for _, rng := range []string{"@{push}..HEAD", "@{upstream}..HEAD", "origin/HEAD..HEAD"} {
		out, err := gitcfg.RunGitIn(cwd, "diff", "--no-color", "--no-ext-diff", "-U0", rng)
		if err == nil {
			diff = out
			resolved = true
			break
		}
	}
	if !resolved || diff == "" {
		return policy.Decision{}, false // nothing to compare against: no opinion
	}
	if len(diff) > maxPushScan {
		return ruleDecision(policy.Ask, "tq/push-pii", "the unpushed diff is too large to scan for personal data; push smaller batches or confirm"), true
	}
	var lines []commitscan.AddedLine
	file := ""
	for _, l := range strings.Split(diff, "\n") {
		l = strings.TrimRight(l, "\r")
		switch {
		case strings.HasPrefix(l, "+++ "):
			file = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
		case strings.HasPrefix(l, "+"):
			lines = append(lines, commitscan.AddedLine{File: file, Text: l[1:]})
		}
	}
	h := hits(lines, piiClasses(ws))
	if len(h) == 0 {
		return policy.Decision{}, false
	}
	return ruleDecision(action, "tq/push-pii", "the commits about to be pushed record personal or financial data: "+strings.Join(h, ", ")+
		". Rewrite those commits locally (remove the values, amend or rebase) before pushing"), true
}

var ghTextRe = regexp.MustCompile(`(?i)\bgh\s+(pr|issue|release)\s+(create|edit|comment|review|merge|close)\b`)
var textFileFlag = regexp.MustCompile(`(?i)\s(--body-file|--notes-file|--file|-F)[\s=]+("([^"]+)"|'([^']+)'|(\S+))`)

func textPIIDecision(ws *resolve.Workspace, call policy.ToolCall, cwd string, action policy.Action) (policy.Decision, bool) {
	if !call.IsShell() {
		return policy.Decision{}, false
	}
	relevant := ghTextRe.MatchString(call.Command)
	for _, inv := range guard.GitInvocations(guard.StripHeredocs(call.Command)) {
		if inv.Sub == "commit" || inv.Sub == "tag" || inv.Sub == "notes" {
			relevant = true
		}
	}
	if !relevant {
		return policy.Decision{}, false
	}
	classes := piiClasses(ws)
	var where []string
	for name := range redact.Count(call.Command, classes) {
		where = append(where, "command text ("+name+")")
	}
	for _, m := range textFileFlag.FindAllStringSubmatch(call.Command, -1) {
		p := m[3] + m[4] + m[5]
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if len(raw) > maxTextFile {
			raw = raw[:maxTextFile]
		}
		for name := range redact.Count(string(raw), classes) {
			where = append(where, filepath.Base(p)+" ("+name+")")
		}
	}
	if len(where) == 0 {
		return policy.Decision{}, false
	}
	sort.Strings(where)
	return ruleDecision(action, "tq/text-pii", "this text would publish personal or financial data: "+strings.Join(where, ", ")+
		". Describe the case without the values (a ticket id, a role, a masked sample)"), true
}

var (
	fromTableRe   = regexp.MustCompile(`(?i)(?:\b(?:from|join|into|table|update)\s+|\s(?:-t|--table)[\s=]+)("?[a-z_][\w]*"?(?:\."?[a-z_][\w]*"?)?)`)
	fixtureDirRe  = regexp.MustCompile(`(?i)(^|[\s'"=])\.?/?((tests?|fixtures|__fixtures__|seeds?|testdata)/)`)
	writeTargetRe = regexp.MustCompile(`(?i)(>{1,2}\s*|\s(-o|-f|--out|--output|--file)[\s=]|\bto\s+'?)([^\s'";&|]+)`)
	copyToRe      = regexp.MustCompile(`(?i)\\?copy\b[^;]*\bto\s+'?([^\s'";]+)`)
	pgDumpFileRe  = regexp.MustCompile(`(?i)\bpg_dump\b[^;|&]*\s(-f|--file)[\s=]([^\s'"]+)`)
)

// pathWords returns slash-normalised words of a command that look like paths.
func pathWords(cmd string) []string {
	var out []string
	for _, w := range strings.Fields(cmd) {
		w = strings.Trim(w, `"'`)
		if strings.ContainsAny(w, `/\`) || strings.Contains(w, ".") {
			out = append(out, strings.ReplaceAll(w, `\`, "/"))
		}
	}
	return out
}

// classesOf returns the classes of every classified path or table the command names.
func classesOf(data manifest.Data, cmd string) (classes []string, what string) {
	set := map[string]bool{}
	for _, w := range pathWords(cmd) {
		for _, c := range data.Classified(w) {
			set[c] = true
			what = "path " + w
		}
	}
	tables := data.Tables()
	for _, m := range fromTableRe.FindAllStringSubmatch(cmd, -1) {
		name := strings.ToLower(strings.ReplaceAll(m[1], `"`, ""))
		if cs, ok := tables[name]; ok {
			for _, c := range cs {
				set[c] = true
			}
			what = "table " + name
		}
	}
	for c := range set {
		classes = append(classes, c)
	}
	sort.Strings(classes)
	return classes, what
}

func piiFixtureDumpDecision(ws *resolve.Workspace, call policy.ToolCall, action policy.Action) (policy.Decision, bool) {
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Data.Enabled() || !call.IsShell() {
		return policy.Decision{}, false
	}
	cs, what := classesOf(ws.Manifest.Data, call.Command)
	if cs == nil {
		return policy.Decision{}, false
	}
	var targets []string
	for _, m := range writeTargetRe.FindAllStringSubmatch(call.Command, -1) {
		targets = append(targets, m[3])
	}
	for _, m := range copyToRe.FindAllStringSubmatch(call.Command, -1) {
		targets = append(targets, m[1])
	}
	for _, m := range pgDumpFileRe.FindAllStringSubmatch(call.Command, -1) {
		targets = append(targets, m[2])
	}
	if f := strings.Fields(strings.TrimSpace(call.Command)); len(f) >= 3 && (f[0] == "cp" || strings.EqualFold(f[0], "copy")) {
		targets = append(targets, f[len(f)-1])
	}
	for _, tgt := range targets {
		if fixtureDirRe.MatchString(" " + strings.ReplaceAll(tgt, `\`, "/")) {
			return ruleDecision(action, "tq/pii-fixture-dump",
				fmt.Sprintf("this copies rows from %s (classified %s) into %s. Fixtures and seeds must be synthetic: generate them "+
					"(Faker or a factory) or pseudonymise through a script that never writes real values. See docs/privacy.md.",
					what, strings.Join(cs, ","), tgt)), true
		}
	}
	return policy.Decision{}, false
}
