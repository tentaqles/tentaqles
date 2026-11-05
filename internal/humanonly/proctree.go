package humanonly

import (
	"path/filepath"
	"strings"
)

// ParentFunc reads one step of the process tree: the parent of pid, as its
// pid, executable base name and command line (empty when the OS will not say).
// ok is false at the root, for a pid that is gone, and on an OS without a
// walker. The per-OS implementations live in proctree_*.go; tests feed a fake
// tree.
type ParentFunc func(pid int) (ppid int, name string, cmdline string, ok bool)

// Process is one process found in the parent chain.
type Process struct {
	PID     int
	Name    string
	Cmdline string
}

// maxDepth bounds the walk: no real chain is this deep, and a stale parent
// pid on Windows could otherwise point anywhere.
const maxDepth = 64

// harnessNames are the executables (without directory or .exe) that mark a
// harness or a subagent in the process tree. node is matched on its command
// line (see IsHarness), since an npm-installed Claude Code or Codex runs as
// node with the harness's script as an argument.
var harnessNames = map[string]bool{"claude": true, "codex": true, "opencode": true, "kimi": true}

// IsHarness reports whether a process with this executable name and command
// line is a harness or subagent. The name may carry a directory and an .exe
// suffix; the match is case-insensitive and exact on the base name.
func IsHarness(name, cmdline string) bool {
	n := strings.ToLower(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
	n = strings.TrimSuffix(n, ".exe")
	if harnessNames[n] {
		return true
	}
	if n == "node" {
		c := strings.ToLower(cmdline)
		return strings.Contains(c, "claude") || strings.Contains(c, "codex")
	}
	return false
}

// Walk follows parentOf from pid up to the root and returns the first harness
// or subagent process found. The process at pid itself is not examined.
func Walk(pid int, parentOf ParentFunc) (Process, bool) {
	seen := map[int]bool{pid: true}
	for i := 0; i < maxDepth; i++ {
		ppid, name, cmdline, ok := parentOf(pid)
		if !ok || ppid <= 0 || seen[ppid] {
			return Process{}, false
		}
		if IsHarness(name, cmdline) {
			return Process{PID: ppid, Name: name, Cmdline: cmdline}, true
		}
		seen[ppid] = true
		pid = ppid
	}
	return Process{}, false
}
