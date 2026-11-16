//go:build linux

package sandbox

// The tests in this file run in source order and the file sorts last on
// purpose: TestApplyInProcess confines the test binary itself and must be
// the final test of the package.

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

const helperEnv = "GR_REPORT_SANDBOX_HELPER"

type helperResult struct {
	Status Status
	Err    string
	Checks map[string]string // name -> "" (allowed) or the error text
}

func TestMain(m *testing.M) {
	if dir := os.Getenv(helperEnv); dir != "" {
		runHelper(dir)
		return
	}
	os.Exit(m.Run())
}

// runHelper is executed in a child process: it applies the sandbox the way
// the server does and reports what is still possible.
func runHelper(dir string) {
	res := helperResult{Checks: map[string]string{}}
	rw, ro := filepath.Join(dir, "rw"), filepath.Join(dir, "ro")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		res.Err = err.Error()
	}
	journal, err := os.OpenFile(filepath.Join(rw, "journal"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		res.Err = err.Error()
	}
	st, err := Apply(Config{Mode: Mode(os.Getenv(helperEnv + "_MODE")), ReadWrite: []string{rw}, ReadOnly: []string{ro}})
	res.Status = st
	if err != nil {
		res.Err = err.Error()
	}
	check := func(name string, err error) {
		if err != nil {
			res.Checks[name] = err.Error()
		} else {
			res.Checks[name] = ""
		}
	}
	_, err = journal.WriteString("append\n")
	check("append_open_journal", err)
	check("create_in_rw", os.WriteFile(filepath.Join(rw, "new"), []byte("x"), 0o600))
	check("rename_in_rw", os.Rename(filepath.Join(rw, "new"), filepath.Join(rw, "renamed")))
	_, err = os.ReadFile(filepath.Join(ro, "cert.pem"))
	check("read_ro", err)
	check("write_ro", os.WriteFile(filepath.Join(ro, "evil"), []byte("x"), 0o600))
	_, err = os.ReadFile("/etc/passwd")
	check("read_etc_passwd", err)
	check("write_outside", os.WriteFile(filepath.Join(dir, "outside"), []byte("x"), 0o600))
	check("mkdir_in_rw", os.Mkdir(filepath.Join(rw, "sub"), 0o700))
	check("exec", exec.Command("/bin/true").Run())
	_, err = net.Dial("tcp", ln.Addr().String())
	check("tcp_connect", err)
	_, err = net.Listen("tcp", "127.0.0.1:0")
	check("tcp_listen", err)
	_, err = net.ListenPacket("udp", "127.0.0.1:0")
	check("udp_socket", err)
	_, _, e := syscall.RawSyscall(syscall.SYS_PTRACE, 0, 0, 0)
	if e == syscall.EPERM {
		check("ptrace", e)
	} else {
		check("ptrace", nil)
	}
	check("setuid", syscall.Setuid(os.Getuid()))
	// The already open listener must keep working.
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
		}
	}()
	_ = json.NewEncoder(os.Stdout).Encode(res)
}

func runChild(t *testing.T, mode Mode) helperResult {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"rw", "ro"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ro", "cert.pem"), []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), helperEnv+"="+dir, helperEnv+"_MODE="+string(mode), "GOCOVERDIR="+t.TempDir())
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}
	var res helperResult
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("helper output: %v\n%s", err, out)
	}
	return res
}

func TestEnforcement(t *testing.T) {
	res := runChild(t, ModeAuto)
	t.Logf("status: %+v", res.Status)
	if res.Err != "" {
		t.Fatalf("Apply: %s", res.Err)
	}
	st := res.Status
	if !st.CoreDisabled || !st.Seccomp || !st.NoNewPrivs {
		t.Errorf("basic hardening missing: %+v", st)
	}
	expect := func(name string, allowed bool) {
		t.Helper()
		msg, ok := res.Checks[name]
		if !ok {
			t.Errorf("%s: no result", name)
		} else if allowed && msg != "" {
			t.Errorf("%s: should work inside the sandbox, got: %s", name, msg)
		} else if !allowed && msg == "" {
			t.Errorf("%s: should be blocked by the sandbox", name)
		}
	}
	// seccomp
	for _, name := range []string{"exec", "ptrace", "setuid", "tcp_listen", "udp_socket", "tcp_connect"} {
		expect(name, false)
	}
	// What the server needs keeps working.
	for _, name := range []string{"append_open_journal", "create_in_rw", "rename_in_rw", "read_ro"} {
		expect(name, true)
	}
	if st.LandlockABI == 0 {
		t.Log("Landlock unavailable (old kernel or cgo build): file system checks skipped")
		return
	}
	for _, name := range []string{"write_ro", "read_etc_passwd", "write_outside", "mkdir_in_rw"} {
		expect(name, false)
	}
	if !st.CapsDropped {
		t.Errorf("capabilities not dropped: %+v", st)
	}
}

