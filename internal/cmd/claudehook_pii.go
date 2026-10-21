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

// piiClasses are the classes the exit rules scan for: the manifest's
// data.classes (secrets excluded: tq/commit-secret owns those) plus
// "checksummed". redact.Resolve, shared with `tq redact`, maps them to
// detectors, so the checksummed ones always run.
func piiClasses(ws *resolve.Workspace) []string {
	out := []string{"checksummed"}
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Data.Enabled() {
		return out
	}
	for _, c := range ws.Manifest.Data.Classes {
		if c != "secrets" {
			out = append(out, c)
		}
	}
	return out
}

// fixtureDirRe names test and fixture directories. It is the single
// definition behind the tq:allow-pii marker scope and the fixture-dump
// targets, so the two cannot drift.
var fixtureDirRe = regexp.MustCompile(`(?i)(^|/)(tests?|fixtures|__fixtures__|seeds?|seed|testdata)(/|$)`)

const markerScopeNote = "tq:allow-pii only applies inside test and fixture directories"

func slashPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return p
}

func inFixtureDir(file string) bool {
	f := slashPath(file)
	if i := strings.LastIndex(f, "/"); i >= 0 {
		return fixtureDirRe.MatchString(f[:i+1])
	}
	return false
}

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

var (
	ghTextRe = regexp.MustCompile(`(?i)\bgh\s+(pr|issue|release)\s+(create|edit|comment|review|merge|close)\b`)
	ghAPIRe  = regexp.MustCompile(`(?i)\bgh\s+api\b`)
)

// ghAPIText reports whether a gh api segment sends text: a -f/-F/--field/
// --raw-field value or an --input body.
func ghAPIText(seg string) bool {
	if !ghAPIRe.MatchString(seg) {
		return false
	}
	toks, _ := tokenize(seg)
	for _, t := range toks {
		switch {
		case t == "-f" || t == "-F" || t == "--field" || t == "--raw-field" || t == "--input",
			strings.HasPrefix(t, "--field=") || strings.HasPrefix(t, "--raw-field=") || strings.HasPrefix(t, "--input="),
			len(t) > 2 && (strings.HasPrefix(t, "-f") || strings.HasPrefix(t, "-F")) && !strings.HasPrefix(t, "--"):
			return true
		}
	}
	return false
}

// ghAPIFileArgs returns the files a gh api call reads its text from:
// `-F key=@file` / `--field key=@file` and `--input file` ("-" is stdin).
// -f/--raw-field values are literal and already part of the command text.
func ghAPIFileArgs(toks []string) []string {
	var out []string
	field := func(v string) {
		if i := strings.Index(v, "=@"); i >= 0 {
			out = append(out, v[i+2:])
		}
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case (t == "-F" || t == "--field") && i+1 < len(toks):
			field(toks[i+1])
			i++
		case strings.HasPrefix(t, "--field="):
			field(strings.TrimPrefix(t, "--field="))
		case strings.HasPrefix(t, "-F") && len(t) > 2:
			field(t[2:])
		case t == "--input" && i+1 < len(toks):
			out = append(out, toks[i+1])
			i++
		case strings.HasPrefix(t, "--input="):
			out = append(out, strings.TrimPrefix(t, "--input="))
		case (t == "-f" || t == "--raw-field") && i+1 < len(toks):
			i++
		}
	}
	return out
}

var heredocSubstRe = regexp.MustCompile(`^\$\(\s*cat\s+<<`)

// hasSubstitution reports whether seg builds text with a command
// substitution tq cannot evaluate: `$(...)` outside single quotes (except
// the `$(cat <<'EOF' ... EOF)` form, whose body is in the command text), or
// in Bash a backtick outside single quotes. Heredoc bodies are data and are
// not looked at; in PowerShell a backtick is an escape.
func hasSubstitution(seg string, bash bool) bool {
	s := guard.BlankHeredocBodies(seg)
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case c == '\\' && bash && i+1 < len(s):
			i++
		case c == '\'' && !inDouble:
			inSingle = true
		case c == '"':
			inDouble = !inDouble
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			if !heredocSubstRe.MatchString(s[i:]) {
				return true
			}
		case c == '`' && bash:
			return true
		}
	}
	return false
}

