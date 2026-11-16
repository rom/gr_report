# 0009 – Testing strategy: coverage floor, fuzzing, real sandbox tests

* Status: Accepted
* Date: 2026-11-16
* ASRs: Q-9, Q-3, Q-5

## Context

The brief requires tests, code coverage with a floor of 85 % and fuzzing.
Security controls and crash recovery are the parts where a regression is
most costly and least visible.

## Decision

* **Structure for testability.** Domain rules live in a pure package
  (`model`); clocks are injected; the HTTP layer is tested in-process with
  `httptest` against a real store in a temporary directory.
* **Coverage floor.** `make cover` fails when total statement coverage of
  the module is below 85 % (`COVER_MIN`). The floor is on the total, not
  per package, because `cmd/` and the Linux system call paths have
  inherently unreachable error branches.
* **Fuzzing** with Go's native fuzzer for everything that parses untrusted
  or stored input: report input, template definitions, search/sort, text
  cleaning, the journal parser (with a compaction round-trip property),
  stored password hashes, queue parameters, the whole HTTP router (no 5xx,
  no unescaped markup), and the BPF generator against a reference policy.
  `make fuzz` runs each target for a fixed time; crashers land in
  `testdata/fuzz` and become regression tests.
* **Real enforcement tests.** The sandbox is tested by re-executing the
  test binary as a child that confines itself and then attempts forbidden
  operations. An end-to-end test builds the actual binary and runs it
  sandboxed with TLS.
* **Two build modes.** The coverage run uses `CGO_ENABLED=0` (the release
  configuration, full sandbox); a separate `-race` run uses cgo.
* **Static checks.** `gofmt`, `go vet` for Linux amd64/arm64 and macOS,
  `golangci-lint`, `shellcheck`, and validation of the systemd, AppArmor
  and SELinux files where the tools are installed.

## Alternatives considered

* **Per-package floors.** Stricter, but forces contrived tests for
  `main()` and for kernel failure paths.
* **Browser automation in CI.** Valuable for layout, but heavy; since there
  is no client-side logic, form-level HTTP tests cover the behaviour.
* **Mocking the store.** The real store is fast enough and tests then also
  cover persistence.

## Consequences

* A change that drops coverage below the floor or breaks a security
  property fails `make check` and CI.
* Sandbox tests depend on kernel features; they adapt to what the kernel
  offers and log what was skipped.
* The last test in `internal/sandbox` confines the test process itself and
  must remain last (the file is named `zz_linux_test.go` for that reason).
* SELinux and AppArmor enforcement is outside automated testing and must be
  checked on the target distribution.
