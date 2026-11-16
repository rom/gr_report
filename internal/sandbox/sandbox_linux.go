//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"syscall"
	"unsafe"
)

const platform = "linux"

// oPath is O_PATH, which package syscall does not export. The value is the
// same on all architectures supported here.
const oPath = 0x200000

// Landlock UAPI, <linux/landlock.h>. The system call numbers are the same
// on every architecture.
const (
	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446

	landlockCreateRulesetVersion = 1

	llRulePathBeneath = 1

	llFSExecute    = 1 << 0
	llFSWriteFile  = 1 << 1
	llFSReadFile   = 1 << 2
	llFSReadDir    = 1 << 3
	llFSRemoveDir  = 1 << 4
	llFSRemoveFile = 1 << 5
	llFSMakeChar   = 1 << 6
	llFSMakeDir    = 1 << 7
	llFSMakeReg    = 1 << 8
	llFSMakeSock   = 1 << 9
	llFSMakeFifo   = 1 << 10
	llFSMakeBlock  = 1 << 11
	llFSMakeSym    = 1 << 12
	llFSRefer      = 1 << 13 // ABI 2
	llFSTruncate   = 1 << 14 // ABI 3
	llFSIoctlDev   = 1 << 15 // ABI 5

	llNetBindTCP    = 1 << 0 // ABI 4
	llNetConnectTCP = 1 << 1 // ABI 4

	llScopeAbstractUnixSocket = 1 << 0 // ABI 6

	// Rights that make sense on a regular file (others give EINVAL).
	llFSFileRights = llFSExecute | llFSWriteFile | llFSReadFile | llFSTruncate | llFSIoctlDev

	llReadOnly  = llFSReadFile | llFSReadDir
	llReadWrite = llReadOnly | llFSWriteFile | llFSRemoveFile | llFSMakeReg | llFSTruncate
)

const (
	prSetDumpable    = 4
	prCapBSetDrop    = 24
	prSetNoNewPrivs  = 38
	prCapAmbient     = 47
	prCapAmbientClrA = 4

	capV3     = 0x20080522
	capLastOK = 63

	seccompSetModeFilter   = 1
	seccompFilterFlagTSync = 1
)

type landlockRulesetAttr struct {
	HandledAccessFS  uint64
	HandledAccessNet uint64
	Scoped           uint64
}

type sockFprog struct {
	Len    uint16
	_      [6]byte
	Filter *sockFilter
}

type capHeader struct {
	Version uint32
	Pid     int32
}

type capData struct{ Effective, Permitted, Inheritable uint32 }

// allThreads runs a system call on every thread of the process.
func allThreads(trap, a1, a2, a3 uintptr) error {
	_, _, e := syscall.AllThreadsSyscall(trap, a1, a2, a3)
	if e != 0 {
		return e
	}
	return nil
}

func apply(cfg Config) (Status, error) {
	st := Status{Platform: platform}

	// No core dumps and no ptrace attach by same-uid processes: both
	// could leak password hashes and session tokens.
	if err := syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{}); err != nil {
		st.note("RLIMIT_CORE: %v", err)
	} else if _, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, prSetDumpable, 0, 0); e != 0 {
		st.note("PR_SET_DUMPABLE: %v", e)
	} else {
		st.CoreDisabled = true
	}

	perThread := true
	if err := allThreads(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0); err != nil {
		if errors.Is(err, syscall.ENOTSUP) {
			// cgo build: per-thread calls are impossible, Landlock and the
			// capability drop are skipped; seccomp still works via TSYNC.
			perThread = false
			st.note("built with cgo: Landlock and capability drop unavailable (build with CGO_ENABLED=0)")
		} else {
			st.note("no_new_privs: %v", err)
		}
	} else {
		st.NoNewPrivs = true
	}

	if perThread {
		if err := dropCapabilities(); err != nil {
			st.note("capabilities: %v", err)
		} else {
			st.CapsDropped = true
		}
		if err := applyLandlock(cfg, &st); err != nil {
			return st, fmt.Errorf("landlock: %w", err)
		}
	}

	if err := applySeccomp(); err != nil {
		st.note("seccomp: %v", err)
	} else {
		st.Seccomp = true
		st.NoNewPrivs = true
	}
	if os.Geteuid() == 0 {
		st.note("running as root; use a dedicated unprivileged user")
	}
	return st, nil
}

// dropCapabilities empties the bounding, ambient, effective, permitted and
// inheritable capability sets of all threads.
func dropCapabilities() error {
	for c := uintptr(0); c <= capLastOK; c++ {
		// Fails with EPERM without CAP_SETPCAP and EINVAL above the last
		// capability; neither matters once the permitted set is empty.
		_ = allThreads(syscall.SYS_PRCTL, prCapBSetDrop, c, 0)
	}
	_ = allThreads(syscall.SYS_PRCTL, prCapAmbient, prCapAmbientClrA, 0)
	// A service started by systemd with an empty CapabilityBoundingSet has
	// nothing left to drop, and capset may be filtered there: look first.
	if capsEmpty() {
		return nil
	}
	hdr := capHeader{Version: capV3}
	var data [2]capData
	err := allThreads(syscall.SYS_CAPSET, uintptr(unsafe.Pointer(&hdr)), uintptr(unsafe.Pointer(&data[0])), 0)
	runtime.KeepAlive(&hdr)
	runtime.KeepAlive(&data)
	return err
}

