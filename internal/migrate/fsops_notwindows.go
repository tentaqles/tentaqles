//go:build !windows

package migrate

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// windowsIsLink is unreachable off Windows -- IsLink guards on runtime.GOOS --
// but must exist so the portable file compiles everywhere.
func windowsIsLink(string, os.FileInfo) (bool, string) { return false, "" }

// hasReparsePoint is a Windows concept; nothing else has reparse points.
func hasReparsePoint(os.FileInfo) bool { return false }

// linkTargetViaDir has no meaning off Windows: os.Readlink already answers.
func linkTargetViaDir(path string) (string, error) {
	return "", fmt.Errorf("dir /AL %s: not supported on this OS", path)
}

// mklinkJ has no meaning off Windows: MakeLink uses os.Symlink there.
func mklinkJ(link, target string) error {
	return fmt.Errorf("mklink /J %s %s: not supported on this OS", link, target)
}

// isCrossDevice reports whether err is a rename refused because source and
// destination live on different filesystems.
func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }

// syncDir fsyncs a directory so a rename or create inside it is durable. On
// ext4 with data=ordered a file can be fsynced and still vanish after a crash
// if the directory entry naming it was never flushed.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		// Some filesystems refuse fsync on a directory handle. That is not a
		// failure of the caller's write, which was already fsynced.
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
			return nil
		}
		return err
	}
	return nil
}
