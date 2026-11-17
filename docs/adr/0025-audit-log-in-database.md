# 0025 – The audit log in the database, with a viewer

* Status: Accepted
* Date: 2026-11-17
* ASRs: F-14, Q-3, Q-4

## Context

Audit events were written to the system log only. Reading them needed
shell access and `journalctl`; an administrator of the application could
not see who had done what.

## Decision

* Every audit event is written **both** to the system log (as before) and
  to an `audit` table: time, user, role, client address, event name and a
  short `key=value` detail string.
* Administrators browse it at *Audit log*: newest first, 100 per page,
  with a search over user, event, address and detail.
* Events never contain passwords, report text or chat messages – only who,
  what kind of action, and identifiers or counts. A test checks this.
* The table keeps the newest 100 000 entries. It survives a new exercise.
* Writing the audit row is best effort: if it fails, the action still
  completes and the system log has the event.

## Alternatives considered

* **Parse the system log from the application.** The sandbox cannot read
  it, and it is not there on every platform.
* **Append-only file in the data directory.** No search or paging without
  reading it all.
* **Leaders can read it too.** It lists every login and address; kept to
  the role that manages accounts.
* **Tamper-proof chaining of entries.** Whoever can write the database can
  rewrite it anyway; the system log is the independent copy.

## Consequences

* Failed-login floods consume audit rows; the cap bounds the growth.
* The audit log is included in backups and archives.
* It is evidence for administrators, not protection against one: someone
  with database access can alter it.
