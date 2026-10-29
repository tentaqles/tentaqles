package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// tq/outside-path-write (spec E §5.3, category system) is a hook rule: it
// needs the roots a write may land in, which only the hook knows. Like every
// shell rule it is a heuristic over the command text.

const outsidePathWriteID = guardcfg.OutsidePathWriteID

// writeRoots are the folders a write may land in without a confirm: the
// workspace root, the session cwd, the registry's base folders (the user's
// repos), the temp folders and the agents' own working folders.
func writeRoots(ws *resolve.Workspace, cfg *registry.Config, cwd string) []string {
	var out []string
	add := func(p string) {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	if ws != nil {
		add(ws.Root)
	}
	add(cwd)
	if cfg != nil {
		for _, b := range cfg.Bases {
			add(b)
		}
	}
	add(os.TempDir())
	for _, k := range []string{"TMPDIR", "TEMP", "TMP"} {
		add(os.Getenv(k))
	}
	add("/tmp")
	add("/var/tmp")
	// The agents' own working folders: Claude's auto-memory (under
	// projects/), plans and todos in its config dir, and Codex's sessions
	// and memories. The config dirs themselves are not roots: their
	// settings, rules and hooks files stay guarded.
	home, _ := os.UserHomeDir()
	agentDir := func(name, def string) string {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
		if home == "" {
			return ""
		}
		return filepath.Join(home, def)
	}
	if d := agentDir("CLAUDE_CONFIG_DIR", ".claude"); d != "" {
		for _, sub := range []string{"projects", "plans", "todos"} {
			add(filepath.Join(d, sub))
		}
	}
	if d := agentDir("CODEX_HOME", ".codex"); d != "" {
		for _, sub := range []string{"sessions", "memories"} {
			add(filepath.Join(d, sub))
		}
	}
	return out
}

// outsideWriteTargets returns call's absolute write or recursive-delete
// targets that lie outside every root. File tools count by their path.
func outsideWriteTargets(call policy.ToolCall, roots []string) []string {
	var cands []string
	switch {
	case call.IsShell():
		cmd := call.Command
		if call.Tool == "Bash" {
			cmd = guard.BlankDataHeredocs(cmd)
		}
		cands = shellWriteTargets(cmd)
	case isFileWriteTool(call.Tool):
		cands = []string{call.Path}
	}
	var out []string
	for _, c := range cands {
		abs, ok := absTarget(c)
		if !ok {
			continue
		}
		inside := false
		for _, r := range roots {
			if pathInside(abs, r) {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, c)
		}
	}
	return out
}

func isFileWriteTool(tool string) bool {
	switch tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return true
	}
	return false
}

var (
	winAbsRe  = regexp.MustCompile(`^[a-zA-Z]:[\\/]`)
	gitBashRe = regexp.MustCompile(`^/([a-zA-Z])(/|$)`)
	homeVarRe = regexp.MustCompile(`^(~|\$HOME|\$\{HOME\}|\$env:USERPROFILE|\$env:HOME|%USERPROFILE%)([\\/]|$)`)
)