func textRelevant(seg string) bool {
	if ghTextRe.MatchString(seg) || ghAPIText(seg) {
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
	if p == "-" || p == "" {
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
		if hasSubstitution(sg.Text, call.Tool != "PowerShell") {
			asks = append(asks, "the text is built by a command substitution, which tq cannot scan; write it to a file and pass --body-file / -F <file>")
		}
		files := bodyFileArgs(toks)
		if ghAPIText(sg.Text) {
			files = ghAPIFileArgs(toks)
		}
		for _, p := range files {
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
		out = append(out, psWriteTargets(toks)...)
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

// psFileWriters are PowerShell cmdlets (and aliases) whose path parameter,
// or first positional argument, is the file they write.
var psFileWriters = map[string]bool{"set-content": true, "sc": true, "add-content": true, "ac": true,
	"out-file": true, "export-csv": true, "epcsv": true, "tee-object": true, "export-clixml": true}

// psCopiers write to -Destination, or to their second positional argument.
var psCopiers = map[string]bool{"copy-item": true, "cpi": true, "move-item": true, "mi": true, "move": true}

// psValueParams take a value that is not a path.
var psValueParams = []string{"value", "encoding", "delimiter", "inputobject", "width", "stream", "filter",
	"include", "exclude", "credential", "variable", "depth"}

// psParam reports whether tok is a PowerShell parameter naming one of full
// (case-insensitive, abbreviation allowed, -Name:value form accepted) and
// returns its attached value.
func psParam(tok string, full ...string) (ok bool, value string, attached bool) {
	if !strings.HasPrefix(tok, "-") || len(tok) < 3 {
		return false, "", false
	}
	name := strings.ToLower(tok[1:])
	if i := strings.Index(name, ":"); i >= 0 {
		value, attached = tok[1+i+1:], true
		name = name[:i]
	}
	for _, f := range full {
		if strings.HasPrefix(f, name) {
			return true, value, attached
		}
	}
	return false, "", false
}

// psWriteTargets returns the files a PowerShell writer cmdlet writes.
func psWriteTargets(toks []string) []string {
	vi := verbIndex(toks)
	if vi < 0 {
		return nil
	}
	verb := strings.ToLower(baseName(toks[vi]))
	writer, copier := psFileWriters[verb], psCopiers[verb]
	if !writer && !copier {
		return nil
	}
	var out, pos []string
	for j := vi + 1; j < len(toks); j++ {
		t := toks[j]
		take := func(v string, attached bool) {
			if attached {
				out = append(out, v)
			} else if j+1 < len(toks) {
				out = append(out, toks[j+1])
				j++
			}
		}
		if writer {
			if ok, v, att := psParam(t, "path", "filepath", "literalpath"); ok {
				take(v, att)
				continue
			}
		} else {
			if ok, v, att := psParam(t, "destination"); ok {
				take(v, att)
				continue
			}
			if ok, _, att := psParam(t, "path", "literalpath"); ok {
				if !att {
					j++ // the source
				}
				pos = append(pos, "") // a named source takes the first position
				continue
			}
		}
		if ok, _, att := psParam(t, psValueParams...); ok {
			if !att {
				j++
			}
			continue
		}
		if strings.HasPrefix(t, "-") && len(t) > 1 {
			continue // a switch such as -NoTypeInformation or -Force
		}
		pos = append(pos, t)
	}
	switch {
	case writer && len(out) == 0 && len(pos) > 0:
		out = append(out, pos[0])
	case copier && len(out) == 0 && len(pos) > 1:
		out = append(out, pos[1])
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

// foldData is data with its path globs lower-cased, for matching paths
// case-insensitively (Data/Exports names data/exports on Windows and macOS).
func foldData(data manifest.Data) manifest.Data {
	f := data
	f.Paths = make([]manifest.DataPath, len(data.Paths))
	for i, p := range data.Paths {
		p.Path = strings.ToLower(p.Path)
		f.Paths[i] = p
	}
	return f
}

// candidatePaths returns the forms of a path word to match against the
// workspace's classified globs: as written, and resolved from the
// directory the command runs in (cwd, or a leading cd) relative to the
// workspace root, so absolute, ./- and ../-relative spellings all match.
func candidatePaths(root, base, w string) []string {
	out := []string{strings.TrimPrefix(slashPath(w), "/")}
	if base == "" || root == "" {
		return out
	}
	abs, err := guard.ResolvePath(base, w)
	if err != nil {
		return out
	}
	r := strings.ToLower(filepath.ToSlash(filepath.Clean(root))) + "/"
	a := strings.ToLower(filepath.ToSlash(filepath.Clean(abs)))
	if strings.HasPrefix(a, r) {
		out = append(out, a[len(r):])
	}
	return out
}

// classesOf returns the classes of every classified path or table the
// command names. root is the workspace root and base the directory the
// command runs in ("" when unknown: paths are then matched as written).
func classesOf(data manifest.Data, root, base, cmd string) (classes []string, what string) {
	set := map[string]bool{}
	folded := foldData(data)
	for _, w := range pathWords(cmd) {
		for _, cand := range candidatePaths(root, base, w) {
			for _, c := range folded.Classified(strings.ToLower(cand)) {
				set[c] = true
				what = "path " + w
			}
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

func piiFixtureDumpDecision(ws *resolve.Workspace, call policy.ToolCall, cwd string, action policy.Action) (policy.Decision, bool) {
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Data.Enabled() || !call.IsShell() {
		return policy.Decision{}, false
	}
	base, berr := guard.BaseDir(cwd, call.Command)
	if berr != nil {
		base = ""
	}
	cs, what := classesOf(ws.Manifest.Data, ws.Root, base, call.Command)
	targets := writeTargets(call.Command)
	if cs == nil {
		if berr != nil {
			// the directory is unknown, so a relative source path cannot be
			// matched: ask when the command writes into a fixture directory
			for _, tgt := range targets {
				if fixtureDirRe.MatchString(slashPath(tgt)) {
					return ruleDecision(policy.Ask, "tq/pii-fixture-dump",
						"this command writes into "+tgt+" from a directory tq cannot resolve ("+berr.Error()+"); confirm the source is not classified data"), true
				}
			}
		}
		return policy.Decision{}, false
	}
	for _, tgt := range targets {
		if fixtureDirRe.MatchString(slashPath(tgt)) {
			return ruleDecision(action, "tq/pii-fixture-dump",
				fmt.Sprintf("this copies rows from %s (classified %s) into %s. Fixtures and seeds must be synthetic: generate them "+
					"(Faker or a factory) or pseudonymise through a script that never writes real values. See docs/privacy.md.",
					what, strings.Join(cs, ","), tgt)), true
		}
	}
	return policy.Decision{}, false
}
