package sandbox

import (
	"errors"
	"strings"
	"testing"
)

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{"auto": ModeAuto, "STRICT": ModeStrict, "off": ModeOff} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseMode("maybe"); err == nil {
		t.Error("unknown mode accepted")
	}
}

func TestApplyOff(t *testing.T) {
	st, err := Apply(Config{Mode: ModeOff, ReadWrite: []string{"/nonexistent"}})
	if err != nil || st.Complete() || len(st.Notes) != 1 || st.Platform == "" {
		t.Errorf("Apply(off) = %+v, %v", st, err)
	}
}

func TestStatusComplete(t *testing.T) {
	full := Status{NoNewPrivs: true, CapsDropped: true, LandlockABI: 4, LandlockNet: true, Seccomp: true, CoreDisabled: true}
	if !full.Complete() {
		t.Error("full status not complete")
	}
	partial := full
	partial.LandlockNet = false
	if partial.Complete() {
		t.Error("partial status complete")
	}
	if !errors.Is(ErrIncomplete, ErrIncomplete) {
		t.Fatal("unreachable")
	}
}

const (
	testArch  = 0xC000003E
	otherArch = 0x40000003 // AUDIT_ARCH_I386
)

func TestBuildFilter(t *testing.T) {
	denied := []uint32{59, 101, 165, 322}
	for _, x32 := range []bool{true, false} {
		p := buildFilter(testArch, x32, denied)
		for _, nr := range denied {
			if got := evalFilter(p, testArch, nr); got != seccompRetErrno|errnoEPERM {
				t.Errorf("x32=%v: syscall %d -> %#x, want EPERM", x32, nr, got)
			}
		}
		for _, nr := range []uint32{0, 1, 58, 60, 100, 321, 323, 9999} {
			if got := evalFilter(p, testArch, nr); got != seccompRetAllow {
				t.Errorf("x32=%v: syscall %d -> %#x, want ALLOW", x32, nr, got)
			}
		}
		// A foreign ABI could reach denied calls under other numbers.
		if got := evalFilter(p, otherArch, 1); got != seccompRetKillProcess {
			t.Errorf("foreign arch -> %#x, want KILL", got)
		}
		got := evalFilter(p, testArch, x32Bit|59)
		if x32 && got != seccompRetKillProcess {
			t.Errorf("x32 execve -> %#x, want KILL", got)
		}
		if !x32 && got != seccompRetAllow {
			t.Errorf("non-x86 high syscall -> %#x, want ALLOW", got)
		}
	}
	if got := evalFilter(nil, testArch, 1); got != seccompRetKillProcess {
		t.Errorf("empty program -> %#x", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("oversized deny list accepted")
		}
	}()
	buildFilter(testArch, false, make([]uint32, 251))
}

// FuzzFilter cross-checks the generated BPF program against a direct
// evaluation of the policy for arbitrary deny lists and system calls.
func FuzzFilter(f *testing.F) {
	f.Add([]byte{59, 101, 165}, uint32(59), true, true)
	f.Add([]byte{}, uint32(0), false, false)
	f.Add([]byte(strings.Repeat("\x07", 250)), uint32(7), true, false)
	f.Fuzz(func(t *testing.T, raw []byte, nr uint32, sameArch, x32 bool) {
		if len(raw) > 250 {
			raw = raw[:250]
		}
		denied := make([]uint32, len(raw))
		isDenied := false
		for i, b := range raw {
			denied[i] = uint32(b) * 3
			isDenied = isDenied || denied[i] == nr
		}
		arch := uint32(testArch)
		if !sameArch {
			arch = otherArch
		}
		want := uint32(seccompRetAllow)
		switch {
		case !sameArch, x32 && nr >= x32Bit:
			want = seccompRetKillProcess
		case isDenied:
			want = seccompRetErrno | errnoEPERM
		}
		if got := evalFilter(buildFilter(testArch, x32, denied), arch, nr); got != want {
			t.Errorf("nr=%d arch=%#x x32=%v denied=%v: got %#x, want %#x", nr, arch, x32, isDenied, got, want)
		}
	})
}
