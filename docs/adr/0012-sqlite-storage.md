# 0012 – SQLite through a pure-Go driver

* Status: Accepted (supersedes [0003](0003-journal-storage.md))
* Date: 2026-11-16
* ASRs: Q-2, Q-4, Q-5, C-5

## Context

[ADR 0003](0003-journal-storage.md) kept all state in memory with a
hand-written append-only journal, because third-party modules were not
allowed. SQLite is now requested. It removes the memory ceiling, gives
transactions, indexes and a file format with mature tooling.

## Decision

* Store everything in one SQLite database, `gr_report.db`, in the data
  directory, using `modernc.org/sqlite` (no cgo,
  [ADR 0010](0010-vetted-dependencies.md)) through `database/sql`.
* Settings: `journal_mode=WAL`, `synchronous=FULL` (a commit is on disk
  before the request is acknowledged), `foreign_keys=ON`,
  `temp_store=MEMORY` (no files outside the data directory, which Landlock
  would refuse), `trusted_schema=OFF`, `STRICT` tables.
* One connection (`SetMaxOpenConns(1)`): SQLite has a single writer anyway
  and this rules out lock contention between the server's own goroutines.
* Schema changes are numbered migrations applied in a transaction at start;
  the version lives in `PRAGMA user_version`. A database with a newer
  version than the program knows is refused, not downgraded. `PRAGMA
  quick_check` runs at start.
* The exclusive `flock` on the data directory is kept, so two server
  processes can never share a database (sessions are in memory).
* Nested values (template fields, request recipients, report field values)
  are JSON text columns; everything that is filtered on is a real column
  with an index.
* **Search**: each report row carries `search_text`, the lower-cased
  concatenation of its searchable parts. Lower-casing is done in Go because
  SQLite folds ASCII only and Å, Ä, Ö must match. A search term becomes a
  bound `LIKE ? ESCAPE '\'` parameter with `%`, `_` and `\` escaped.
* All values reach SQL as bound parameters. No SQL text is ever built from
  user input.
* Sorting and paging of a queue view are done in Go on the filtered rows,
  as before, to keep one definition of the sort order.
* Read methods of `store` have no error result: a read can only fail when
  the database is closed, damaged or the disk is gone. They panic with
  `*store.Failure`; the HTTP layer recovers and answers 500. Writes return
  errors.

There is no migration from the journal format: it was never released.

## Alternatives considered

* **cgo SQLite (`mattn/go-sqlite3`).** Rejected in ADR 0010.
* **Full-text search with FTS5.** Finds words, not substrings, and its
  tokenizer needs configuration for Swedish; the simple substring search
  matches the previous behaviour exactly and is fast enough at this scale.
* **A connection pool with concurrent readers.** WAL allows it, but it
  brings `SQLITE_BUSY` handling for no measurable gain here.
* **Returning errors from every read.** Would add an untestable error
  branch to some forty call sites in the handlers for a condition that
  means the server is broken anyway.
* **A client/server database.** Still too heavy to operate for an exercise.

## Consequences

* Memory use no longer grows with the number of reports; start-up no
  longer replays history.
* The data directory holds `gr_report.db`, and while running
  `gr_report.db-wal` and `gr_report.db-shm`. A consistent online backup
  needs the SQLite backup command (see [operations.md](../operations.md)),
  not a plain copy of one file.
* The database can be inspected with the `sqlite3` command line tool.
* A fuzz target cross-checks the SQL search against the reference matcher
  in `model`; it found that a NUL byte in a search term truncated the
  `LIKE` pattern to "match everything", which is now prevented at the
  source (control characters are removed from search terms).
* A queue view still loads all matching rows before sorting. Move sorting
  and `LIMIT` into SQL if a single queue is expected to exceed some tens of
  thousands of reports.
* The SELinux policy needs the `map` permission on data files because
  SQLite memory-maps the `-shm` file.
