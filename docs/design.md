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
* **Filter and sort.** `ReportFilter` describes a selection: receiver,
  sender, status, template, request, folder, time range and up to 10 search
  terms (AND, case-insensitive substring over subject, content, sender,
  template, folder and field values; control characters are removed from
  terms). The store translates it to SQL; `Match` is the reference
  implementation the SQL is fuzz-tested against. `SortReports` is stable
  and total: ties are broken by timestamp and ID.
* **Chat.** `ChatParty(user)` maps a user to its party (group ID, or
  `leader`); `ChatConv(party, peer)` gives the conversation key, identical
  from both sides; `CleanChat` validates a message (1–2 000 characters).
* **Errors** are `*i18n.Msg` values (message key and arguments) so the
  HTTP layer can show them in the user's language.

## internal/store

One SQLite database, `gr_report.db`, opened through `database/sql` with the
pure-Go driver ([ADR 0012](adr/0012-sqlite-storage.md)).

### Schema

| Table | Columns of note |
| --- | --- |
| `users` | `username` unique; `role`, `disabled`, `must_change` |
| `templates` | `fields` (JSON array of field definitions), `archived` |
| `requests` | `template_id`, `recipients` (JSON array, empty = all groups), `closed` |
| `reports` | `ts` (Unix nanoseconds), sender and receiver IDs and names, `fields` (JSON array of key/label/value), `request_id`, `status`, `folder`, `search_text` |
| `chat_messages` (migration 2; `overseen` added by migration 3) | `seq` (autoincrement, the cursor), `conv`, `party_a`, `party_b`, sender party and name, `body`, `overseen` |
| `chat_read` (migration 2) | primary key (`party`, `conv`), `seq` last seen |

Indexes on `reports(receiver_id, sender_id, status)`, `reports(sender_id)`
and `reports(request_id)`. Tables are `STRICT`. There are no deletes:
accounts are disabled and templates archived.

### Connection settings

`journal_mode=WAL`, `synchronous=FULL`, `foreign_keys=ON`,
`busy_timeout=5000`, `temp_store=MEMORY`, `trusted_schema=OFF`, one
connection.

### Open

1. Create the directory (0700), take an exclusive `flock` on `lock`.
2. Create the database file with mode 0600 if it does not exist, open it.
3. Apply pending migrations, each in its own transaction, and store the
   new `user_version`. A higher version than the program knows aborts.
4. `PRAGMA quick_check`.

### Reading and writing

* `Put…` methods are single `INSERT … ON CONFLICT`/`INSERT OR REPLACE`
  statements; `UpdateReport` reads, modifies and writes inside one
  transaction. They return errors.
* `Reports(filter)` builds a `WHERE` clause from fixed fragments and bound
  parameters. Each search term adds `search_text LIKE ? ESCAPE '\'`.
  `search_text` is maintained on every write of a report (it includes the
  folder, which can change).
* `Stats` and `NewCount` are aggregate queries.
* `ChatMessages(conv, after, before, limit)` pages by sequence number;
  `ChatUnread(party)` counts, per conversation of the party, messages from
  others beyond the party's read marker; `MarkChatRead` only ever moves the
  marker forward.
* Read methods panic with `*store.Failure` when the database fails; the
  HTTP layer turns that into a 500 page.

## internal/auth

