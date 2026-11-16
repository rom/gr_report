# Development

## Prerequisites

* Go 1.24 or newer. Nothing else is needed to build and test.
* Optional: `golangci-lint`, `shellcheck`, `openssl` (end-to-end test and
  `make run`), `systemd-analyze`, `apparmor_parser`, `selinux-policy-devel`
  (for `make verify-deploy`).

Linux and macOS are supported for development. On macOS everything works
except the in-process sandbox, which is a logged no-op; the Linux-only
tests are excluded by build tags.

## Common tasks

| Command | Purpose |
| --- | --- |
| `make build` | Build `bin/gr_report` |
| `make dev` | Run on <http://127.0.0.1:8080> with demo accounts (password `exercise-dev-1`) |
| `make dev PORT=9000` | Same on another port |
| `make run` | Run with TLS on <https://127.0.0.1:8443>, creating `./tls` if needed |
| `make test` | All tests |
| `make test-short` | Without the end-to-end test that builds the binary |
| `make test-race` | With the race detector |
| `make cover` | Tests with the 85 % coverage floor |
| `make cover-html` | Coverage report in `coverage.html` |
| `make fuzz FUZZTIME=30s` | Run every fuzz target |
| `make lint` | Formatting, vet (three platforms), linters |
| `make verify-deploy` | Validate systemd, AppArmor, SELinux files |
| `make build-linux build-darwin` | Cross-compile into `dist/` |
| `make check` | Everything CI runs |
| `make help` | List all targets |

`make dev` stores data in `./data`; delete the directory to start over.
The demo accounts are `admin`, `leader`, `alpha` and `bravo`.

## Layout

```
cmd/gr_report/        entry point, end-to-end test
internal/app/         configuration, bootstrap, TLS, lifecycle
internal/web/         routes, middleware, handlers
internal/web/templates/   HTML pages (one layout)
internal/web/static/      stylesheet, icon
internal/auth/        passwords, sessions, throttling
internal/store/       journal storage
internal/model/       entities, validation, filter, sort
internal/sandbox/     Landlock, seccomp, capabilities (Linux), no-op elsewhere
scripts/              certificates, coverage gate, fuzz runner
deploy/systemd/       unit, drop-in, env file, sysusers, tmpfiles
deploy/selinux/       policy module
deploy/apparmor/      profile
docs/                 documentation, ADRs
```

## Conventions

* No third-party modules ([ADR 0001](adr/0001-go-standard-library-only.md)).
  If one seems necessary, write an ADR first.
* No JavaScript and no inline styles; the CSP forbids both
  ([ADR 0002](adr/0002-server-rendered-html-no-javascript.md)).
* All user input goes through `model` before it reaches `store`.
* A handler gets its user only through `Server.authed`; add new routes in
  `web.Handler` with an explicit role list and add them to
  `TestRoleAccess`.
* Anything that parses external or stored input gets a fuzz target.
* Platform-specific code lives behind build tags in `internal/sandbox`
  only.
* A significant design change gets a new ADR; update
  [asr.md](asr.md) and [architecture.md](architecture.md) with it.

## Adding a field type

1. Add the constant to `model.FieldTypes`.
2. Validate it in `model.FillReport`.
3. Render it in `internal/web/templates/report_new.html`.
4. Extend `TestFillReport` and the `sitrep()` fixture used by the fuzzers.

## Adding a denied system call

1. Add the name and number to both
   `internal/sandbox/seccomp_linux_amd64.go` and `…_arm64.go` (numbers from
   `asm/unistd_64.h` and `asm-generic/unistd.h`).
2. If the server could conceivably need it, add it to the `needed` map in
   `TestDeniedSyscallTable` instead.
3. Run `make test`; the end-to-end test shows whether the server still
   works under the filter.

## Releasing

```bash
make check
```

```bash
make build-linux VERSION=v1.2.3
```

The binaries in `dist/` are static; ship them with `deploy/` and
`scripts/`.
