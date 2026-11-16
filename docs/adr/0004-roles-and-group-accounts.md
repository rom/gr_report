# 0004 – Three roles, one shared account per group, one shared leader queue

* Status: Accepted
* Date: 2026-11-16
* ASRs: F-1, F-4, F-5, F-6, Q-1

## Context

The brief names three actors: groups (or persons) that report, an exercise
leader who receives, and an administrator. Reports are supplied by a
"person/group". The leader's default view is one queue per group.

## Decision

* Roles `group`, `leader`, `admin`, exactly one per account.
* A participating group is **one account** with the `group` role, shared by
  its members. Its display name is the sender name on reports. A single
  person taking part alone is simply a group of one.
* All `leader` accounts share one queue, addressed by the pseudo receiver
  `leader`. Report status (`new`/`read`/`filed`) and folder are shared
  between leaders.
* "One queue per group" is a view: the leader queue partitioned by sender,
  with per-group counters, plus an "all" view.
* A group may also address a report to another group; each group therefore
  has an inbox that works like the leader queue.
* The administrator manages accounts and templates but cannot read
  reports. Leaders manage templates and requests but not accounts.

## Alternatives considered

* **Personal accounts with group membership.** Gives per-person
  accountability, but multiplies account administration before an exercise
  and adds a membership model. Deferred.
* **Per-leader read state.** More precise with several leaders, but they
  work as one staff function and a shared state avoids double handling.
* **Queues as stored objects with routing rules.** Unnecessary; the default
  asked for falls out of filtering by sender.
* **Admin can do everything.** Rejected: least privilege; the person
  running the server need not see exercise content.

## Consequences

* Setting up an exercise is creating one account per group.
* No audit trail of which person in a group wrote a report.
* If two leaders want separate work lists they use folders or filters.
* Leaders can read group-to-group reports (oversight) although those do not
  appear in the leader queue.
