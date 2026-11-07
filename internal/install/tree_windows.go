//go:build windows

package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"golang.org/x/sys/windows"
)

// tree stops a process and its descendants. On Windows the child is put in a job object right
// after it starts, and its descendants join the job when they are created; ending the run
// terminates the job. The job sets no limit of its own, so a run that ends normally leaves
// whatever the package manager meant to leave running. A child that could not be put in a
// job (it is already in one that forbids it) is stopped with taskkill /T instead.
type tree struct {
	mu       sync.Mutex
	job      windows.Handle
	canceled bool
	c        *exec.Cmd
}

func prepareTree(c *exec.Cmd) *tree {
	t := &tree{c: c}
	c.Cancel = t.kill
	return t
}

// attach is called once the process has started.
func (t *tree) attach(c *exec.Cmd) {
	t.mu.Lock()
	defer t.mu.Unlock()
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(c.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	t.job = job
	if t.canceled { // the limit was reached before the job existed
		_ = windows.TerminateJobObject(job, 1)
	}
}

func (t *tree) kill() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.canceled = true
	if t.job != 0 {
		_ = windows.TerminateJobObject(t.job, 1)
	} else if t.c.Process != nil {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		_ = exec.Command(filepath.Join(root, "System32", "taskkill.exe"), "/T", "/F", "/PID", strconv.Itoa(t.c.Process.Pid)).Run()
	}
	if t.c.Process != nil {
		return t.c.Process.Kill()
	}
	return nil
}

func (t *tree) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
}
