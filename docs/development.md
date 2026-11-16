# Development

## Prerequisites

* Go 1.24 or newer. The first build downloads the modules listed in
  `go.mod`; nothing else is needed to build and test. No Node, no bundler:
  the stylesheet and the script are plain files.
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
internal/web/static/      stylesheet (themes), script, icon
internal/i18n/        message catalogue (sv.go), language negotiation
internal/auth/        passwords, sessions, throttling
internal/store/       SQLite storage and migrations
internal/model/       entities, validation, filter, sort
internal/sandbox/     Landlock, seccomp, capabilities (Linux), no-op elsewhere
scripts/              certificates, coverage gate, fuzz runner
deploy/systemd/       unit, drop-in, env file, sysusers, tmpfiles
deploy/selinux/       policy module
deploy/apparmor/      profile
docs/                 documentation, ADRs
```

## Conventions

* Dependencies are few and justified, and never need cgo
  ([ADR 0010](adr/0010-vetted-dependencies.md)); amend that record when
  adding one.
* JavaScript is progressive enhancement in `static/app.js` only: no inline
  script, style or event handler (the CSP forbids them), no HTML insertion,
  and every feature must work without it
  ([ADR 0011](adr/0011-progressive-enhancement.md)).
* Every user-visible text goes through the catalogue: `{{$.T "…"}}` in
  templates, `c.t("…")` in handlers, `i18n.Errorf` for validation errors.
  Add the Swedish translation to `internal/i18n/sv.go`; the tests fail
  until you do.
* No colour outside the theme blocks of `app.css`.
* Never edit a shipped entry of `store.migrations`; append a new one.
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

## Adding a text

1. Use the English text as the key where it is shown.
2. Run `go test ./internal/i18n`; `TestCatalogComplete` names the missing
   key.
3. Add the Swedish translation to `sv.go` with the same format verbs.

## Adding a theme

1. Add a `[data-theme="name"] { … }` block to `internal/web/static/app.css`
   defining every colour variable the other themes define.
2. Add `{"name", "Label"}` to `web.Themes` and translate the label.
3. Run `go test ./internal/web -run TestThemeContrast`; it reports each
   colour pair that misses the required contrast ratio.

## Changing the database schema

1. Append one SQL string to `store.migrations`.
2. Update the column lists and scan functions in `store.go`.
3. `TestSchemaVersion` checks that the version is recorded and that a
   failing migration rolls back.

## Changing a page

`go test ./internal/web -run TestAccessibility` renders every page for
every role in both languages and audits the markup. Add new pages to the
list in that test.

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
