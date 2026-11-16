# Testing

## Running

```bash
make test
```

```bash
make cover
```

```bash
make fuzz FUZZTIME=30s
```

`make cover` runs the whole suite with `-covermode=atomic`, prints the
per-package and total statement coverage and **fails below 85 %**
(`make cover COVER_MIN=90` raises the bar). `make cover-html` writes
`coverage.html`.

## What is tested where

| Package | Kind | Highlights |
| --- | --- | --- |
| `internal/model` | Unit, table-driven | Validation of users, templates, requests, reports per field type; key derivation; filter, search, sort; deep copies |
| `internal/store` | Unit with real files | CRUD, ordering, persistence across reopen, compaction, directory lock, torn final record, corruption in the middle, failed writes leave state untouched |
| `internal/auth` | Unit with fake clock | Hash format and verification, policy, session idle/absolute expiry, eviction, flash, throttling back-off and bounds |
| `internal/web` | Integration over HTTP (`httptest`) with a real store | Login/logout, cookie attributes, security headers, throttling, CSRF and origin checks, role matrix for every route, object-level access, the full report / queue / request / template / account flows, escaping of hostile input, body limit, stale sessions, storage failure |
| `internal/app` | Integration | Flag and environment parsing, first-run bootstrap, HTTP and TLS serving, TLS minimum version, certificate reload (good and broken), start-up errors, graceful shutdown |
| `internal/sandbox` | Unit + child process | BPF program against an interpreter; **enforcement**: a child applies the sandbox and tries exec, sockets, connect, ptrace, setuid, forbidden reads and writes; strict mode; in-process apply |
| `cmd/gr_report` | Unit + end-to-end | Exit codes; builds the binary, runs it with TLS and the sandbox, logs in, writes, reloads the certificate with SIGHUP, shuts down with SIGTERM |

The role matrix in `TestRoleAccess` is the checklist for authorisation:
every new route belongs in it.

## Fuzz targets

| Target | Input | Property |
| --- | --- | --- |
| `model.FuzzFillReport` | report form values | no panic; accepted reports respect limits, are clean UTF-8, select/checkbox values are legal |
| `model.FuzzTemplateValidate` | template definitions | accepted templates have unique valid keys; validation is idempotent |
| `model.FuzzFilter` | search query, sort key | bounded terms; empty query matches all; sorting never panics |
| `model.FuzzClean` | arbitrary bytes | output is valid UTF-8 without control characters; cleaning is idempotent |
| `store.FuzzReplay` | journal bytes | no panic; accepted state survives compaction and replay unchanged |
| `auth.FuzzVerify` | stored hash, password | malformed hashes never verify |
| `web.FuzzQueueParams` | query string | parameters are normalised and round-trip; generated links are safe |
| `web.FuzzHandlers` | path, query, form body as leader and group | never a 5xx; never unescaped markup |
| `sandbox.FuzzFilter` | deny list, system call, ABI | generated BPF equals the reference policy |

The seed corpus of each target runs as a normal test in `make test`.
A failing input found by the fuzzer is written to
`testdata/fuzz/<Target>/` in the package; commit it so it stays a
regression test.

Run a single target for longer:

```bash
go test -run '^$' -fuzz '^FuzzReplay$' -fuzztime 10m ./internal/store
```

## Platform notes

* The sandbox enforcement tests need Linux. They adapt to the kernel: on a
  kernel without Landlock the file-system assertions are skipped and the
  test log says so.
* In a cgo build (`make test-race`) Landlock and the capability drop cannot
  be applied to all threads; those assertions are skipped there and covered
  by the `CGO_ENABLED=0` run.
* `TestApplyInProcess` confines the test process itself and is therefore
  the last test of its package; do not add tests after it or run that
  package with `-shuffle`.
* The end-to-end test needs `openssl` and the Go toolchain; it is skipped
  with `-short`.
* On macOS the Linux-only test files are excluded by build tags.

## Not covered by automated tests

* Behaviour under a running systemd with the shipped unit.
* SELinux and AppArmor in enforcing mode.
* Let's Encrypt issuance (`mkcert-letsencrypt.sh` needs a public host).
* Visual layout in browsers.

`make verify-deploy` syntax-checks the unit, the profile and the policy
module; see [security.md](security.md) for how to verify them on a target
host.
