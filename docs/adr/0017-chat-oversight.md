# 0017 – Leader oversight of group chat as a visible, forward-only setting

* Status: Accepted (amends [0016](0016-chat.md), which made group-to-group
  chat private without exception)
* Date: 2026-11-16
* ASRs: F-7, Q-3, Q-4

## Context

[ADR 0016](0016-chat.md) kept direct conversations between two groups
private and noted that oversight, if wanted, should be "an explicit,
visible setting". Exercise control often does need to follow what groups
tell each other. The risk is the opposite of the need: participants who
believe a conversation is private while it is being read.

## Decision

* **A server setting, off by default:** `-chat-oversight` /
  `GR_REPORT_CHAT_OVERSIGHT=on`. It is chosen by whoever runs the server,
  not toggled in the user interface, and its state is logged at start.
* **Visible before writing.** While it is on, every group-to-group
  conversation shows a prominent notice above the messages, tied to the
  message field for screen readers: the exercise leader can read what is
  sent here. The line that says who can read the conversation changes
  accordingly.
* **Visible per message.** A message between two groups sent while
  oversight is on is stored with an `overseen` flag and shows the tag
  "visible to the exercise leader" to both groups, also on messages that
  arrive live.
* **Forward-only.** The flag is fixed when the message is sent. The leader
  is only ever shown flagged messages. Switching oversight on therefore
  **does not expose anything written before**, and what a participant was
  told when sending stays true afterwards. The migration that adds the
  column marks all existing messages as not overseen.
* **Read-only and separate.** Leaders reach these conversations through
  their own list, *Oversight of group conversations*, and a separate route
  (`/chat/oversight/{a}/{b}`). There is no form and no write path; the
  leader cannot post into a conversation between two groups. Looking does
  not change the groups' unread counters.
* **Switching it off** closes the leader's view immediately, including of
  messages sent while it was on. The tags stay on those messages for the
  groups, because they were readable at the time.
* **Accountability.** Each time a leader opens an overseen conversation,
  an audit log entry records who, which conversation and how many messages
  – not their text.
* Conversations the leader is a party to anyway (*Everyone*, group ↔
  leader) are unaffected and carry no notice. Administrators still have no
  chat.

## Alternatives considered

* **A toggle in the leader's or administrator's user interface.** More
  convenient, but it invites switching oversight on in the middle of an
  exercise; a setting that requires a restart is a deliberate act by the
  operator. The forward-only rule would protect history either way.
* **Oversight of the whole history once switched on.** Simpler (no
  per-message flag) but it would retroactively break the promise shown to
  participants when they wrote.
* **A banner only, without per-message tags.** A participant scrolling
  back could not tell which messages were readable.
* **Keeping overseen messages readable by the leader after switching
  off.** Defensible, since they were sent knowingly; rejected so that "off"
  has one simple meaning: the leader reads no group-to-group chat.
* **Letting leaders post into the conversation.** That is what the
  group ↔ leader conversation is for.

## Consequences

* Groups can always tell, before and after sending, whether the exercise
  leader can read a message.
* An exercise that wants oversight from the start must switch it on before
  participants begin to chat; earlier messages stay private for good.
* Leaders see a conversation that may have gaps (messages sent while
  oversight was off); the page says so.
* None of this limits someone with access to the database file, who can
  read every message regardless of the flag ([security.md](../security.md)).
* The oversight view is not updated live; the leader reloads it.
