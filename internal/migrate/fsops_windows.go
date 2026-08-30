//go:build windows

package migrate

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	// fileAttributeReparsePoint is FILE_ATTRIBUTE_REPARSE_POINT.
	fileAttributeReparsePoint = 0x400
	// fsctlGetReparsePoint is FSCTL_GET_REPARSE_POINT.
	fsctlGetReparsePoint = 0x000900A8
	// ioReparseTagMountPoint is the tag a directory junction carries.
	ioReparseTagMountPoint = 0xA0000003
	// ioReparseTagSymlink is the tag a directory or file symlink carries.
	ioReparseTagSymlink = 0xA000000C
	// maxReparseBuffer is MAXIMUM_REPARSE_DATA_BUFFER_SIZE.
	maxReparseBuffer = 16 * 1024
	// errorNotSameDevice is ERROR_NOT_SAME_DEVICE, what MoveFileEx returns for
	// a cross-volume rename.
	errorNotSameDevice = syscall.Errno(17)
)

// hasReparsePoint reads FILE_ATTRIBUTE_REPARSE_POINT off a Windows FileInfo.
//
// The field is read through the concrete *syscall.Win32FileAttributeData rather
// than reflectively, so if the shape of that struct ever changes this stops
// compiling instead of silently reporting every junction as a plain directory
// -- which would let MoveDir rename a link as though it were the real identity
// directory.
func hasReparsePoint(fi os.FileInfo) bool {
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok || d == nil {
		return false
	}
	return d.FileAttributes&fileAttributeReparsePoint != 0
}

// windowsIsLink implements IsLink on Windows. See IsLink for the contract.
func windowsIsLink(path string, fi os.FileInfo) (bool, string) {
	if !hasReparsePoint(fi) {
		// Not a reparse point at all. Go sets ModeSymlink for the reparse kinds
		// it recognises, which implies the attribute; if that ever diverges,
		// trust the mode bit rather than silently reporting "not a link".
		if fi.Mode()&os.ModeSymlink == 0 {
			return false, ""
		}
		if tgt, err := os.Readlink(path); err == nil && tgt != "" {
			return true, normalizeTarget(tgt)
		}
		return true, ""
	}
	tag, tgt, err := readReparsePoint(path)
	if err == nil {
		if tag != ioReparseTagMountPoint && tag != ioReparseTagSymlink {
			// Some other reparse point: a cloud-file placeholder, an AppExec
			// alias, a dedup stub. Not a link tq created and not one it may
			// remove.
			return false, ""
		}
		if tgt != "" {
			return true, normalizeTarget(tgt)
		}
	}
	// It carries a reparse point we could not read. Report it as a link with an
	// unknown target so MoveDir still refuses to rename it as a plain
	// directory; every caller that acts on the target checks for "".
	if t, e := os.Readlink(path); e == nil && t != "" {
		return true, normalizeTarget(t)
	}
	if t, e := linkTargetViaDir(path); e == nil && t != "" {
		return true, normalizeTarget(t)
	}
	return true, ""
}

// readReparsePoint returns the reparse tag on path and, for junctions and
// symlinks, the substitute name they point at.
//
// This reads the reparse point itself instead of parsing the output of a shell
// command, so the answer cannot be confused by a sibling with a similar name
// and does not depend on the console locale.
func readReparsePoint(path string) (uint32, string, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, "", fmt.Errorf("reparse %s: %w", path, err)
	}
	h, err := syscall.CreateFile(p, 0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 0, "", fmt.Errorf("reparse %s: opening: %w", path, err)
	}
	defer syscall.CloseHandle(h)

	buf := make([]byte, maxReparseBuffer)
	var n uint32
	if err := syscall.DeviceIoControl(h, fsctlGetReparsePoint, nil, 0,
		&buf[0], uint32(len(buf)), &n, nil); err != nil {
		return 0, "", fmt.Errorf("reparse %s: FSCTL_GET_REPARSE_POINT: %w", path, err)
	}
	b := buf[:n]
	if len(b) < 8 {
		return 0, "", fmt.Errorf("reparse %s: short buffer (%d bytes)", path, n)
	}
	tag := binary.LittleEndian.Uint32(b[0:4])

	// REPARSE_DATA_BUFFER is ReparseTag(4) ReparseDataLength(2) Reserved(2),
	// then SubstituteNameOffset(2) SubstituteNameLength(2) PrintNameOffset(2)
	// PrintNameLength(2). A symlink carries an extra Flags(4) before its
	// PathBuffer; a mount point does not.
	var pathBuf int
	switch tag {
	case ioReparseTagMountPoint:
		pathBuf = 16
	case ioReparseTagSymlink:
		pathBuf = 20
	default:
		return tag, "", nil
	}
	if len(b) < pathBuf {
		return tag, "", fmt.Errorf("reparse %s: truncated reparse data", path)
	}
	off := int(binary.LittleEndian.Uint16(b[8:10]))
	ln := int(binary.LittleEndian.Uint16(b[10:12]))
	start := pathBuf + off
	end := start + ln
	if ln == 0 || ln%2 != 0 || start < pathBuf || end > len(b) {
		return tag, "", fmt.Errorf("reparse %s: substitute name out of range", path)
	}
	u := make([]uint16, ln/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[start+2*i:])
	}
	return tag, syscall.UTF16ToString(u), nil
}

