//go:build windows

package toolrun

import (
	"errors"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                            = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObject                 = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJob               = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJob              = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject              = kernel32.NewProc("TerminateJobObject")
	procCreateToolhelp32Snapshot        = kernel32.NewProc("CreateToolhelp32Snapshot")
	procThread32First                   = kernel32.NewProc("Thread32First")
	procThread32Next                    = kernel32.NewProc("Thread32Next")
	procOpenThread                      = kernel32.NewProc("OpenThread")
	procResumeThread                    = kernel32.NewProc("ResumeThread")
	jobObjectExtendedLimitInfo   uint32 = 9
)

const (
	jobObjectLimitKillOnJobClose = 0x00002000
	processSetQuota              = 0x0100
	processTerminate             = 0x0001
	createSuspended              = 0x00000004
	th32csSnapThread             = 0x00000004
	threadSuspendResume          = 0x0002
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

type threadEntry32 struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePri        int32
	DeltaPri       int32
	Flags          uint32
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
	ph, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(t.pid))
	if err != nil {
		return fail(err)
	}
	r, _, aerr := procAssignProcessToJob.Call(uintptr(t.job), uintptr(ph))
	syscall.CloseHandle(ph)
	if r == 0 {
		return fail(errors.New("AssignProcessToJobObject: " + aerr.Error()))
	}
	if err := resumeProcess(uint32(t.pid)); err != nil {
		return fail(err)
	}
	return t, nil
}

// resumeProcess resumes every thread of pid (a new suspended process has one).
func resumeProcess(pid uint32) error {
	snap, _, err := procCreateToolhelp32Snapshot.Call(th32csSnapThread, 0)
	if syscall.Handle(snap) == syscall.InvalidHandle {
		return errors.New("CreateToolhelp32Snapshot: " + err.Error())
	}
	defer syscall.CloseHandle(syscall.Handle(snap))
	var te threadEntry32
	te.Size = uint32(unsafe.Sizeof(te))
	resumed := 0
	for ok, _, _ := procThread32First.Call(snap, uintptr(unsafe.Pointer(&te))); ok != 0; ok, _, _ = procThread32Next.Call(snap, uintptr(unsafe.Pointer(&te))) {
		if te.OwnerProcessID != pid {
			continue
		}
		th, _, _ := procOpenThread.Call(threadSuspendResume, 0, uintptr(te.ThreadID))
		if th == 0 {
			continue
		}
		if r, _, _ := procResumeThread.Call(th); r != 0xFFFFFFFF {
			resumed++
		}
		syscall.CloseHandle(syscall.Handle(th))
	}
	if resumed == 0 {
		return errors.New("the tool's main thread could not be resumed")
	}
	return nil
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
