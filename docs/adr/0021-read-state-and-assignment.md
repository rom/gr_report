# 0021 – Read state per person, status and assignment shared

* Status: Accepted (amends [0004](0004-roles-and-group-accounts.md), which
  made read state shared between leaders)
* Date: 2026-11-17
* ASRs: F-12

## Context

All leaders shared one status per report: once one leader opened a report
it stopped looking new to the others. With several leaders, and now with
personal accounts in groups, each person needs to see what *they* have not
read, and leaders need to divide the work.

## Decision

Three separate things are tracked:

| | Scope | Values | Changed by |
| --- | --- | --- | --- |
| **Seen** | per user and report (`report_seen`) | seen / not seen | opening the report; "mark as read"; "mark as new" clears it for that user |
| **Status** | per report, shared | new, read, filed | "new" until anybody in the receiving party opens it; filing |
| **Assignee** | per report, shared (leader queue only) | a leader, or nobody | "assign to me", the assign control, "remove assignment" |

* The bold row, the dot, the "Only unread" filter and the counters in the
  menu and the queue list are about **seen by me**.
* The status tag stays about the team: "new" means nobody has opened it.
* Assignment is advisory: it filters and labels, it does not restrict who
  may open or file a report.
* The upgrade marks every report that was not "new" as seen by every
  existing account, so nobody's history turns unread.

## Alternatives considered

* **Per-person status including "filed".** Filing is a team decision about
  the report, not a personal view.
* **Dropping the shared status.** The team-level "has anyone looked at
  this" is exactly what a staff wants at a glance.
* **Assignment as access control.** Would block a colleague from covering.

## Consequences

* An account created later sees earlier reports as unread.
* One more table that grows with users × reports opened; rows are two
  short strings.
* Counters are now computed per viewer on every page.
