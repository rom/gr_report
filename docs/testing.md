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
| `internal/store` | Unit with real database files | CRUD, ordering, persistence across reopen, directory lock, schema version and migration rollback, refusal of non-database files, search including Swedish letters and `LIKE` wildcards, loud failure on closed or damaged data |
| `internal/i18n` | Unit + source scan | Translation, fallback, nested messages, `Accept-Language` matching; **every message used in the code has a Swedish translation, none is unused, format verbs match** |
| `internal/auth` | Unit with fake clock | Argon2id hash format, the reference test vector, parameter bounds, PBKDF2 compatibility and rehash decision, concurrency limit, policy, session idle/absolute expiry, eviction, flash, throttling back-off and bounds |
| `internal/web` | Integration over HTTP (`httptest`) with a real store | Login/logout, cookie attributes, security headers, throttling, CSRF and origin checks, role matrix for every route, object-level access, the full report / queue / request / template / account flows (also in Swedish), language and theme selection, chat (who can read what, unread counters, paging, polling, rate limit, switch-off; oversight: notice, per-message tag, not retroactive, read-only, live polling, off again), escaping of hostile input, body limit, stale sessions, write failure, read failure; **accessibility audit of every page**; **contrast ratios of every theme** |
| `internal/app` | Integration | Configuration file (strict parsing, precedence over defaults and under environment and flags, relative paths, round trip of the rendered configuration, the shipped file), flag and environment parsing, first-run bootstrap, HTTP and TLS serving, TLS minimum version, certificate reload (good and broken), start-up errors, graceful shutdown |
| `internal/sandbox` | Unit + child process | BPF program against an interpreter; **enforcement**: a child applies the sandbox and tries exec, sockets, connect, ptrace, setuid, forbidden reads and writes; strict mode; in-process apply |
| `cmd/gr_report` | Unit + end-to-end | Exit codes; builds the binary, runs it from a YAML configuration file with TLS and the sandbox, logs in, writes, reloads the certificate with SIGHUP, shuts down with SIGTERM |

The role matrix in `TestRoleAccess` is the checklist for authorisation:
every new route belongs in it.

## Fuzz targets

| Target | Input | Property |
| --- | --- | --- |
| `model.FuzzFillReport` | report form values | no panic; accepted reports respect limits, are clean UTF-8, select/checkbox values are legal |
| `model.FuzzTemplateValidate` | template definitions | accepted templates have unique valid keys; validation is idempotent |
| `model.FuzzFilter` | search query, sort key | bounded terms; empty query matches all; sorting never panics |
| `model.FuzzClean` | arbitrary bytes | output is valid UTF-8 without control characters; cleaning is idempotent |
| `store.FuzzReportRoundTrip` | report text, search query | stored text comes back byte for byte; the SQL search agrees with the reference matcher; no input is interpreted as SQL |
| `i18n.FuzzMatch` | `Accept-Language` header | no panic; always a supported language |
| `app.FuzzConfigFile` | configuration file bytes | no panic; an accepted file renders and parses back to the same values |
| `auth.FuzzVerify` | stored hash (Argon2id or PBKDF2), password | malformed hashes never verify; cost parameters beyond the bounds are never accepted |
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

## Accessibility, contrast and translations

These are ordinary Go tests and run in `make test` and `make cover`.

| Test | What it proves |
| --- | --- |
| `web.TestAccessibility` | Every page, for every role, in English and Swedish, with data and with validation errors: language attribute, title, one `h1`, heading order, skip link first, a single `main`, every control labelled, radio groups in a `fieldset` with `legend`, tables named and headers scoped, landmarks named, references to existing IDs, unique IDs, no inline script/style/handlers, no positive `tabindex`, zoom not blocked |
| `web.TestAuditDetects` | The audit reports each of those mistakes on a deliberately bad page |
| `web.TestThemeContrast` | For each theme: text pairs ≥ 4.5:1 (high contrast theme ≥ 7:1), input borders and focus ring ≥ 3:1; every theme defines every variable; no colour outside a theme; `auto` equals dark / contrast under the media queries |
| `i18n.TestCatalogComplete`, `TestCatalogVerbs` | No untranslated or stale message; arguments line up |

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
* The page script (`app.js`) and visual layout in browsers. They were
  exercised by hand: new-report notice, select-all, add-field, theme
  preview, live chat between three browsers, each theme, system dark and increased-contrast detection, the
  skip link, and operation with JavaScript disabled.
* The experience with a screen reader, and a scan with a browser-based
  accessibility checker such as axe.

`make verify-deploy` syntax-checks the unit, the profile and the policy
module; see [security.md](security.md) for how to verify them on a target
host.
