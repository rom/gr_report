# 0020 – Personal accounts as members of a group account

* Status: Accepted (amends [0004](0004-roles-and-group-accounts.md), which
  gave each group exactly one shared account)
* Date: 2026-11-17
* ASRs: F-9, Q-4

## Context

A group shared one login, so nothing recorded which person wrote a report
or a chat message. Personal accounts were asked for, "with group
association". Everything else – queues per group, requests addressed to
groups, chat between groups – is built on the group being the party, and
should stay that way.

## Decision

* The group account stays the identity of the group. A **personal
  account** is an account with the group role and a `group_id` pointing at
  that group account.
* A user acts as a **party** (`User.Party`): the group for the group
  account and for each member, `leader` for exercise leaders, nothing for
  administrators. Reports, inboxes, requests and chat all use the party.
* What a member does is attributed to the person as well: reports store
  `author_id` and `author_name` next to the sending group; chat messages
  show "Person (Group)". Drafts, read markers, saved filters, language,
  theme and time zone belong to the person.
* A report written from the shared login has no author.
* Members are not receivers, recipients, queues or chat partners; only
  groups are.
* Disabling a group account disables the group: its members cannot log in
  either. A group whose shared login should not be used simply never hands
  out that password (or resets it).
* The association is set when the account is created (form or import) and
  not changed afterwards.

## Alternatives considered

* **A separate `groups` table with members only.** Cleaner in theory, but
  every existing reference to a group is the ID of a group account; it
  would mean migrating all of them and removing shared logins, which small
  exercises still want.
* **Membership in several groups.** Not asked for, and "who am I acting as
  right now" would need a switcher on every page.
* **Recording the person only in the audit log.** Invisible to the leader
  reading the report.

## Consequences

* Exercises can mix shared and personal logins, group by group.
* The leader sees who wrote a report; search and export include the
  author.
* Moving a person to another group means a new account.
* A shared login remains anonymous within the group, by design.