// runCmdLine runs cmd.exe with an explicitly built command line.
//
// exec.Command quotes an argument only when it contains a space or a tab, so
// the characters cmd.exe treats specially -- & | ^ ( ) < > -- otherwise reach
// the shell bare and are re-parsed by it. Since tq builds these command lines
// out of user-supplied paths (TQ_HOME, a Windows account name, both of which
// may legally contain &), that is a command-injection hole as well as a plain
// bug: a directory named a&calc&b would run calc. Setting SysProcAttr.CmdLine
// bypasses Go's escaping entirely, so the quoting the caller applies is the
// only quoting there is -- which is why every caller must first reject paths
// containing a double quote (checkNoQuote).
func runCmdLine(cmdline string) (string, error) {
	c := exec.Command("cmd")
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: "/c " + cmdline}
	out, err := c.CombinedOutput()
	return string(out), err
}

// dirTags are the markers `dir /AL` prints in place of a size for a reparse
// entry. The name of the entry follows the tag and the target follows the name
// in square brackets.
var dirTags = []string{"<JUNCTION>", "<SYMLINKD>", "<SYMLINK>"}

// linkTargetViaDir recovers a junction's target by parsing `dir /AL` output,
// which renders reparse entries as `<JUNCTION>     name [C:\real\target]`.
//
// It is only a fallback for the rare case where the reparse point itself cannot
// be read. The name field is compared for exact equality rather than
// substring-matched: `dir /AL` lists every reparse entry in the directory, and
// a substring match asked for "b" would happily return the target of a sibling
// junction named "ab", writing another directory's path into a durable journal.
func linkTargetViaDir(path string) (string, error) {
	parent := filepath.Dir(path)
	base := filepath.Base(path)
	if err := checkNoQuote("dir /AL", parent); err != nil {
		return "", err
	}
	out, err := runCmdLine(fmt.Sprintf(`dir /AL "%s"`, parent))
	if err != nil {
		return "", fmt.Errorf("dir /AL %s: %w (%s)", parent, err, strings.TrimSpace(out))
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		tagEnd := -1
		for _, tag := range dirTags {
			if i := strings.Index(line, tag); i >= 0 {
				tagEnd = i + len(tag)
				break
			}
		}
		if tagEnd < 0 {
			continue
		}
		rest := line[tagEnd:]
		open := strings.LastIndex(rest, "[")
		closeIdx := strings.LastIndex(rest, "]")
		if open < 0 || closeIdx <= open {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(rest[:open]), base) {
			continue
		}
		return strings.TrimSpace(rest[open+1 : closeIdx]), nil
	}
	return "", fmt.Errorf("dir /AL %s: no reparse entry named %s", parent, base)
}

// mklinkJ creates a Windows directory junction at link pointing at target.
func mklinkJ(link, target string) error {
	if err := checkNoQuote("mklink /J", link, target); err != nil {
		return err
	}
	out, err := runCmdLine(fmt.Sprintf(`mklink /J "%s" "%s"`, link, target))
	if err != nil {
		return fmt.Errorf("mklink /J %s %s: %w (%s)", link, target, err, strings.TrimSpace(out))
	}
	return nil
}

// isCrossDevice reports whether err is a rename refused because source and
// destination live on different volumes.
func isCrossDevice(err error) bool {
	return errors.Is(err, errorNotSameDevice) || errors.Is(err, syscall.EXDEV)
}

// syncDir fsyncs a directory so a rename or create inside it is durable.
// Windows has no directory fsync (and a directory handle opened by os.Open
// cannot be flushed), so metadata durability is left to the filesystem.
func syncDir(string) error { return nil }
