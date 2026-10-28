//go:build windows

package main

import (
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createNoWindow keeps the child hub from opening a console window of its own.
const createNoWindow = 0x08000000

func prepareChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// procTree is the child hub and everything it starts (its runs' agents: claude on Windows is a
// .cmd shim with a node child). They share a Job Object that kills every member when its last
// handle closes, so the tree ends with the hub, and with this app even when the app crashes.
type procTree struct {
	pid  int
	job  windows.Handle // 0 when the job could not be set up; the force stop is taskkill /T /F then
	once sync.Once
}

func attachTree(cmd *exec.Cmd) *procTree {
	t := &procTree{pid: cmd.Process.Pid}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return t
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return t
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(t.pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return t
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return t
	}
	t.job = job
	return t
}

// interruptProcess: Windows has no signal a GUI app can send a console-less child; the polite
// stop is closing the hub's stdin (TQ_HUB_PARENT_STDIN), which the caller already did.
func interruptProcess(*exec.Cmd) {}

// killTree force-ends the hub and every process it started.
func (t *procTree) killTree() {
	if t.job != 0 {
		_ = windows.TerminateJobObject(t.job, 1)
		return
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(t.pid), "/T", "/F").Run()
}

// release runs once the hub has exited: closing the job ends anything it left behind.
func (t *procTree) release() {
	t.once.Do(func() {
		if t.job != 0 {
			_ = windows.CloseHandle(t.job)
		}
	})
}