// capsEmpty reports whether the calling thread holds no capabilities. All
// threads share the same sets because nothing in the process changes them
// per thread.
func capsEmpty() bool {
	hdr := capHeader{Version: capV3}
	var data [2]capData
	if _, _, e := syscall.RawSyscall(syscall.SYS_CAPGET, uintptr(unsafe.Pointer(&hdr)), uintptr(unsafe.Pointer(&data[0])), 0); e != 0 {
		return false
	}
	for _, d := range data {
		if d.Effective|d.Permitted|d.Inheritable != 0 {
			return false
		}
	}
	return true
}

func landlockABI() int {
	v, _, e := syscall.Syscall(sysLandlockCreateRuleset, 0, 0, landlockCreateRulesetVersion)
	if e != 0 {
		return 0
	}
	return int(v)
}

// handledFS returns the file system rights known to the given ABI. All of
// them are denied unless a rule grants them.
func handledFS(abi int) uint64 {
	fs := uint64(llFSExecute | llFSWriteFile | llFSReadFile | llFSReadDir | llFSRemoveDir |
		llFSRemoveFile | llFSMakeChar | llFSMakeDir | llFSMakeReg | llFSMakeSock |
		llFSMakeFifo | llFSMakeBlock | llFSMakeSym)
	if abi >= 2 {
		fs |= llFSRefer
	}
	if abi >= 3 {
		fs |= llFSTruncate
	}
	if abi >= 5 {
		fs |= llFSIoctlDev
	}
	return fs
}

func applyLandlock(cfg Config, st *Status) error {
	abi := landlockABI()
	if abi < 1 {
		st.note("Landlock not supported by this kernel")
		return nil
	}
	attr := landlockRulesetAttr{HandledAccessFS: handledFS(abi)}
	size := unsafe.Sizeof(attr.HandledAccessFS)
	if abi >= 4 {
		// The server already holds its listening socket: deny every new
		// TCP bind and connect.
		attr.HandledAccessNet = llNetBindTCP | llNetConnectTCP
		size += unsafe.Sizeof(attr.HandledAccessNet)
	} else {
		st.note("Landlock ABI %d: no network rules (needs Linux 6.7)", abi)
	}
	if abi >= 6 {
		attr.Scoped = llScopeAbstractUnixSocket
		size += unsafe.Sizeof(attr.Scoped)
	}
	fd, _, e := syscall.Syscall(sysLandlockCreateRuleset, uintptr(unsafe.Pointer(&attr)), size, 0)
	if e != 0 {
		return fmt.Errorf("create ruleset: %w", e)
	}
	defer syscall.Close(int(fd))

	add := func(path string, access uint64) error {
		pfd, err := syscall.Open(path, oPath|syscall.O_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		defer syscall.Close(pfd)
		var stt syscall.Stat_t
		if err := syscall.Fstat(pfd, &stt); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if stt.Mode&syscall.S_IFMT != syscall.S_IFDIR {
			access &= llFSFileRights
		}
		access &= attr.HandledAccessFS
		// The kernel struct is packed: 8 bytes access + 4 bytes fd.
		var raw [12]byte
		*(*uint64)(unsafe.Pointer(&raw[0])) = access
		*(*int32)(unsafe.Pointer(&raw[8])) = int32(pfd)
		if _, _, e := syscall.Syscall6(sysLandlockAddRule, fd, llRulePathBeneath,
			uintptr(unsafe.Pointer(&raw[0])), 0, 0, 0); e != 0 {
			return fmt.Errorf("%s: add rule: %w", path, e)
		}
		return nil
	}
	for _, p := range cfg.ReadWrite {
		if err := add(p, llReadWrite); err != nil {
			return err
		}
	}
	for _, p := range cfg.ReadOnly {
		if err := add(p, llReadOnly); err != nil {
			return err
		}
	}
	if err := allThreads(sysLandlockRestrictSelf, fd, 0, 0); err != nil {
		return fmt.Errorf("restrict self: %w", err)
	}
	st.LandlockABI = abi
	st.LandlockNet = abi >= 4
	return nil
}

func deniedNumbers() []uint32 {
	nrs := make([]uint32, 0, len(deniedSyscalls))
	for _, nr := range deniedSyscalls {
		nrs = append(nrs, nr)
	}
	sort.Slice(nrs, func(i, j int) bool { return nrs[i] < nrs[j] })
	return nrs
}

func applySeccomp() error {
	if len(deniedSyscalls) == 0 {
		return fmt.Errorf("no filter for %s", runtime.GOARCH)
	}
	prog := buildFilter(auditArch, rejectX32, deniedNumbers())
	fprog := sockFprog{Len: uint16(len(prog)), Filter: &prog[0]}
	// TSYNC needs no_new_privs on the calling thread and then applies both
	// to all threads atomically.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0); e != 0 {
		return fmt.Errorf("no_new_privs: %w", e)
	}
	_, _, e := syscall.RawSyscall(sysSeccomp, seccompSetModeFilter, seccompFilterFlagTSync,
		uintptr(unsafe.Pointer(&fprog)))
	runtime.KeepAlive(prog)
	if e != 0 {
		return e
	}
	return nil
}
