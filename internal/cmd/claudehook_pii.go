package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/commitscan"
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

var fixturePathRe = regexp.MustCompile(`(?i)(^|/)(tests?|fixtures|__fixtures__|seeds?|seed|testdata)/`)

const markerScopeNote = "tq:allow-pii only applies inside test and fixture directories"

func slashPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return p
}

func inFixtureDir(file string) bool { return fixturePathRe.MatchString(slashPath(file)) }

// hits formats findings as "file:line (detector)" without values. The
// tq:allow-pii marker exempts a line only inside test and fixture
// directories; markerIgnored reports a marked line that was still a finding.
//
// The scan is bounded: it stops with errScanTooLarge after maxScanBytes and
// with ctx's error when the shared budget ends.
func hits(ctx context.Context, lines []commitscan.AddedLine, classes []string) (out []string, markerIgnored bool, err error) {
	scanned := 0
	for i, l := range lines {
		if i%256 == 0 && ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		if scanned += len(l.Text); scanned > maxScanBytes {
			return nil, false, errScanTooLarge
		}
		var counts map[string]int
		if inFixtureDir(l.File) {
			counts = redact.Count(l.Text, classes)
		} else {
			counts = redact.CountAll(l.Text, classes)
			if len(counts) > 0 && strings.Contains(l.Text, redact.AllowMarker) {
				markerIgnored = true
			}
		}
		for name := range counts {
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
	return out, markerIgnored, nil
}

// maxScanBytes bounds the text one rule scans for personal data.
const maxScanBytes = commitscan.MaxTotalBytes

var errScanTooLarge = errors.New("too large to scan")

// commitScanAsk words the ask for a commit scan that could not finish; it
// completes "the secret ..." / "the personal-data ...".
func commitScanAsk(ctx context.Context, err error) string {
	switch {
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		return scanTimedOut + "; retry, or commit fewer files"
	case commitscan.IsAsk(err):
		return "scan cannot vouch for this commit: " + err.Error()
	case errors.Is(err, errScanTooLarge):
		return fmt.Sprintf("scan stopped: what this commit records is larger than the %d MiB scan limit; commit fewer files", maxScanBytes>>20)
	}
	return "scan could not read what this commit records; check the repository state or confirm"
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

func commitPIIDecision(ctx context.Context, ws *resolve.Workspace, call policy.ToolCall, cwd string, run commitscan.Runner, action policy.Action) (policy.Decision, bool) {
	if !call.IsShell() || run == nil || !commitscan.PlanFor(call.Command).Commits {
		return policy.Decision{}, false
	}
	added, err := commitscan.AddedCtx(ctx, cwd, call.Command, run)
	var h []string
	markerIgnored := false
	if err == nil {
		h, markerIgnored, err = hits(ctx, added, piiClasses(ws))
	}
	if err != nil {
		if !commitscan.IsAsk(err) && ctx.Err() == nil && !errors.Is(err, errScanTooLarge) {
			return ruleDecision(policy.Ask, "tq/commit-pii", "could not scan what this commit records for personal data; check the repository state or confirm"), true
		}
		return ruleDecision(policy.Ask, "tq/commit-pii", "the personal-data "+commitScanAsk(ctx, err)), true
	}
	if len(h) == 0 {
		return policy.Decision{}, false
	}
	reason := "this commit would record personal or financial data: " + strings.Join(h, ", ") +
		". Keep real values out of the repository (synthetic fixtures, environment variables, external storage); mark a known-synthetic line in a test or fixture file with `tq:allow-pii`"
	if markerIgnored {
		reason += ". " + markerScopeNote
	}
	return ruleDecision(action, "tq/commit-pii", reason), true
}

var ghTextRe = regexp.MustCompile(`(?i)\bgh\s+(pr|issue|release)\s+(create|edit|comment|review|merge|close)\b`)

func textRelevant(seg string) bool {
	if ghTextRe.MatchString(seg) {
		return true
	}
	for _, inv := range guard.GitInvocations(guard.StripHeredocs(seg)) {
		if inv.Sub == "commit" || inv.Sub == "tag" || inv.Sub == "notes" {
			return true
		}
	}
	return false
}

var bodyFileFlags = []string{"--body-file", "--notes-file", "--file"}

// bodyFileArgs returns the values of the message-file flags in a segment
// (case-sensitive: -F yes, -f no). "-" means stdin.
func bodyFileArgs(toks []string) []string {
	var out []string
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t == "-F" || t == "--body-file" || t == "--notes-file" || t == "--file" {
			if i+1 < len(toks) {
				out = append(out, toks[i+1])
				i++
			}
			continue
		}
		matched := false
		for _, f := range bodyFileFlags {
			if strings.HasPrefix(t, f+"=") {
				out = append(out, strings.TrimPrefix(t, f+"="))
				matched = true
				break
			}
		}
		if !matched && strings.HasPrefix(t, "-F") && len(t) > 2 {
			out = append(out, strings.TrimPrefix(t[2:], "="))
		}
	}
	return out
}

// scanBodyFile reads a message file with a size cap. ask is non-empty when
// the file cannot be scanned safely.
func scanBodyFile(cwd, p string, classes []string) (found []string, ask string) {
	if p == "-" {
		return nil, "body from stdin cannot be scanned; use --body-file <path>"
	}
	full := p
	if !filepath.IsAbs(full) {
		full = filepath.Join(cwd, full)
	}
	fi, err := os.Stat(full)
	if err != nil {
		return nil, "could not read the body file; write it inside the repository"
	}
	if !fi.Mode().IsRegular() {
		return nil, "cannot scan a non-regular body file"
	}
	if fi.Size() > maxTextFile {
		return nil, "the body file is larger than the scan limit; shorten it"
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, "could not read the body file; write it inside the repository"
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxTextFile))
	if err != nil {
		return nil, "could not read the body file; write it inside the repository"
	}
	for name := range redact.CountAll(string(raw), classes) {
		found = append(found, filepath.Base(full)+" ("+name+")")
	}
	return found, ""
}

func textPIIDecision(ws *resolve.Workspace, call policy.ToolCall, cwd string, action policy.Action) (policy.Decision, bool) {
	if !call.IsShell() {
		return policy.Decision{}, false
	}
	classes := piiClasses(ws)
	var where, asks []string
	anyRelevant := false
	for _, sg := range splitSegments(call.Command) {
		if !textRelevant(sg.Text) {
			continue
		}
		anyRelevant = true
		toks, ok := tokenize(sg.Text)
		if sg.Unbalanced || !ok {
			asks = append(asks, "could not parse the command safely (unbalanced quote); simplify the quoting")
			continue
		}
		for name := range redact.CountAll(sg.Text, classes) {
			where = append(where, "command text ("+name+")")
		}
		for _, p := range bodyFileArgs(toks) {
			found, ask := scanBodyFile(cwd, p, classes)
			where = append(where, found...)
			if ask != "" {
				asks = append(asks, ask)
			}
		}
	}
	if anyRelevant && strings.Contains(call.Command, "<<") {
		// a heredoc body is data on following lines; scan the whole command text
		for name := range redact.CountAll(call.Command, classes) {
			where = append(where, "command text ("+name+")")
		}
	}
	if len(where) > 0 {
		sort.Strings(where)
		return ruleDecision(action, "tq/text-pii", "this text would publish personal or financial data: "+strings.Join(dedupeSorted(where), ", ")+
			". Describe the case without the values (a ticket id, a role, a masked sample)"), true
	}
	if len(asks) > 0 {
		return ruleDecision(policy.Ask, "tq/text-pii", asks[0]), true
	}
	return policy.Decision{}, false
}

var (
	fromTableRe = regexp.MustCompile(`(?i)(?:\b(?:from|join|into|table|update)\s+|\s(?:-t|--table)[\s=]+)("?[a-z_][\w]*"?(?:\."?[a-z_][\w]*"?)?)`)
	copyToRe    = regexp.MustCompile(`(?i)\\?copy\b[^;]*\bto\s+(?:'([^']+)'|"([^"]+)"|([^\s'";]+))`)
)

var copyVerbs = map[string]bool{"cp": true, "mv": true, "rsync": true, "install": true, "tee": true, "copy": true}

// targetDirVerbs honour -t/--target-directory (for rsync, -t is "preserve times").
var targetDirVerbs = map[string]bool{"cp": true, "mv": true, "install": true}

// copyWrappers are words after which a copy verb is the real command; the
// injecting ones (xargs, find -exec) supply the sources, so a lone remaining
// argument is the target.
var copyWrappers = map[string]bool{"sudo": true, "doas": true, "env": true, "nice": true, "command": true, "exec": true,
	"busybox": true, "xargs": true, "-exec": true, "-execdir": true, "-ok": true, "nohup": true, "time": true}

var injectingWrappers = map[string]bool{"xargs": true, "-exec": true, "-execdir": true, "-ok": true}

// wrapperValueFlags take a separate value token after a wrapper.
var wrapperValueFlags = map[string]bool{"-I": true, "-n": true, "-L": true, "-P": true, "-u": true, "-g": true, "-s": true, "-a": true, "-d": true}

var fixtureDirRe = regexp.MustCompile(`(?i)(^|/)(tests?|fixtures|__fixtures__|seeds?|seed|testdata)(/|$)`)

// redirects finds unquoted redirect operators anywhere in the words (>, >>,
// &>, 2>, >|, >&path): it returns their operands and the indexes of the
// words that are an operator or an operand, so callers do not mistake them
// for arguments. A '>' that came from quotes is data, not an operator.
func redirects(ws []word) (targets []string, skip map[int]bool) {
	skip = map[int]bool{}
	for i, w := range ws {
		if len(w.GT) == 0 {
			continue
		}
		rest := strings.TrimLeft(w.Text[w.GT[0]:], ">")
		rest = strings.TrimPrefix(rest, "|")
		skip[i] = true
		if rest == "" && i+1 < len(ws) {
			rest = ws[i+1].Text
			skip[i+1] = true
		}
		if strings.HasPrefix(rest, "&") {
			rest = rest[1:] // >&path writes to path; >&2 and >&- are descriptors
			if rest == "-" || (rest != "" && strings.Trim(rest, "0123456789") == "") {
				rest = ""
			}
		}
		if rest != "" {
			targets = append(targets, rest)
		}
	}
	return targets, skip
}

type verbHit struct {
	idx      int
	injected bool
}

// copyVerbHits finds copy verbs used as the command word or behind wrappers
// (with their options and further wrappers skipped).
func copyVerbHits(toks []string) []verbHit {
	var hits []verbHit
	seen := map[int]bool{}
	add := func(i int, inj bool) {
		if i < len(toks) && copyVerbs[baseName(toks[i])] && !seen[i] {
			seen[i] = true
			hits = append(hits, verbHit{i, inj})
		}
	}
	if vi := verbIndex(toks); vi >= 0 {
		add(vi, false)
	}
	for i, t := range toks {
		if !copyWrappers[strings.ToLower(t)] {
			continue
		}
		inj := false
		j := i
		for j < len(toks) && copyWrappers[strings.ToLower(toks[j])] {
			if injectingWrappers[strings.ToLower(toks[j])] {
				inj = true
			}
			j++
			for j < len(toks) && strings.HasPrefix(toks[j], "-") && len(toks[j]) > 1 && !copyWrappers[strings.ToLower(toks[j])] {
				if wrapperValueFlags[toks[j]] {
					j++
				}
				j++
			}
		}
		add(j, inj)
	}
	return hits
}

// writeTargets lists every place the command writes to: redirects, output
// flags, dump files, COPY ... TO and copy verbs, in any segment.
func writeTargets(cmd string) []string {
	var out []string
	for _, m := range copyToRe.FindAllStringSubmatch(cmd, -1) {
		out = append(out, m[1]+m[2]+m[3])
	}
	for _, sg := range splitSegments(cmd) {
		ws, _ := tokenizeWords(sg.Text)
		toks := make([]string, len(ws))
		for i, w := range ws {
			toks[i] = w.Text
		}
		redirTargets, skip := redirects(ws)
		out = append(out, redirTargets...)
		dump, mysql := false, false
		for _, t := range toks {
			switch baseName(t) {
			case "pg_dump", "pg_dumpall":
				dump = true
			case "mysqldump":
				mysql = true
			}
		}
		for i, t := range toks {
			if skip[i] {
				continue
			}
			names := []string{"-o", "--out", "--output"}
			if dump {
				names = append(names, "-f", "--file")
			}
			if mysql {
				names = append(names, "-r", "--result-file")
			}
			for _, n := range names {
				if t == n && i+1 < len(toks) {
					out = append(out, toks[i+1])
				} else if strings.HasPrefix(t, n+"=") && strings.HasPrefix(n, "--") {
					out = append(out, strings.TrimPrefix(t, n+"="))
				}
			}
		}
		for _, vh := range copyVerbHits(toks) {
			i := vh.idx
			verb := baseName(toks[i])
			var args []string
			dir := ""
			for j := i + 1; j < len(toks); j++ {
				t := toks[j]
				switch {
				case skip[j]:
				case (t == "--target-directory" || (t == "-t" && targetDirVerbs[verb])) && j+1 < len(toks):
					dir = toks[j+1]
					j++
				case strings.HasPrefix(t, "--target-directory="):
					dir = strings.TrimPrefix(t, "--target-directory=")
				case strings.HasPrefix(t, "-") && len(t) > 1:
				case t == `\` || t == ";" || t == "+" || t == "{}":
				default:
					args = append(args, t)
				}
			}
			switch {
			case dir != "":
				out = append(out, dir)
			case verb == "tee":
				out = append(out, args...)
			case len(args) >= 2 || (len(args) == 1 && vh.injected):
				out = append(out, args[len(args)-1])
			}
		}
	}
	return out
}

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
	for _, tgt := range writeTargets(call.Command) {
		if fixtureDirRe.MatchString(slashPath(tgt)) {
			return ruleDecision(action, "tq/pii-fixture-dump",
				fmt.Sprintf("this copies rows from %s (classified %s) into %s. Fixtures and seeds must be synthetic: generate them "+
					"(Faker or a factory) or pseudonymise through a script that never writes real values. See docs/privacy.md.",
					what, strings.Join(cs, ","), tgt)), true
		}
	}
	return policy.Decision{}, false
}
