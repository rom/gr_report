# 0001 – Go with the standard library only, one static binary

* Status: Accepted
* Date: 2026-11-16
* ASRs: C-1, Q-2, Q-3, Q-10

## Context

The tool is used for the duration of an exercise, often on an isolated
network, and is set up by people whose main job is the exercise, not
operating software. It handles credentials and must be "secure". Go is a
given constraint.

## Decision

Use only the Go standard library. The module has no `require` lines. Build
with `CGO_ENABLED=0` into a single static binary with templates and CSS
embedded (`embed`).

This is feasible because the standard library covers everything needed:
`net/http` with pattern routing, `html/template`, `crypto/tls`,
`crypto/pbkdf2` (Go 1.24), `log/slog`, and `syscall` for Landlock, seccomp
and capabilities.

## Alternatives considered

* **Web framework / router (chi, echo, gin).** Saves little since Go 1.22
  routing; adds supply-chain surface.
* **`golang.org/x/sys` and `golang.org/x/crypto`.** Would give named
  constants for system calls and Argon2/bcrypt. Rejected to keep the
  dependency count at zero; the cost is a hand-maintained system call table
  for two architectures (generated from kernel headers and spot-checked in
  a test) and PBKDF2 instead of a memory-hard hash.
* **Landlock/seccomp helper libraries.** Same trade-off; the needed subset
  is about 250 lines.

## Consequences

* Nothing to audit or update except the Go toolchain itself.
* Installation is copying one file; cross-compilation is trivial.
* The binary is not position independent: Go's internal linker can only
  produce a PIE that needs the system's dynamic loader, and a fully static
  binary was preferred over ASLR of the text segment in a memory-safe
  language.
* `CGO_ENABLED=0` is required for the full sandbox
  (`syscall.AllThreadsSyscall`), and also removes libc from the attack
  surface.
* Supporting another Linux architecture means adding one system call table
  file; without it the seccomp filter is skipped and `strict` mode refuses
  to start.
* PBKDF2 is weaker against GPU attacks than Argon2id. Mitigated by 600 000
  iterations, generated one-time passwords and login throttling. Revisit if
  the standard library gains a memory-hard function.
