//go:build linux && arm64

package sandbox

// Generated from the kernel UAPI headers; see docs/security.md.

const (
	auditArch  = 0xC00000B7 // AUDIT_ARCH_AARCH64
	sysSeccomp = 277
	rejectX32  = false
)

// deniedSyscalls maps the names of denied system calls to their numbers.
var deniedSyscalls = map[string]uint32{
	"execve":            221,
	"execveat":          281,
	"ptrace":            117,
	"mount":             40,
	"umount2":           39,
	"pivot_root":        41,
	"chroot":            51,
	"swapon":            224,
	"swapoff":           225,
	"reboot":            142,
	"kexec_load":        104,
	"kexec_file_load":   294,
	"init_module":       105,
	"finit_module":      273,
	"delete_module":     106,
	"bpf":               280,
	"perf_event_open":   241,
	"userfaultfd":       282,
	"process_vm_readv":  270,
	"process_vm_writev": 271,
	"setuid":            146,
	"setgid":            144,
	"setreuid":          145,
	"setregid":          143,
	"setresuid":         147,
	"setresgid":         149,
	"setgroups":         159,
	"setfsuid":          151,
	"setfsgid":          152,
	"unshare":           97,
	"setns":             268,
	"keyctl":            219,
	"add_key":           217,
	"request_key":       218,
	"acct":              89,
	"quotactl":          60,
	"settimeofday":      170,
	"clock_settime":     112,
	"adjtimex":          171,
	"clock_adjtime":     266,
	"sethostname":       161,
	"setdomainname":     162,
	"personality":       92,
	"mknodat":           33,
	"io_uring_setup":    425,
	"io_uring_enter":    426,
	"io_uring_register": 427,
	"open_tree":         428,
	"move_mount":        429,
	"fsopen":            430,
	"fsconfig":          431,
	"fsmount":           432,
	"fspick":            433,
	"pidfd_getfd":       438,
	"mount_setattr":     442,
	"kcmp":              272,
	"socket":            198,
	"connect":           203,
	"bind":              200,
	"listen":            201,
	"socketpair":        199,
	"vhangup":           58,
	"syslog":            116,
}
