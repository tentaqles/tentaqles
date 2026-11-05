//go:build darwin

package humanonly

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// processTreeMethod names how this OS walks the tree, for tq doctor.
const processTreeMethod = "sysctl kern.proc"

// parentOf reads pid's parent through sysctl KERN_PROC (the pid, the comm
// name and the owner) and KERN_PROCARGS2 (the command line), and falls back
// to ps when sysctl refuses. launchd (pid 1) has no parent. A command line
// that cannot be read is unknown unless the parent belongs to another uid
// (sshd as root, launchd): it is then no process of this user's, and
// reported known and empty.
func parentOf(pid int) (Parent, bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return psParentOf(pid)
	}
	ppid := int(kp.Eproc.Ppid)
	if ppid <= 0 {
		return Parent{}, false
	}
	parent, err := unix.SysctlKinfoProc("kern.proc.pid", ppid)
	if err != nil {
		return psParentOf(pid)
	}
	p := Parent{PID: ppid, Name: unix.ByteSliceToString(parent.Proc.P_comm[:])}
	if p.Name == "" {
		return Parent{}, false
	}
	switch args, ok := procArgs(ppid); {
	case ok && args != "":
		p.Cmdline, p.CmdlineKnown = args, true
	case int(parent.Eproc.Ucred.Uid) != os.Getuid():
		p.CmdlineKnown = true
	}
	return p, true
}

// procArgs is pid's command line from kern.procargs2: argc, the executable
// path, NUL padding, then argc NUL-terminated arguments. ok is false when the
// OS will not say (another user's process).
func procArgs(pid int) (string, bool) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(raw) < 4 {
		return "", false
	}
	argc := int(binary.LittleEndian.Uint32(raw[:4]))
	rest := raw[4:]
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return "", false
	}
	rest = bytes.TrimLeft(rest[i:], "\x00")
	var args []string
	for len(args) < argc && len(rest) > 0 {
		j := bytes.IndexByte(rest, 0)
		if j < 0 {
			args = append(args, string(rest))
			break
		}
		args = append(args, string(rest[:j]))
		rest = rest[j+1:]
	}
	return strings.Join(args, " "), true
}

// psParentOf is the last resort: ps prints the parent pid, then the parent's
// name, owner and arguments.
func psParentOf(pid int) (Parent, bool) {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return Parent{}, false
	}
	ppid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || ppid <= 0 {
		return Parent{}, false
	}
	name, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(ppid)).Output()
	if err != nil || strings.TrimSpace(string(name)) == "" {
		return Parent{}, false
	}
	p := Parent{PID: ppid, Name: strings.TrimSpace(string(name))}
	args, err := exec.Command("ps", "-o", "args=", "-p", strconv.Itoa(ppid)).Output()
	if err == nil && strings.TrimSpace(string(args)) != "" {
		p.Cmdline, p.CmdlineKnown = strings.TrimSpace(string(args)), true
		return p, true
	}
	if uid, err := exec.Command("ps", "-o", "uid=", "-p", strconv.Itoa(ppid)).Output(); err == nil {
		if u, err := strconv.Atoi(strings.TrimSpace(string(uid))); err == nil && u != os.Getuid() {
			p.CmdlineKnown = true
		}
	}
	return p, true
}