// absTarget turns a target word into an absolute path. Relative paths, the
// null devices and words with an unknown variable are not targets (false).
func absTarget(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	switch strings.ToLower(p) {
	case "nul", "$null", "con", "/dev/null", "nul:":
		return "", false
	}
	if strings.HasPrefix(p, "/dev/") || strings.HasPrefix(p, "/proc/") {
		return "", false
	}
	if m := homeVarRe.FindStringSubmatch(p); m != nil {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", false
		}
		p = filepath.Join(home, p[len(m[1]):])
	}
	if strings.ContainsAny(p, "$%`") {
		return "", false // an unknown variable: cannot tell where it points
	}
	if runtime.GOOS == "windows" {
		if m := gitBashRe.FindStringSubmatch(p); m != nil {
			p = m[1] + ":/" + strings.TrimPrefix(p[2:], "/")
		}
	}
	if winAbsRe.MatchString(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\\`) || filepath.IsAbs(p) {
		return p, true
	}
	return "", false
}

// pathInside reports whether p is root or below it (case-insensitive on
// Windows, symlinks of the root resolved).
func pathInside(p, root string) bool {
	np := normTargetPath(p)
	for _, r := range []string{root, evalOrSelf(root)} {
		nr := normTargetPath(r)
		if nr == "" {
			continue
		}
		if np == nr || strings.HasPrefix(np, strings.TrimSuffix(nr, "/")+"/") {
			return true
		}
	}
	return false
}

func evalOrSelf(p string) string {
	if e, err := filepath.EvalSymlinks(p); err == nil {
		return e
	}
	return p
}

func normTargetPath(p string) string {
	if runtime.GOOS == "windows" {
		if m := gitBashRe.FindStringSubmatch(p); m != nil {
			p = m[1] + ":/" + strings.TrimPrefix(p[2:], "/")
		}
	}
	if winAbsRe.MatchString(p) || filepath.IsAbs(p) {
		p = filepath.Clean(p)
	}
	p = strings.ReplaceAll(p, `\`, "/")
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// shSegment is one simple command: its words (quotes removed) and the
// targets of its output redirects.
type shSegment struct {
	words  []string
	redirs []string
}

// lexShell splits a bash/PowerShell/cmd command line into simple commands
// at ; & && | || and newlines, outside quotes. A backslash escapes only a
// quote, a space or a backslash (Windows paths keep theirs). Output
// redirects (>, >>, 2>, &>, *>) record their target; >&N and input
// redirects do not.
func lexShell(cmd string) []shSegment {
	var segs []shSegment
	var cur shSegment
	var word strings.Builder
	inWord := false
	pendingRedir := 0 // 1: next word is an output target, 2: an input to skip
	flush := func() {
		if !inWord {
			return
		}
		w := word.String()
		word.Reset()
		inWord = false
		switch pendingRedir {
		case 1:
			cur.redirs = append(cur.redirs, w)
		case 2:
		default:
			cur.words = append(cur.words, w)
		}
		pendingRedir = 0
	}
	endSeg := func() {
		flush()
		pendingRedir = 0
		if len(cur.words) > 0 || len(cur.redirs) > 0 {
			segs = append(segs, cur)
		}
		cur = shSegment{}
	}
	rs := []rune(cmd)
	var q rune
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case q == '\'':
			if r == '\'' {
				q = 0
			} else {
				word.WriteRune(r)
			}
		case q == '"':
			switch {
			case r == '"':
				q = 0
			case r == '\\' && i+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[i+1]):
				i++
				word.WriteRune(rs[i])
			default:
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			q, inWord = r, true
		case r == '\\' && i+1 < len(rs) && strings.ContainsRune("\"' \\", rs[i+1]):
			i++
			word.WriteRune(rs[i])
			inWord = true
		case r == ';' || r == '\n' || r == '|':
			endSeg()
		case r == '&':
			if i+1 < len(rs) && rs[i+1] == '>' {
				flush()
				i++
				if i+1 < len(rs) && rs[i+1] == '>' {
					i++
				}
				pendingRedir = 1
				continue
			}
			endSeg()
		case r == '>':
			// a word of digits or * right before is the fd: drop it
			if inWord && isFDWord(word.String()) {
				word.Reset()
				inWord = false
			} else {
				flush()
			}
			if i+1 < len(rs) && rs[i+1] == '>' {
				i++
			}
			if i+1 < len(rs) && rs[i+1] == '&' {
				// >&2: an fd dup, no file
				i++
				for i+1 < len(rs) && (rs[i+1] >= '0' && rs[i+1] <= '9' || rs[i+1] == '-') {
					i++
				}
				continue
			}
			pendingRedir = 1
		case r == '<':
			flush()
			pendingRedir = 2
		case r == ' ' || r == '\t' || r == '\r':
			flush()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	endSeg()
	return segs
}

func isFDWord(w string) bool {
	if w == "*" {
		return true
	}
	if w == "" {
		return false
	}
	for _, c := range w {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// runPrefixWords run the next word as the command.
var runPrefixWords = map[string]bool{"sudo": true, "doas": true, "env": true, "nohup": true, "time": true, "command": true, "exec": true, "builtin": true, "nice": true}

// shellWriteTargets returns the targets of a command line's writes and
// recursive deletes: redirect targets, tee's files, cp/mv destinations,
// Set-Content/Out-File/Add-Content paths, sed -i files, and the arguments of
// rm -r, rmdir /s and Remove-Item -Recurse. They are the words as written.
func shellWriteTargets(cmd string) []string {
	var out []string
	for _, s := range lexShell(cmd) {
		out = append(out, s.redirs...)
		w := s.words
		for len(w) > 0 {
			b := strings.ToLower(filepath.Base(strings.ReplaceAll(w[0], `\`, "/")))
			b = strings.TrimSuffix(b, ".exe")
			switch {
			case isAssign(w[0]):
				w = w[1:]
				continue
			case runPrefixWords[b]:
				w = w[1:]
				for len(w) > 0 && strings.HasPrefix(w[0], "-") {
					w = w[1:]
				}
				continue
			}
			break
		}
		if len(w) == 0 {
			continue
		}
		verb := strings.TrimSuffix(strings.ToLower(filepath.Base(strings.ReplaceAll(w[0], `\`, "/"))), ".exe")
		args := w[1:]
		out = append(out, verbTargets(verb, args)...)
	}
	return out
}

func isAssign(w string) bool {
	k, _, ok := strings.Cut(w, "=")
	if !ok || k == "" || strings.HasPrefix(k, "-") {
		return false
	}
	for i, c := range k {
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// psValueFlags are PowerShell parameters whose next word is a value.
var psValueFlags = map[string]bool{"-path": true, "-literalpath": true, "-filepath": true, "-destination": true, "-value": true, "-encoding": true, "-include": true, "-exclude": true, "-filter": true, "-inputobject": true, "-width": true, "-stream": true, "-credential": true}

func verbTargets(verb string, args []string) []string {
	var pos []string
	flags := map[string]string{}
	posix := func() {
		for _, a := range args {
			if strings.HasPrefix(a, "-") && len(a) > 1 {
				flags[strings.ToLower(a)] = ""
				continue
			}
			pos = append(pos, a)
		}
	}
	ps := func() {
		for i := 0; i < len(args); i++ {
			a := args[i]
			la := strings.ToLower(a)
			if strings.HasPrefix(a, "-") && len(a) > 1 {
				if psValueFlags[la] && i+1 < len(args) {
					flags[la] = args[i+1]
					i++
				} else {
					flags[la] = ""
				}
				continue
			}
			pos = append(pos, a)
		}
	}
	has := func(names ...string) bool {
		for k := range flags {
			for _, n := range names {
				if k == n || (len(n) > 2 && strings.HasPrefix(n, k) && len(k) >= 3) {
					return true
				}
			}
		}
		return false
	}
	switch verb {
	case "rm":
		posix()
		recursive := has("--recursive", "-recurse")
		for k := range flags {
			if !strings.HasPrefix(k, "--") && strings.ContainsAny(k[1:], "rR") {
				recursive = true
			}
		}
		if recursive {
			return pos
		}
	case "remove-item", "ri", "del", "erase":
		ps()
		if has("-recurse") {
			if p := flags["-path"]; p != "" {
				pos = append(pos, p)
			}
			if p := flags["-literalpath"]; p != "" {
				pos = append(pos, p)
			}
			return pos
		}
	case "rmdir", "rd":
		var out []string
		recursive := false
		for _, a := range args {
			switch strings.ToLower(a) {
			case "/s", "-r", "-rf", "-recurse", "--recursive":
				recursive = true
			default:
				if !strings.HasPrefix(a, "/") || len(a) > 3 {
					if !strings.HasPrefix(a, "-") {
						out = append(out, a)
					}
				}
			}
		}
		if recursive {
			return out
		}
	case "tee", "tee-object":
		ps()
		if p := flags["-filepath"]; p != "" {
			pos = append(pos, p)
		}
		return pos
	case "cp", "mv", "copy", "move", "copy-item", "cpi", "move-item", "mi":
		ps()
		if d := flags["-destination"]; d != "" {
			return []string{d}
		}
		if len(pos) >= 2 {
			return pos[len(pos)-1:]
		}
	case "set-content", "sc", "add-content", "ac", "out-file":
		ps()
		for _, k := range []string{"-path", "-literalpath", "-filepath"} {
			if p := flags[k]; p != "" {
				return []string{p}
			}
		}
		if len(pos) > 0 {
			return pos[:1]
		}
	case "sed":
		inPlace, script := false, false
		var files []string
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "-i" || strings.HasPrefix(a, "-i") || a == "--in-place" || strings.HasPrefix(a, "--in-place="):
				inPlace = true
			case a == "-e" || a == "-f" || a == "--expression" || a == "--file":
				script = true
				i++
			case strings.HasPrefix(a, "-") && len(a) > 1:
			default:
				files = append(files, a)
			}
		}
		if !inPlace {
			return nil
		}
		if !script && len(files) > 0 {
			files = files[1:] // the first positional word is the script
		}
		return files
	}
	return nil
}
