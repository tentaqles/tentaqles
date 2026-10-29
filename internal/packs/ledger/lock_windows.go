//go:build windows

package ledger

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
)

// tryLock takes an exclusive lock on f without waiting.
func tryLock(f *os.File) bool {
	var ol syscall.Overlapped
	r, _, _ := procLockFileEx.Call(f.Fd(), lockfileExclusiveLock|lockfileFailImmediately, 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	return r != 0
}

func unlock(f *os.File) {
	var ol syscall.Overlapped
	procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
}

// renameRetryable reports a sharing violation (another process has the
// ledger open), which a later attempt may get past.
func renameRetryable(err error) bool {
	if le, ok := err.(*os.LinkError); ok {
		err = le.Err
	}
	errno, ok := err.(syscall.Errno)
	return ok && (errno == 32 || errno == 5) // ERROR_SHARING_VIOLATION, ERROR_ACCESS_DENIED
}

func syncDir(string) error { return nil }
