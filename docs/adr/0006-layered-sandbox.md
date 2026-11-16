# 0006 – Layered sandbox: self-confinement after start-up plus OS policy

* Status: Accepted
* Date: 2026-11-16
* ASRs: Q-3, Q-6

## Context

The brief asks for SELinux, seccomp, systemd, Landlock, AppArmor and
capabilities. These overlap. Some are applied by the operating system
around the process, some by the process itself; some may be missing on a
given host (SELinux *or* AppArmor, old kernels), and none exist on macOS.

## Decision

Use independent layers so that losing one does not remove the protection.

1. **In the process** (`internal/sandbox`), applied after the database, the
   certificate and the listening socket are open:
   no_new_privs, all capabilities dropped, Landlock (file system: data
   directory read/write, TLS directories read-only; network: no TCP bind or
   connect), and a seccomp-BPF filter.
2. **systemd unit**: unprivileged user, empty capability bounding set,
   namespaces, read-only file system, system call allow-list.
3. **Mandatory access control**: an SELinux policy module and an AppArmor
   profile, of which a host uses one.

Specific choices:

* **Order: sandbox after listen.** The process then needs no bind
  permission at all, and a low port needs `CAP_NET_BIND_SERVICE` only for
  an instant.
* **The in-process seccomp filter is a deny-list**, the systemd filter is
  the allow-list. The Go runtime's system call usage changes between
  releases; an in-process allow-list would turn a toolchain upgrade into a
  crash at an arbitrary moment. The deny-list covers what an attacker wants
  and the server never needs (exec, new sockets, ptrace, mounts, identity
  changes, …) and kills on foreign ABIs.
* **Three modes.** `auto` degrades gracefully and logs; `strict` refuses to
  start unless everything is active (used by the shipped service
  configuration); `off` for development.
* **Platform split by build tags.** Linux code in `*_linux*.go`, a no-op in
  `sandbox_other.go`, so macOS builds and runs.
* **Landlock signal scoping is not used.** Each thread gets its own
  Landlock domain when restricted via `AllThreadsSyscall`; on kernels
  before the same-process exemption that would block the signals the Go
  runtime sends between its threads.

## Alternatives considered

* **Only systemd hardening.** No protection when started by hand or under
  another supervisor, and nothing travels with the binary.
* **Only in-process.** Misses namespaces, MAC and resource limits.
* **In-process seccomp allow-list.** See above; fragile.
* **Confining before start-up with a wrapper (bubblewrap, firejail).** An
  extra dependency, and it cannot drop the access needed only during
  start-up.

## Consequences

* A compromised request handler cannot start a program, open a socket,
  connect out, read files outside two directories, or regain privileges –
  even when run by hand without systemd.
* Tests can verify the in-process layer for real by running a child
  process under it.
* The system call table must be maintained per architecture (amd64, arm64
  today).
* `strict` needs Linux ≥ 6.7; older kernels must use `auto`.
* SELinux and AppArmor policies must be kept in step with the paths in the
  unit; they are syntax-checked by `make verify-deploy` but not exercised
  by the automated tests.
