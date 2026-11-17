# 0019 – A switch for chat oversight in the user interface

* Status: Accepted (amends [0017](0017-chat-oversight.md), which made
  oversight a server setting only, and [0018](0018-yaml-configuration.md),
  which put every setting in the configuration file)
* Date: 2026-11-16
* ASRs: F-8, Q-3, Q-4

## Context

[ADR 0017](0017-chat-oversight.md) made oversight a setting that needs a
restart, and listed a user interface toggle as a rejected alternative: it
"invites switching oversight on in the middle of an exercise". A toggle
has now been asked for. The protections of 0017 that matter to
participants – they are told, per conversation and per message; nothing
written earlier is exposed; the leader cannot write; looks are logged – do
not depend on how the setting is changed. One thing does: a participant
with a conversation already open when the switch is flipped.

## Decision

* **The exercise leader has a switch** on the chat page: *Switch oversight
  on / off*, with the current state, an explanation of what it means, and
  who last changed it and when. Groups and administrators have no switch.
* **The operator decides whether the switch exists**: `chat.oversight_toggle`
  (default `true`). With `false`, `chat.oversight` in the configuration
  file is final, the page says so, and a choice made earlier with the
  switch is ignored.
* **Precedence when the switch exists**: a choice made with the switch,
  stored in the database (`settings` table, migration 4, with user and
  time), wins over `chat.oversight`, which is then only the state until the
  switch is first used. The choice survives restarts. The state and its
  source are logged at start.
* **Nobody is caught by a flip.** A group-to-group chat page always
  contains the oversight notice, hidden while oversight is off:
  * the page's polling learns the current state every 4 seconds and shows
    or hides the notice (a live region) and the matching "who can read
    this" line;
  * the message form carries which state the writer was shown. If
    oversight is on when a message arrives but the form says the writer saw
    "off", the message is **not sent**: the page comes back with the
    notice, an explanation and the draft, and the writer decides again.
    This covers browsers without JavaScript and the seconds between a flip
    and the next poll.
  The opposite case – written under "on", arriving after "off" – is sent
  as a private message without asking.
* **Changes are audited**: `event=chat_oversight_set` with the user and
  the new state.
* Unchanged from 0017: forward-only (only messages sent while oversight is
  on are ever shown to the leader), read-only, off closes the view, each
  look is logged.

## Alternatives considered

* **Administrator switches, leader reads** (separation of duties). The
  administrator is the person running the server, who can already set the
  configuration file; the exercise leader is the one who knows when
  oversight is needed. The operator's control is kept through
  `chat.oversight_toggle` instead.
* **Switch enabled only when the operator opts in** (default `false`).
  Safer by default, but the feature was asked for, and every use of it is
  visible to the participants, not retroactive and logged. Operators who
  want the old behaviour set one line.
* **Announce the flip with a message in every conversation.** Leaves
  permanent noise in each chat; the notice, the per-message mark and the
  held-back message already ensure nobody writes unaware.
* **Reload open pages on a flip.** Would discard what a participant is
  typing.
* **Store the switch in the YAML file.** The server cannot and should not
  write its own configuration file (it is read-only to the service and
  outside its sandbox).

## Consequences

* The configuration file no longer tells the whole story for this one
  setting: with the switch enabled, the effective state is in the
  database. The leader's chat page and the start-up log show it;
  `-print-config` shows only the configured initial state.
* A leader can switch oversight on at any moment during an exercise.
  Participants see it within seconds in any open group conversation and
  before sending in every case; earlier messages stay private.
* A message can come back unsent once, right after a flip.
* A group-to-group chat page is slightly larger, because it always carries
  the hidden notice.
* With several leader accounts, any leader can flip the switch for all.
