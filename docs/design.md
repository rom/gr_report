# Design

Component-level design. For the overall structure see
[architecture.md](architecture.md); for the reasons see the
[ADRs](adr/README.md).

## internal/model

Pure data and rules, no I/O.

* **Normalisation.** `CleanLine` (single-line fields: invalid UTF-8
  replaced, control characters to spaces, trimmed) and `CleanText`
  (multi-line: CRLF to LF, control characters other than newline and tab
  removed). Both are idempotent, which is a fuzzed property.
* **Limits.** Subject 200, content 20 000, field value 4 000 characters;
  40 fields per template; 50 options per select. Lengths are counted in
  characters, not bytes.
* **Templates.** `Template.Validate` derives a stable key from each field
  label (`KeyFromLabel`), makes keys unique with numeric suffixes and
  keeps an existing key when a field is relabelled, so the meaning of
  stored values does not shift.
* **Reports.** `FillReport(report, template, subject, content, get)`
  validates each field by type: `number` (digits with optional sign and
  decimals), `datetime` (`2006-01-02T15:04`), `select` (must be one of the
  options), `checkbox` (stored as `yes`/`no`). It stores label and value
  for every template field, in template order.
* **Filter and sort.** `ReportFilter` matches on receiver, sender, status,
  template, request, folder, time range and up to 10 search terms (AND,
  case-insensitive substring over subject, content, sender, template,
  folder and field values). `SortReports` is stable and total: ties are
  broken by timestamp and ID.

## internal/store

State is four maps guarded by one `sync.RWMutex`, plus a username index.

### Journal format

`journal.jsonl`, one JSON object per line:

```json
{"k":"report","v":{"id":"…","timestamp":"…","sender_id":"…", "…":"…"}}
```

`k` is `user`, `template`, `request` or `report`; `v` is the complete
entity. A later record for the same ID replaces the earlier one. There are
no deletes: accounts are disabled and templates archived.

### Write path

1. Encode the record (maximum 1 MiB).
2. `write` the line to the journal opened with `O_APPEND`.
3. `fsync`.
4. Only then update the maps.

If step 2 or 3 fails the maps are untouched and the caller gets the error.

### Open

1. Create the directory (0700), take an exclusive `flock` on `lock`.
2. Replay the journal. An unparsable **final** record is treated as a torn
   write from a crash and dropped; an unparsable record anywhere else
   aborts the start, because silently skipping it could lose data.
3. Compact: write one record per live entity to a temporary file, `fsync`,
   `rename` over the journal, `fsync` the directory.
4. Open the journal for appending.

Readers get deep copies (`Clone`), so no handler can modify stored state
except through `Put…`/`UpdateReport`.

## internal/auth

* **Passwords.** `pbkdf2-sha256$<iterations>$<salt>$<key>`, 16-byte salt,
  32-byte key, 600 000 iterations by default, parameters read back from
  the hash so the work factor can be raised later. Verification of an
  unknown user runs against a dummy hash to keep timing uniform.
  Minimum length 10 characters; generated one-time passwords are 16
  characters from a 55-symbol alphabet without look-alikes (≈ 92 bits).
* **Sessions.** Map from SHA-256(token) to `{user, csrf, flash, created,
  last seen}`. Idle time-out 2 h, lifetime 12 h, at most 10 000 sessions
  (expired ones are purged first, then the least recently used).
* **Throttling.** Two limiters: per `username|address` (5 failures) and
  per address (50 failures, forgotten after 10 quiet minutes, because many
  participants may share one address). After the burst the key is locked
  for 30 s, doubling with each further failure up to 15 min. Tables are
  bounded.

## internal/web

### Request pipeline

```
headers → body limit → router → [session → form parse + origin → CSRF →
must-change-password → role] → handler → html/template → response
```

* `headers` sets CSP, `nosniff`, frame, referrer, COOP/CORP, permissions
  policy, `Cache-Control: no-store` and (with TLS) HSTS on every response.
