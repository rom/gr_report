package sandbox

// sockFilter is struct sock_filter from <linux/filter.h>.
type sockFilter struct {
	Code uint16
	Jt   uint8
	Jf   uint8
	K    uint32
}

const (
	bpfLdAbsW = 0x20 // BPF_LD | BPF_W | BPF_ABS
	bpfJeqK   = 0x15 // BPF_JMP | BPF_JEQ | BPF_K
	bpfJgeK   = 0x35 // BPF_JMP | BPF_JGE | BPF_K
	bpfRetK   = 0x06 // BPF_RET | BPF_K

	seccompRetKillProcess = 0x80000000
	seccompRetErrno       = 0x00050000
	seccompRetAllow       = 0x7fff0000

	offNr   = 0 // offsetof(struct seccomp_data, nr)
	offArch = 4 // offsetof(struct seccomp_data, arch)

	errnoEPERM = 1

	// x32Bit marks x32 ABI syscalls on x86-64.
	x32Bit = 0x40000000
)

// buildFilter returns a classic BPF program that kills the process when a
// system call is made with a foreign ABI and makes the listed system calls
// fail with EPERM. Everything else is allowed: the allow-list lives in the
// systemd unit (SystemCallFilter=), the in-process filter is a deny-list of
// calls a compromised server would want and the server never needs.
func buildFilter(arch uint32, rejectX32 bool, denied []uint32) []sockFilter {
	if len(denied) > 250 {
		panic("sandbox: too many denied syscalls for 8-bit jump offsets")
	}
	p := []sockFilter{
		{bpfLdAbsW, 0, 0, offArch},
		{bpfJeqK, 1, 0, arch},
		{bpfRetK, 0, 0, seccompRetKillProcess},
		{bpfLdAbsW, 0, 0, offNr},
	}
	if rejectX32 {
		p = append(p,
			sockFilter{bpfJgeK, 0, 1, x32Bit},
			sockFilter{bpfRetK, 0, 0, seccompRetKillProcess})
	}
	n := len(denied)
	for i, nr := range denied {
		// On match jump over the remaining comparisons and the ALLOW.
		p = append(p, sockFilter{bpfJeqK, uint8(n - i), 0, nr})
	}
	p = append(p,
		sockFilter{bpfRetK, 0, 0, seccompRetAllow},
		sockFilter{bpfRetK, 0, 0, seccompRetErrno | errnoEPERM})
	return p
}

// evalFilter interprets a program produced by buildFilter. It exists so the
// filter logic can be unit tested and fuzzed on any platform.
func evalFilter(p []sockFilter, arch, nr uint32) uint32 {
	var acc uint32
	for pc := 0; pc < len(p); pc++ {
		in := p[pc]
		switch in.Code {
		case bpfLdAbsW:
			if in.K == offArch {
				acc = arch
			} else {
				acc = nr
			}
		case bpfJeqK:
			if acc == in.K {
				pc += int(in.Jt)
			} else {
				pc += int(in.Jf)
			}
		case bpfJgeK:
			if acc >= in.K {
				pc += int(in.Jt)
			} else {
				pc += int(in.Jf)
			}
		case bpfRetK:
			return in.K
		}
	}
	return seccompRetKillProcess
}
