package doctor

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// memoryStaleAfter is how far the newest Claude transcript may run ahead of
// the workspace's memory.db before the plugin's hooks are presumed dead.
const memoryStaleAfter = 48 * time.Hour

// memoryStale reports a workspace whose memory.db has not been written for
// memoryStaleAfter although Claude Code sessions kept running in it: the
// plugin hooks that capture sessions, touches and decisions are not firing
// (wrong interpreter, plugin disabled, hook timing out). A workspace with no
// memory.db at all has never captured and is not reported here.
func memoryStale(ws, root string, now time.Time, add func(level, code, ws, msg, fix string)) {
	db := filepath.Join(root, ".claude", "memory.db")
	fi, err := os.Stat(db)
	if err != nil {
		return
	}
	slug := strings.ToLower(transcriptSlug(root))
	projects := filepath.Join(paths.IdentityDir(ws, "claude"), "projects")
	dirs, err := os.ReadDir(projects)
	if err != nil {
		return
	}
	var newest time.Time
	for _, d := range dirs {
		name := strings.ToLower(d.Name())
		if !d.IsDir() || (name != slug && !strings.HasPrefix(name, slug+"-")) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(projects, d.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			if info, err := f.Info(); err == nil && info.ModTime().After(newest) {
				newest = info.ModTime()
			}
		}
	}
	if newest.IsZero() || newest.Sub(fi.ModTime()) <= memoryStaleAfter {
		return
	}
	add("warn", "memory-stale", ws,
		"memory.db was last written "+humanAge(now.Sub(fi.ModTime()))+" ago but a session ran "+humanAge(now.Sub(newest))+" ago: the plugin hooks are not capturing ("+db+")",
		"run a session in the workspace with `claude --debug hooks` and look for SessionEnd/PostToolUse hook errors; the plugin findings above may explain it")
}

// transcriptSlug is how Claude Code names the projects/ folder for a cwd:
// every character that is not a letter or digit becomes '-'.
func transcriptSlug(root string) string {
	var b strings.Builder
	for _, r := range root {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "under an hour"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Round(time.Hour).Hours())) + " hours"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + " days"
	}
}
