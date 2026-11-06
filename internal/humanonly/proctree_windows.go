//go:build windows

package humanonly

import (
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processTreeMethod names how this OS walks the tree, for tq doctor.
const processTreeMethod = "toolhelp snapshot"

// The snapshot is reused across the steps of one walk (and the re-check the
// confirmation makes): a chain of ten parents would otherwise enumerate every
// process ten times.
var (
	snapMu    sync.Mutex
	snapTaken time.Time
	snapTable map[uint32]windows.ProcessEntry32
)

const snapTTL = 2 * time.Second

// snapshot returns the process table, taking a new toolhelp snapshot when
// the cached one is older than snapTTL.
func snapshot() (map[uint32]windows.ProcessEntry32, error) {
	snapMu.Lock()
	defer snapMu.Unlock()
	if snapTable != nil && time.Since(snapTaken) < snapTTL {
		return snapTable, nil
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	entries := map[uint32]windows.ProcessEntry32{}
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		entries[pe.ProcessID] = pe
	}
	snapTable, snapTaken = entries, time.Now()
	return entries, nil
}

// parentOf reads a toolhelp snapshot for pid's parent: its pid and executable
// name from the snapshot, its command line from its PEB. A parent that is
// gone from the snapshot, or pid 0, is no parent. A parent created after its
// child is a reused pid. A command line that cannot be read is unknown,
// unless the parent sits in another session (a service): it is then no
// process of this user's, and reported known and empty.
func parentOf(pid int) (Parent, bool) {
	entries, err := snapshot()
	if err != nil {
		return Parent{}, false
	}
	me, ok := entries[uint32(pid)]
	if !ok || me.ParentProcessID == 0 {
		return Parent{}, false
	}
	parent, ok := entries[me.ParentProcessID]
	if !ok {
		return Parent{}, false
	}
	p := Parent{PID: int(parent.ProcessID), Name: windows.UTF16ToString(parent.ExeFile[:])}
	if ct, ok := created(me.ProcessID); ok {
		if pt, ok := created(parent.ProcessID); ok && pt > ct {
			p.Reused = true
		}
	}
	if cl, ok := commandLine(parent.ProcessID); ok {
		p.Cmdline, p.CmdlineKnown = cl, true
	} else if !sameSession(parent.ProcessID) {
		p.CmdlineKnown = true
	}
	return p, true
}

// created is a process's creation time, when the OS lets this user ask.
func created(pid uint32) (int64, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(h)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, false
	}
	return creation.Nanoseconds(), true
}

// sameSession reports whether pid runs in this process's session. A
// failure to tell counts as the same session, so the command line stays
// unknown and the walk refuses.
func sameSession(pid uint32) bool {
	var mine, theirs uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &mine); err != nil {
		return true
	}
	if err := windows.ProcessIdToSessionId(pid, &theirs); err != nil {
		return true
	}
	return mine == theirs
}

// commandLine reads a process's command line from its PEB. ok is false when
// the process cannot be opened or read.
func commandLine(pid uint32) (string, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	var pbi windows.PROCESS_BASIC_INFORMATION
	var n uint32
	if err := windows.NtQueryInformationProcess(h, windows.ProcessBasicInformation, unsafe.Pointer(&pbi), uint32(unsafe.Sizeof(pbi)), &n); err != nil {
		return "", false
	}
	if pbi.PebBaseAddress == nil {
		return "", false
	}
	var peb windows.PEB
	if err := windows.ReadProcessMemory(h, uintptr(unsafe.Pointer(pbi.PebBaseAddress)), (*byte)(unsafe.Pointer(&peb)), unsafe.Sizeof(peb), nil); err != nil {
		return "", false
	}
	if peb.ProcessParameters == nil {
		return "", false
	}
	var params windows.RTL_USER_PROCESS_PARAMETERS
	if err := windows.ReadProcessMemory(h, uintptr(unsafe.Pointer(peb.ProcessParameters)), (*byte)(unsafe.Pointer(&params)), unsafe.Sizeof(params), nil); err != nil {
		return "", false
	}
	cl := params.CommandLine
	if cl.Length == 0 || cl.Buffer == nil {
		return "", false
	}
	buf := make([]uint16, cl.Length/2)
	if err := windows.ReadProcessMemory(h, uintptr(unsafe.Pointer(cl.Buffer)), (*byte)(unsafe.Pointer(&buf[0])), uintptr(cl.Length), nil); err != nil {
		return "", false
	}
	return windows.UTF16ToString(buf), true
}