* **Passwords.** Argon2id, stored as
  `$argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>` (16-byte salt, 32-byte
  key). Parameters are read back from the hash, within fixed bounds, so
  the cost can be raised later. At most `MaxConcurrent` (4) derivations
  run at once; the rest queue. Hashes in the earlier
  `pbkdf2-sha256$<iterations>$<salt>$<key>` format still verify;
  `Hasher.NeedsRehash` tells the login handler to replace a hash that uses
  the old scheme or weaker parameters. Verification of an unknown user
  runs against a dummy hash to keep timing uniform.
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
headers → body limit → panic recovery → router → language + theme →
[session → form parse + origin → CSRF → must-change-password → role] →
handler → html/template → response
```

* `headers` sets CSP, `nosniff`, frame, referrer, COOP/CORP, permissions
  policy, `Cache-Control: no-store` and (with TLS) HSTS on every response.
* `open(handler)` serves pages that work with or without a session (login,
  settings).
* `authed(handler, roles…)` is the only way a handler receives a user.
  Unauthenticated requests are redirected to `/login`; a wrong role gives
  403; unknown or foreign objects give 404 (no existence leak).

### Routes

| Method and path | Roles | Purpose |
| --- | --- | --- |
| `GET /login`, `POST /login`, `POST /logout` | – / any | Session |
| `GET,POST /settings` | – / any | Language and theme (stored in cookies) |
| `GET,POST /password` | any | Change own password |
| `GET /home` | group | Dashboard: template picker, open requests, recent reports |
| `GET /reports/new`, `POST /reports` | group | Write a report |
| `GET /reports/{id}` | group, leader | Read (marks read for the receiver) |
| `POST /reports/{id}/status` | group, leader | Mark new/read, file in folder |
| `GET /queue`, `POST /queue/bulk` | group, leader | Queue / inbox with filter, sort, search, bulk actions |
| `GET /sent` | group | Sent reports |
| `GET /api/new-count` | group, leader | JSON `{"new": n, "chat": m}` for the page script |
| `GET /chat`, `GET /chat/{peer}` | group, leader | Conversation list and messages; `peer` is `all`, `leader` or a group ID |
| `POST /chat/{peer}` | group, leader | Send a message |
| `GET /api/chat/{peer}?after=seq` | group, leader | New messages as JSON; marks them read |
| `GET /chat/oversight/{a}/{b}` | leader | With oversight on: read-only view of the overseen messages between groups `a` and `b` |
| `GET /api/chat/oversight/{a}/{b}?after=seq` | leader | New overseen messages as JSON for that view; marks nothing read |
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
* A chat conversation is resolved from the user's own party and the peer
  in the path; peers that are the user itself, not a group, or unknown give
  404. Viewing the newest page or polling marks the conversation read;
  viewing an older page does not.
* A message between two groups gets `overseen = 1` exactly when
  `Server.ChatOversight` is on at the moment it is sent. The oversight
  page and its polling endpoint share one check (`oversightPair`), query
  with `overseen = 1` only, exist only while the setting is on, have no
  `POST`, never mark anything read, and write an audit entry – the endpoint
  only when it delivers messages.
* The `return` parameter of a bulk action is re-parsed into known queue
  parameters and can only lead back to `/queue`.
* The template editor is plain HTML: existing fields plus three empty
  rows; clearing a label removes the field.

### User interface

Embedded templates (`templates/*.html`, one layout), one stylesheet and one
script; no external resources.

* **Translation.** Templates call `{{$.T "English text" args…}}`; values
  stored in English (role, status, field type) go through `{{$.Tr value}}`.
  Handlers use `c.t(…)` and `i18n.TErr(c.lang, err)`.
* **Themes.** `<html data-theme="name">`; `static/app.css` starts with one
  block of custom properties per theme and uses only `var(--…)` colours
  below. `auto` maps to light, dark or high contrast through media queries.
* **Accessibility.** Skip link, landmarks with names, one `h1`, labelled
  controls, `fieldset`/`legend`, table headers with `scope`, `aria-sort`,
  `aria-current`, errors as `role="alert"` linked to the form by
  `aria-describedby`, visible focus, no information by colour alone.
* **Script** (`static/app.js`, optional): new-report notice and unread
  chat counter by polling, live chat messages, select-all, add-field row,
  theme preview. Elements that need it are
  rendered `hidden` with `data-js-show`. It never writes HTML.
* Without the script, the optional `<meta http-equiv="refresh">` on the
  queue is the way to watch for new reports.

## internal/i18n

* `T(lang, key, args…)` – look up `key` (the English text) in the catalogue
  of `lang`, fall back to the key, then `fmt.Sprintf`. `Label` and `*Msg`
  arguments are translated first.
* `Errorf(key, args…)` returns a `*Msg` that is an `error`; `TErr`
  translates any error that wraps one.
* `Match(acceptLanguage, default)` picks the supported language with the
  highest quality value.
* `sv.go` is the Swedish catalogue. Adding a language: add the constant to
  `Languages` and a map to `catalogs`.

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
  TLS unless `-insecure-http`, `-dev-seed` without `-insecure-http`, and an
  unsupported `-lang`.
* `Bootstrap` – starter templates (in the default language) when there are
  none; when there are no
  users, the `admin` account with a random one-time password written to
  `initial-admin-password` (0600). The account is only created if the
  file could be written.
* `Run` – the start-up sequence from the architecture document, an
  `http.Server` with time-outs (header 10 s, read 30 s, write 60 s, idle
  2 min), certificate reload on the `Reload` channel and graceful shutdown
  (10 s) when the context is cancelled.
