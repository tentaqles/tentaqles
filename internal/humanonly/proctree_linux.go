//go:build linux

package humanonly

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// processTreeMethod names how this OS walks the tree, for tq doctor.
const processTreeMethod = "/proc"

// parentOf reads pid's parent from /proc/<pid>/stat, then the parent's name
// from /proc/<ppid>/comm and its command line from /proc/<ppid>/cmdline. pid
// 1 has no parent. A command line that cannot be read, or is empty, is
// unknown unless the parent belongs to another uid (sshd as root, init):
// it is then no process of this user's, and reported known and empty.
func parentOf(pid int) (Parent, bool) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return Parent{}, false
	}
	// "pid (comm) state ppid ...": comm may hold spaces and parentheses, so
	// the fields start after the last ')'.
	s := string(stat)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return Parent{}, false
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 2 {
		return Parent{}, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil || ppid <= 0 {
		return Parent{}, false
	}
	dir := "/proc/" + strconv.Itoa(ppid)
	p := Parent{PID: ppid}
	cmdline, readable := procCmdline(dir)
	if b, err := os.ReadFile(dir + "/comm"); err == nil {
		p.Name = strings.TrimSpace(string(b))
	}
	// comm is cut at 15 bytes; argv[0] carries the whole name when readable.
	if a := argv0(cmdline); a != "" && (p.Name == "" || len(p.Name) >= 15) {
		p.Name = filepath.Base(a)
	}
	if p.Name == "" {
		return Parent{}, false
	}
	switch {
	case readable && cmdline != "":
		p.Cmdline, p.CmdlineKnown = cmdline, true
	case !sameUID(dir):
		p.CmdlineKnown = true
	}
	return p, true
}

// procCmdline is /proc/<pid>/cmdline with its NUL separators as spaces.
func procCmdline(dir string) (string, bool) {
	b, err := os.ReadFile(dir + "/cmdline")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " ")), true
}

// sameUID reports whether the process at /proc/<pid> runs as this user. A
// failure to tell counts as the same user, so the walk refuses.
func sameUID(dir string) bool {
	fi, err := os.Stat(dir)
	if err != nil {
		return true
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	return int(st.Uid) == os.Getuid()
}
