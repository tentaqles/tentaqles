//go:build darwin

package humanonly

import (
	"bytes"
	"encoding/binary"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// processTreeMethod names how this OS walks the tree, for tq doctor.
const processTreeMethod = "sysctl kern.proc"

// parentOf reads pid's parent through sysctl KERN_PROC (the pid and the
// comm name) and KERN_PROCARGS2 (the command line), and falls back to ps
// when sysctl refuses.
func parentOf(pid int) (int, string, string, bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return psParentOf(pid)
	}
	ppid := int(kp.Eproc.Ppid)
	if ppid <= 0 {
		return 0, "", "", false
	}
	parent, err := unix.SysctlKinfoProc("kern.proc.pid", ppid)
	if err != nil {
		return psParentOf(pid)
	}
	name := unix.ByteSliceToString(parent.Proc.P_comm[:])
	if name == "" {
		return 0, "", "", false
	}
	return ppid, name, procArgs(ppid), true
}

// procArgs is pid's command line from kern.procargs2: argc, the executable
// path, NUL padding, then argc NUL-terminated arguments. "" when the OS will
// not say (another user's process).
func procArgs(pid int) string {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(raw) < 4 {
		return ""
	}
	argc := int(binary.LittleEndian.Uint32(raw[:4]))
	rest := raw[4:]
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return ""
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
	return strings.Join(args, " ")
}

// psParentOf is the last resort: ps prints the parent pid, then the parent's
// name and arguments.
func psParentOf(pid int) (int, string, string, bool) {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, "", "", false
	}
	ppid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || ppid <= 0 {
		return 0, "", "", false
	}
	name, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(ppid)).Output()
	if err != nil || strings.TrimSpace(string(name)) == "" {
		return 0, "", "", false
	}
	args, _ := exec.Command("ps", "-o", "args=", "-p", strconv.Itoa(ppid)).Output()
	return ppid, strings.TrimSpace(string(name)), strings.TrimSpace(string(args)), true
}
