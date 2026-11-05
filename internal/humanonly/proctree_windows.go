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
// name from the snapshot, its command line from its PEB (empty when the
// process cannot be opened or read, as for another user's process).
func parentOf(pid int) (int, string, string, bool) {
	entries, err := snapshot()
	if err != nil {
		return 0, "", "", false
	}
	me, ok := entries[uint32(pid)]
	if !ok {
		return 0, "", "", false
	}
	parent, ok := entries[me.ParentProcessID]
	if !ok || parent.ProcessID == 0 {
		return 0, "", "", false
	}
	return int(parent.ProcessID), windows.UTF16ToString(parent.ExeFile[:]), commandLine(parent.ProcessID), true
}

// commandLine reads a process's command line from its PEB. Any failure
// returns "": the walker then matches the executable name alone.
func commandLine(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var pbi windows.PROCESS_BASIC_INFORMATION
	var n uint32
	if err := windows.NtQueryInformationProcess(h, windows.ProcessBasicInformation, unsafe.Pointer(&pbi), uint32(unsafe.Sizeof(pbi)), &n); err != nil {
		return ""
	}
	if pbi.PebBaseAddress == nil {
		return ""
	}
	var peb windows.PEB
	if err := windows.ReadProcessMemory(h, uintptr(unsafe.Pointer(pbi.PebBaseAddress)), (*byte)(unsafe.Pointer(&peb)), unsafe.Sizeof(peb), nil); err != nil {
		return ""
	}
	if peb.ProcessParameters == nil {
		return ""
	}
	var params windows.RTL_USER_PROCESS_PARAMETERS
	if err := windows.ReadProcessMemory(h, uintptr(unsafe.Pointer(peb.ProcessParameters)), (*byte)(unsafe.Pointer(&params)), unsafe.Sizeof(params), nil); err != nil {
		return ""
	}
	cl := params.CommandLine
	if cl.Length == 0 || cl.Buffer == nil {
		return ""
	}
	buf := make([]uint16, cl.Length/2)
	if err := windows.ReadProcessMemory(h, uintptr(unsafe.Pointer(cl.Buffer)), (*byte)(unsafe.Pointer(&buf[0])), uintptr(cl.Length), nil); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf)
}
