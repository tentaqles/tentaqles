package scan

import (
	"path"
	"strings"
)

// Shell script files a command runs, for the pack guards: they read a
// script (bounded, through GuardCtx.ReadLocal) and ask when it names their
// tool, as they ask on a PowerShell -File.

// MaxScripts bounds how many script files one command reports.
const MaxScripts = 4

// scriptShells run their first non-flag argument as a script file.
var scriptShells = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true, "ksh": true, "ash": true}

// scriptLaunchers are skipped before the command word.
var scriptLaunchers = map[string]bool{"sudo": true, "env": true, "exec": true, "nohup": true, "time": true,
	"timeout": true, "nice": true, "command": true, "&": true}

var scriptExts = []string{".sh", ".bash", ".zsh", ".ksh"}

// ScriptFiles returns the script files a command line runs as written:
// `bash|sh|zsh|dash|ksh <script>` (no -c, no -s), `source <script>`,
// `. <script>`, and a command word that is a script path (./build.sh,
// scripts/x.bash, C:\x\run.sh). Paths are as written (relative to the
// command's cwd). Words with $, `, *, ? or ~ are skipped (the shell
// computes them).
func ScriptFiles(cmd string) []string {
	// Cheap prefilter: every form names "sh" (bash, sh, zsh, .sh, .bash,
	// .zsh, .ksh), "source" or a leading dot.
	low := strings.ToLower(cmd)
	if !strings.Contains(low, "sh") && !strings.Contains(low, "source") && !strings.Contains(cmd, ". ") {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || strings.ContainsAny(p, "$`*?~") || seen[p] || len(out) >= MaxScripts {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, seg := range Segments(cmd) {
		w := seg
		for len(w) > 0 {
			h := strings.ToLower(w[0])
			if scriptLaunchers[h] || (strings.Contains(w[0], "=") && !strings.HasPrefix(w[0], "-")) {
				w = w[1:]
				for scriptLaunchers[h] && len(w) > 0 && strings.HasPrefix(w[0], "-") {
					w = w[1:] // sudo -E, env -i, nice -n (its value is a number, skipped below)
				}
				for (h == "timeout" || h == "nice") && len(w) > 0 && strings.IndexFunc(w[0], func(r rune) bool { return r < '0' || r > '9' }) < 0 {
					w = w[1:] // timeout 600 ./x.sh
				}
				continue
			}
			break
		}
		if len(w) == 0 {
			continue
		}
		name := strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(w[0], `\`, "/"))), ".exe")
		switch {
		case scriptShells[name]:
			for _, a := range w[1:] {
				if a == "-s" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsRune(a[1:], 'c')) {
					break // a -c script or stdin: not a file
				}
				if strings.HasPrefix(a, "-") || strings.HasPrefix(a, "+") {
					continue
				}
				add(a)
				break
			}
		case name == "source" || w[0] == ".":
			if len(w) > 1 {
				add(w[1])
			}
		default:
			if isScriptPath(w[0]) {
				add(w[0])
			}
		}
	}
	return out
}

func isScriptPath(w string) bool {
	l := strings.ToLower(w)
	for _, e := range scriptExts {
		if strings.HasSuffix(l, e) {
			return true
		}
	}
	return false
}

// MaxSegmentWords bounds Segments on a hostile command.
const MaxSegmentWords = 50000

// Segments splits a command line roughly into simple commands of words:
// words on blanks, segments on unquoted ; & | ( ) { } and newlines, quotes
// removed. It stops at MaxSegmentWords.
func Segments(cmd string) [][]string {
	var segs [][]string
	var seg []string
	var b strings.Builder
	in := false
	var quote byte
	words := 0
	flushWord := func() {
		if in {
			seg = append(seg, b.String())
			words++
		}
		b.Reset()
		in = false
	}
	flushSeg := func() {
		flushWord()
		if len(seg) > 0 {
			segs = append(segs, seg)
		}
		seg = nil
	}
	for i := 0; i < len(cmd) && words <= MaxSegmentWords; i++ {
		c := cmd[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				b.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, in = c, true
		case c == ' ' || c == '\t':
			flushWord()
		case c == ';' || c == '&' || c == '|' || c == '(' || c == ')' || c == '\n' || c == '\r' || c == '{' || c == '}':
			flushSeg()
		default:
			b.WriteByte(c)
			in = true
		}
	}
	flushSeg()
	return segs
}
