# 0008 – Reports are immutable and self-contained

* Status: Accepted
* Date: 2026-11-16
* ASRs: F-2, F-3, Q-4

## Context

A report is a record of what a group told the leader at a point in time.
Templates and account names can change during an exercise. The minimal
report content is fixed by the brief.

## Decision

* Every report has the five mandatory parts – timestamp, sender, subject,
  content, receiver – regardless of template. A template can only *add*
  fields and guidance. The "Minimal report" starter template adds nothing.
* Timestamp and sender are set by the server from its clock and the
  session; the receiver defaults to the exercise leader.
* A report cannot be edited or deleted. Corrections are new reports.
* A report stores copies of the sender's display name, the template name
  and each field's label next to the value.
* The only mutable attributes are `status` and `folder`, which describe
  the receiver's handling.

## Alternatives considered

* **Editable reports with history.** More complex and blurs "what was
  reported when".
* **Rendering reports through the current template.** Changing or
  archiving a template would alter or break old reports.
* **Templates that redefine the whole report.** Would allow templates
  without subject or content and break the minimal-report guarantee, the
  queue columns and search.

## Consequences

* Old reports stay readable and unchanged whatever happens to templates
  and accounts.
* Search, sort and the queue can rely on subject, sender and time always
  being present.
* Some duplication of names in the journal.
* A mistaken report cannot be withdrawn; the leader can file it away.
