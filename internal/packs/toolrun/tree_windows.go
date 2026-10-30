//go:build windows

package toolrun

import (
	"errors"
	"fmt"
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
	ntdll                             = syscall.NewLazyDLL("ntdll.dll")
	procNtResumeProcess               = ntdll.NewProc("NtResumeProcess")
	jobObjectExtendedLimitInfo uint32 = 9
)

const (
	jobObjectLimitKillOnJobClose = 0x00002000
	processSetQuota              = 0x0100
	processTerminate             = 0x0001
	processSuspendResume         = 0x0800
	createSuspended              = 0x00000004
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

// procTree is a Job Object holding the child and every process it starts.
// The child is created suspended and resumed only once it is in the job,
// so no grandchild can start outside it.
type procTree struct {
	pid int
	job syscall.Handle
}

func prepareTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createSuspended}
}

// attachTree puts the suspended child in a kill-on-close job and resumes
// it. Any failure kills the child and is returned: the tool never runs
// outside a job.
func attachTree(cmd *exec.Cmd) (*procTree, error) {
	t := &procTree{pid: cmd.Process.Pid}
	fail := func(err error) (*procTree, error) {
		_ = cmd.Process.Kill()
		if t.job != 0 {
			syscall.CloseHandle(t.job)
			t.job = 0
		}
		return nil, err
	}
	h, _, err := procCreateJobObject.Call(0, 0)
	if h == 0 {
		return fail(errors.New("CreateJobObject: " + err.Error()))
	}
	t.job = syscall.Handle(h)
	var info jobExtendedLimit
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	if r, _, err := procSetInformationJob.Call(uintptr(t.job), uintptr(jobObjectExtendedLimitInfo), uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info)); r == 0 {
		return fail(errors.New("SetInformationJobObject: " + err.Error()))
	}
	ph, err := syscall.OpenProcess(processSetQuota|processTerminate|processSuspendResume, false, uint32(t.pid))
	if err != nil {
		return fail(err)
	}
	defer syscall.CloseHandle(ph)
	if r, _, aerr := procAssignProcessToJob.Call(uintptr(t.job), uintptr(ph)); r == 0 {
		return fail(errors.New("AssignProcessToJobObject: " + aerr.Error()))
	}
	// NtResumeProcess resumes the threads of this one process through its
	// handle. It replaces a walk of a Toolhelp snapshot of every thread on
	// the machine, whose cost grows with the whole machine's thread count
	// and in which a loaded CI runner hung a tool start for minutes
	// (TestRuffFindings, tree_windows.go resumeProcess).
	if st, _, _ := procNtResumeProcess.Call(uintptr(ph)); st != 0 {
		return fail(fmt.Errorf("NtResumeProcess: NTSTATUS 0x%08x", uint32(st)))
	}
	return t, nil
}

func (t *procTree) kill() {
	if t.job != 0 {
		procTerminateJobObject.Call(uintptr(t.job), 1)
	}
	// Belt and braces: kill the tree by parent pid too.
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
		// KILL_ON_JOB_CLOSE: a grandchild left behind dies with the handle,
		// which only tq holds.
		syscall.CloseHandle(t.job)
		t.job = 0
	}
}
