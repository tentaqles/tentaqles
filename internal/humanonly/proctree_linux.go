//go:build linux

package humanonly

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// processTreeMethod names how this OS walks the tree, for tq doctor.
const processTreeMethod = "/proc"

// parentOf reads pid's parent from /proc/<pid>/stat, then the parent's name
// from /proc/<ppid>/comm and its command line from /proc/<ppid>/cmdline.
func parentOf(pid int) (int, string, string, bool) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, "", "", false
	}
	// "pid (comm) state ppid ...": comm may hold spaces and parentheses, so
	// the fields start after the last ')'.
	s := string(stat)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, "", "", false
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 2 {
		return 0, "", "", false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil || ppid <= 0 {
		return 0, "", "", false
	}
	cmdline := procCmdline(ppid)
	name := ""
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(ppid) + "/comm"); err == nil {
		name = strings.TrimSpace(string(b))
	}
	// comm is cut at 15 bytes; argv[0] carries the whole name when readable.
	if argv0, _, _ := strings.Cut(cmdline, " "); argv0 != "" && (name == "" || len(name) >= 15) {
		name = filepath.Base(argv0)
	}
	if name == "" {
		return 0, "", "", false
	}
	return ppid, name, cmdline, true
}

// procCmdline is /proc/<pid>/cmdline with its NUL separators as spaces, or
// "" when it cannot be read.
func procCmdline(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " "))
}
