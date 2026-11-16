# Architecture decision records

Each record states the context, the decision, the alternatives that were
considered and the consequences. Records are immutable once accepted; a
changed decision gets a new record that supersedes the old one.

| No. | Decision | Status |
| --- | --- | --- |
| [0001](0001-go-standard-library-only.md) | Go with the standard library only, one static binary | Superseded by 0010 |
| [0002](0002-server-rendered-html-no-javascript.md) | Server-rendered HTML without JavaScript | Superseded by 0011 |
| [0003](0003-journal-storage.md) | In-memory state with an append-only JSON journal | Superseded by 0012 |
| [0004](0004-roles-and-group-accounts.md) | Three roles, one shared account per group, one shared leader queue | Accepted |
| [0005](0005-authentication-and-sessions.md) | Passwords with PBKDF2, server-side in-memory sessions | Accepted; hashing superseded by 0015 |
| [0006](0006-layered-sandbox.md) | Layered sandbox: self-confinement after start-up plus OS policy | Accepted |
| [0007](0007-tls-by-default.md) | TLS required by default, terminated in the process | Accepted |
| [0008](0008-immutable-reports.md) | Reports are immutable and self-contained | Accepted |
| [0009](0009-testing-strategy.md) | Testing strategy: coverage floor, fuzzing, real sandbox tests | Accepted |
| [0010](0010-vetted-dependencies.md) | A small set of vetted Go modules; still one static binary | Accepted |
| [0011](0011-progressive-enhancement.md) | Server-rendered HTML with one optional same-origin script | Accepted |
| [0012](0012-sqlite-storage.md) | SQLite through a pure-Go driver | Accepted |
| [0013](0013-internationalisation.md) | English and Swedish from one message catalogue; language per browser | Accepted |
| [0014](0014-accessibility-and-themes.md) | WCAG 2.1 AA as the bar; themes as checked colour sets | Accepted |
| [0015](0015-argon2id.md) | Argon2id for password hashing, PBKDF2 hashes upgraded at login | Accepted |
| [0016](0016-chat.md) | Chat: one shared conversation plus direct conversations between parties | Accepted; amended by 0017 |
| [0017](0017-chat-oversight.md) | Leader oversight of group chat: off by default, announced, per message, forward-only | Accepted; amended by 0019 |
| [0018](0018-yaml-configuration.md) | One YAML configuration file for all settings; environment and flags override | Accepted; amended by 0019 |
| [0019](0019-oversight-switch.md) | The exercise leader can switch chat oversight in the user interface; the operator can lock it | Accepted |

Template for new records:

```markdown
# NNNN – Title

* Status: Proposed | Accepted | Superseded by NNNN
* Date: YYYY-MM-DD
* ASRs: IDs from ../asr.md

## Context
## Decision
## Alternatives considered
## Consequences
```