func TestStrictMode(t *testing.T) {
	res := runChild(t, ModeStrict)
	if res.Status.Complete() != (res.Err == "") {
		t.Errorf("strict mode: complete=%v but err=%q", res.Status.Complete(), res.Err)
	}
}

func TestDeniedSyscallTable(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("no table for this architecture")
	}
	// Spot-check the generated table against package syscall.
	want := map[string]uintptr{"execve": syscall.SYS_EXECVE, "ptrace": syscall.SYS_PTRACE, "mount": syscall.SYS_MOUNT,
		"socket": syscall.SYS_SOCKET, "connect": syscall.SYS_CONNECT, "bind": syscall.SYS_BIND, "setuid": syscall.SYS_SETUID,
		"unshare": syscall.SYS_UNSHARE, "chroot": syscall.SYS_CHROOT, "reboot": syscall.SYS_REBOOT}
	for name, nr := range want {
		if deniedSyscalls[name] != uint32(nr) {
			t.Errorf("%s = %d, want %d", name, deniedSyscalls[name], nr)
		}
	}
	nrs := deniedNumbers()
	for i := 1; i < len(nrs); i++ {
		if nrs[i] <= nrs[i-1] {
			t.Fatalf("deny list not sorted/unique at %d", i)
		}
	}
	// The server must keep these.
	needed := map[uint32]string{uint32(syscall.SYS_READ): "read", uint32(syscall.SYS_WRITE): "write",
		uint32(syscall.SYS_ACCEPT4): "accept4", uint32(syscall.SYS_FSYNC): "fsync", uint32(syscall.SYS_OPENAT): "openat",
		uint32(syscall.SYS_CLONE): "clone", uint32(syscall.SYS_FUTEX): "futex", uint32(syscall.SYS_MMAP): "mmap"}
	for _, nr := range nrs {
		if name, ok := needed[nr]; ok {
			t.Errorf("%s is denied", name)
		}
	}
}

func TestHandledFS(t *testing.T) {
	if handledFS(1)&(llFSRefer|llFSTruncate|llFSIoctlDev) != 0 {
		t.Error("ABI 1 must not handle newer rights")
	}
	if handledFS(2)&llFSRefer == 0 || handledFS(3)&llFSTruncate == 0 || handledFS(5)&llFSIoctlDev == 0 {
		t.Error("newer rights not handled")
	}
	if handledFS(7)&llReadWrite != llReadWrite {
		t.Error("read-write rights must be handled")
	}
}

// TestApplyInProcess must stay the last test: afterwards this process can
// no longer start child processes or open sockets.
func TestApplyInProcess(t *testing.T) {
	// A missing path is a configuration error, never silently ignored.
	if landlockABI() > 0 {
		if _, err := Apply(Config{Mode: ModeAuto, ReadWrite: []string{"/nonexistent-gr-report-path"}}); err == nil {
			if _, _, e := syscall.AllThreadsSyscall(syscall.SYS_GETPID, 0, 0, 0); e != syscall.ENOTSUP {
				t.Error("missing sandbox path accepted")
			}
		}
	}
	// No t.TempDir here: removing directories is not permitted afterwards.
	tmp, err := os.CreateTemp("", "gr_report-cert-*.pem")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	file := tmp.Name()
	defer os.Remove(file)
	// Keep the whole file system accessible so the test binary can still
	// write its coverage profile; enforcement is checked in TestEnforcement.
	st, err := Apply(Config{Mode: ModeAuto, ReadWrite: []string{"/"}, ReadOnly: []string{file}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("status: %+v", st)
	if !st.Seccomp || !st.NoNewPrivs || !st.CoreDisabled {
		t.Errorf("status = %+v", st)
	}
	if err := syscall.Exec("/bin/true", []string{"true"}, nil); !errors.Is(err, syscall.EPERM) {
		t.Errorf("execve after sandbox: %v, want EPERM", err)
	}
	if _, err := net.Listen("tcp", "127.0.0.1:0"); err == nil {
		t.Error("socket creation still possible")
	}
	if _, err := os.ReadFile(file); err != nil {
		t.Errorf("read of allowed file failed: %v", err)
	}
	// Applying twice only adds restrictions and must not fail.
	if _, err := Apply(Config{Mode: ModeAuto, ReadWrite: []string{"/"}}); err != nil {
		t.Errorf("second Apply: %v", err)
	}
	if st.Complete() {
		if _, err := Apply(Config{Mode: ModeStrict, ReadWrite: []string{"/"}}); err != nil {
			t.Errorf("strict Apply on a capable kernel: %v", err)
		}
	}
}
