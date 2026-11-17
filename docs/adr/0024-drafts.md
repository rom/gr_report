# 0024 – Drafts kept on the server

* Status: Accepted
* Date: 2026-11-17
* ASRs: F-13, Q-5

## Context

A half-written report was lost when the session expired (2 hours idle),
the tab closed or the device ran out of power.

## Decision

* A draft is stored **on the server**, per user, one per template and per
  request (`drafts` table). It holds exactly what is in the form, without
  validation as a report – only the size limits apply.
* **Save as draft** on the form works without JavaScript. With JavaScript
  the form is also saved in the background 15 seconds after a change.
* Opening the form for the same template or request restores the draft
  and says so. Sending the report deletes it. Drafts are listed on the
  home page and can be deleted there.
* At most 20 drafts per user. Drafts are removed by a new exercise.
* A draft is never visible to anyone but its author – not to the group,
  not to the leader.

## Alternatives considered

* **Browser storage (`localStorage`).** No server state, but lost on
  another device and readable by the next person at a shared computer.
* **Warn before the session expires.** Helps only the user who is looking;
  does nothing for a closed tab.
* **Several drafts per template.** Then "continue" needs a chooser; one per
  template and request covers the cases seen in practice.

## Consequences

* Unsent text exists on the server; it is covered by backups and archives
  like everything else.
* Background saving sends the form every 15 seconds while it changes.
* If the session has expired, background saving fails and the form says
  so; the last successful save is still there after logging in again.
