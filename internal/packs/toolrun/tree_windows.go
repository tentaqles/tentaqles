//go:build windows

package toolrun

import (
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                          = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObject               = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJob             = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJob            = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject            = kernel32.NewProc("TerminateJobObject")
	jobObjectExtendedLimitInfo uint32 = 9
)

const (
	jobObjectLimitKillOnJobClose = 0x00002000
	processSetQuota              = 0x0100
	processTerminate             = 0x0001
)

type ioCounters struct {
	ReadOperationCount, WriteOperationCount, OtherOperationCount uint64
	ReadTransferCount, WriteTransferCount, OtherTransferCount    uint64
}

type jobBasicLimit struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobExtendedLimit struct {
	BasicLimitInformation jobBasicLimit
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// procTree is a Job Object holding the child and every process it starts;
// taskkill /T /F is the fallback when the job could not be set up.
type procTree struct {
	pid int
	job syscall.Handle
}

func prepareTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func attachTree(cmd *exec.Cmd) *procTree {
	t := &procTree{pid: cmd.Process.Pid}
	h, _, _ := procCreateJobObject.Call(0, 0)
	if h == 0 {
		return t
	}
	job := syscall.Handle(h)
	var info jobExtendedLimit
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	if r, _, _ := procSetInformationJob.Call(uintptr(job), uintptr(jobObjectExtendedLimitInfo), uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info)); r == 0 {
		syscall.CloseHandle(job)
		return t
	}
	ph, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(t.pid))
	if err != nil {
		syscall.CloseHandle(job)
		return t
	}
	defer syscall.CloseHandle(ph)
	if r, _, _ := procAssignProcessToJob.Call(uintptr(job), uintptr(ph)); r == 0 {
		syscall.CloseHandle(job)
		return t
	}
	t.job = job
	return t
}

func (t *procTree) kill() {
	if t.job != 0 {
		procTerminateJobObject.Call(uintptr(t.job), 1)
	}
	// Fallback (and belt and braces for a child that escaped the job before
	// it was assigned): kill the tree by parent pid.
	if t.pid > 0 {
		c := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(t.pid))
		c.WaitDelay = time.Second
		done := make(chan struct{})
		go func() { _ = c.Run(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			if c.Process != nil {
				_ = c.Process.Kill()
			}
		}
	}
}

func (t *procTree) close() {
	if t.job != 0 {
		// KILL_ON_JOB_CLOSE: a grandchild left behind dies with the handle.
		syscall.CloseHandle(t.job)
		t.job = 0
	}
}
