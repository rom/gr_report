# 0003 – In-memory state with an append-only JSON journal

* Status: Superseded by [0012](0012-sqlite-storage.md)
* Date: 2026-11-16
* ASRs: Q-2, Q-4, Q-5, Q-10

## Context

Data volume is small (one exercise: at most thousands of reports), but an
acknowledged report must not be lost, and operation must be trivial. A
database server contradicts Q-2; SQLite needs cgo or a large pure-Go
dependency, contradicting [ADR 0001](0001-go-standard-library-only.md).

## Decision

Keep all entities in memory. Persist every change as one JSON line appended
to `journal.jsonl` and `fsync` it before the change becomes visible and the
request is acknowledged. On start, replay the journal and rewrite it
compacted (atomic rename). An exclusive `flock` guards the directory
against a second process. Entities are never deleted.

## Alternatives considered

* **SQLite.** Mature, queryable. Rejected for the dependency (cgo
  `mattn/go-sqlite3` or the large `modernc.org/sqlite`).
* **bbolt or another embedded key-value store.** A dependency for little
  gain at this size.
* **One JSON file rewritten on every change.** Simpler, but O(total size)
  per write and a larger window for loss.
* **PostgreSQL.** Operationally too heavy for the use case.

## Consequences

* Backup is copying one human-readable file; it can be inspected with
  `jq`.
* The journal doubles as an audit trail of changes between compactions.
* Writes are serialised and cost one `fsync` each – ample for human-typed
  reports.
* Queries scan memory; filtering 50 000 reports takes milliseconds.
* Memory grows with the data and start-up time with the journal size.
  **Revisit** when a deployment is expected to exceed roughly 100 000
  reports or several hundred megabytes; the `store` API is narrow enough to
  re-implement on a database.
* A corrupt record that is not the last one stops the server from
  starting. This is deliberate: an operator must look at it rather than
  have data silently dropped.
* No multi-process or multi-host operation.
