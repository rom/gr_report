# 0010 – A small set of vetted Go modules; still one static binary

* Status: Accepted (supersedes [0001](0001-go-standard-library-only.md))
* Date: 2026-11-16
* ASRs: C-1, C-5, Q-2, Q-3, Q-10

## Context

[ADR 0001](0001-go-standard-library-only.md) limited the project to the Go
standard library. The project owner has since allowed Go modules, and asked
for SQLite storage ([ADR 0012](0012-sqlite-storage.md)), which the standard
library cannot provide. The properties that motivated 0001 still matter:
one file to install, a small supply chain, and the in-process sandbox,
which needs a build without cgo.

## Decision

Third-party modules are allowed when they earn their place. Rules:

1. **No cgo.** The release build stays `CGO_ENABLED=0`: fully static, no
   libc, and `syscall.AllThreadsSyscall` (Landlock, capability drop) keeps
   working. A module that needs cgo is not acceptable.
2. **Each direct dependency is named and justified** in the table below;
   adding one means amending this record.
3. **Versions are pinned** by `go.mod`/`go.sum` and must build with the Go
   version in `go.mod`, so the linters and CI use one toolchain.
4. **Prefer the standard library** when the need is small. Translation
   look-up and `Accept-Language` matching are a few dozen lines and stay
   in-house rather than pulling `golang.org/x/text`.

| Module | Used by | Why |
| --- | --- | --- |
| `modernc.org/sqlite` | `internal/store` | SQLite without cgo (a mechanical translation of the C sources to Go) |
| `golang.org/x/net/html` | tests only | HTML parser for the accessibility audit |

Everything else in `go.mod` is an indirect dependency of the SQLite driver.

## Alternatives considered

* **`github.com/mattn/go-sqlite3`.** The most used driver, but needs cgo:
  the binary would no longer be static and the sandbox would lose Landlock
  and the capability drop.
* **Keep zero dependencies and hand-write a storage engine.** That was
  ADR 0003; SQLite was asked for and is the better tool.
* **Take more from `golang.org/x`** (`x/sys` for system call numbers,
  `x/crypto` for Argon2id, `x/text` for i18n). Each is reasonable and now
  permitted, but none was needed to meet the current requirements; the
  hand-maintained system call table and PBKDF2 remain. Argon2id is noted as
  a follow-up in [ADR 0005](0005-authentication-and-sessions.md).

## Consequences

* The supply chain grows from zero to one runtime dependency with about ten
  transitive modules; the binary grows from about 8 MB to about 19 MB.
* Dependency updates become a maintenance task (`go list -m -u all`,
  `govulncheck`).
* The SQLite engine is machine-translated C running as Go: no memory
  corruption class of bugs reaching the process from the C heap, but it
  does use `unsafe` extensively. The sandbox layers still apply to it.
* Installation is unchanged: copy one file.