* `authed(handler, roles…)` is the only way a handler receives a user.
  Unauthenticated requests are redirected to `/login`; a wrong role gives
  403; unknown or foreign objects give 404 (no existence leak).

### Routes

| Method and path | Roles | Purpose |
| --- | --- | --- |
| `GET /login`, `POST /login`, `POST /logout` | – / any | Session |
| `GET,POST /password` | any | Change own password |
| `GET /home` | group | Dashboard: template picker, open requests, recent reports |
| `GET /reports/new`, `POST /reports` | group | Write a report |
| `GET /reports/{id}` | group, leader | Read (marks read for the receiver) |
| `POST /reports/{id}/status` | group, leader | Mark new/read, file in folder |
| `GET /queue`, `POST /queue/bulk` | group, leader | Queue / inbox with filter, sort, search, bulk actions |
| `GET /sent` | group | Sent reports |
| `GET /requests`, `GET /requests/{id}` | group, leader | List and read requests |
| `GET /requests/new`, `POST /requests`, `POST /requests/{id}/close` | leader | Send, close, reopen |
| `GET /templates`, `GET /templates/new`, `GET,POST /templates/{id}`, `POST /templates`, `POST /templates/{id}/archive` | leader, admin | Templates |
| `GET,POST /admin/users`, `POST /admin/users/{id}` | admin | Accounts |
| `GET /static/…`, `GET /healthz` | – | Assets, liveness |

### Rules worth knowing

* A report's sender, sender name and timestamp come from the session and
  the server clock.
* When a report answers a request, the template and the receiver
  (exercise leader) are taken from the request, not from the form.
* A group can address another enabled group, never itself or a
  non-group account.
* Queue actions only touch reports whose receiver is the acting user's
  queue; IDs of other reports in a bulk request are silently skipped.
* The `return` parameter of a bulk action is re-parsed into known queue
  parameters and can only lead back to `/queue`.
* The template editor is plain HTML: existing fields plus three empty
  rows; clearing a label removes the field.

### User interface

Embedded templates (`templates/*.html`, one layout) and one stylesheet.
No JavaScript, no external resources, light and dark colour scheme,
usable on a phone. The only "live" behaviour is the optional
`<meta http-equiv="refresh">` on the queue.

## internal/sandbox

`Apply(Config{Mode, ReadWrite, ReadOnly})` returns a `Status` describing
what is active. Linux implementation, in order:

1. `setrlimit(RLIMIT_CORE, 0)`, `prctl(PR_SET_DUMPABLE, 0)`.
2. `prctl(PR_SET_NO_NEW_PRIVS)` on all threads.
3. Drop the bounding set, ambient set and the three capability sets
   (skipped when they are already empty, e.g. under systemd).
4. Landlock: probe the ABI version, handle every file-system right known
   to that ABI (and TCP bind/connect from ABI 4, abstract UNIX socket scope
   from ABI 6), add `path_beneath` rules for the configured paths, restrict
   all threads.
5. seccomp: build the classic-BPF program (`buildFilter`) and install it
   with `SECCOMP_FILTER_FLAG_TSYNC`.

Per-thread system calls use `syscall.AllThreadsSyscall`, which needs a
non-cgo build; in a cgo build (the race detector) Landlock and the
capability drop are skipped and reported, seccomp still applies.

`buildFilter` is platform independent and has a matching interpreter
(`evalFilter`) used by the unit and fuzz tests.

## internal/app

* `ParseConfig` – flags with environment defaults; rejects running without
  TLS unless `-insecure-http`, and `-dev-seed` without `-insecure-http`.
* `Bootstrap` – starter templates when there are none; when there are no
  users, the `admin` account with a random one-time password written to
  `initial-admin-password` (0600). The account is only created if the
  file could be written.
* `Run` – the start-up sequence from the architecture document, an
  `http.Server` with time-outs (header 10 s, read 30 s, write 60 s, idle
  2 min), certificate reload on the `Reload` channel and graceful shutdown
  (10 s) when the context is cancelled.
