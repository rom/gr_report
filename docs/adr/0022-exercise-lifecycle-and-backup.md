# 0022 – Archive-and-reset and backups as database copies on the server

* Status: Accepted
* Date: 2026-11-17
* ASRs: F-11, F-15, Q-2, Q-5

## Context

Starting a new exercise meant stopping the server and deleting files, and
a backup meant running `sqlite3` by hand. Both were asked for in the user
interface: reset for administrators and leaders, backup as a possibility.
The process is sandboxed: it can write only inside its data directory and
cannot create directories.

## Decision

* **One mechanism for both:** `VACUUM INTO` writes a compact, consistent
  copy of the whole database to a new file while the server keeps working.
  Copies go to two subdirectories of the data directory, created at start:
  `backups/` and `archive/`. Files are mode 0600 and are never overwritten.
* **New exercise** (leader or administrator, after typing `RESET`): first
  an archive copy is written; only if that succeeds are reports, requests,
  chat, drafts and read markers deleted, in one transaction. Accounts,
  templates, saved filters, settings and the audit log stay.
* **Backups** three ways: a button for administrators; automatically every
  `backup.interval`, keeping the newest `backup.keep`; and
  `gr_report -backup FILE` on the command line, which works next to a
  running server and can write anywhere the caller may.
* **Copies are not downloadable in the user interface.** They contain
  everything, including password hashes and the reports and chat that
  administrators are deliberately not shown. Taking them off the machine is
  the job of whoever has shell access.
* Archives are listed by name, time and size only. Looking inside one
  means pointing a separate gr_report at it.

## Alternatives considered

* **Download for administrators.** Convenient, but it would hand the
  administrator role all exercise content and all hashes through the
  browser, undoing the separation in [ADR 0004](0004-roles-and-group-accounts.md).
* **Export to JSON instead of a database copy.** Not restorable without
  writing an importer, and another format to keep in step.
* **Soft reset (an "exercise" column on every table).** Several exercises
  in one database, with a switcher – much more than was asked; the archive
  file gives the same history with no change to queries.
* **Reset without archive.** One click from losing an exercise.
* **Copying the file with `cp`.** Not consistent while the server writes.

## Consequences

* A reset cannot lose data unless the archive file itself is lost.
* Automatic backups protect against mistakes and corruption, **not**
  against losing the disk or the machine: they are in the same directory
  as the database until someone copies them elsewhere.
* Disk use grows by one database size per copy; automatic ones are pruned,
  manual backups and archives are not.
* Restoring is a manual, offline step (documented in operations.md).
