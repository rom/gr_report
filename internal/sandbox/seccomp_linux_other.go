//go:build linux && !amd64 && !arm64

package sandbox

// No seccomp table for this architecture: the filter is skipped (and strict
// mode refuses to start).
const (
	auditArch  = 0
	sysSeccomp = 0
	rejectX32  = false
)

var deniedSyscalls = map[string]uint32{}
