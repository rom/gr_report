# Architecture decision records

Each record states the context, the decision, the alternatives that were
considered and the consequences. Records are immutable once accepted; a
changed decision gets a new record that supersedes the old one.

| No. | Decision | Status |
| --- | --- | --- |
| [0001](0001-go-standard-library-only.md) | Go with the standard library only, one static binary | Accepted |
| [0002](0002-server-rendered-html-no-javascript.md) | Server-rendered HTML without JavaScript | Accepted |
| [0003](0003-journal-storage.md) | In-memory state with an append-only JSON journal | Accepted |
| [0004](0004-roles-and-group-accounts.md) | Three roles, one shared account per group, one shared leader queue | Accepted |
| [0005](0005-authentication-and-sessions.md) | Passwords with PBKDF2, server-side in-memory sessions | Accepted |
| [0006](0006-layered-sandbox.md) | Layered sandbox: self-confinement after start-up plus OS policy | Accepted |
| [0007](0007-tls-by-default.md) | TLS required by default, terminated in the process | Accepted |
| [0008](0008-immutable-reports.md) | Reports are immutable and self-contained | Accepted |
| [0009](0009-testing-strategy.md) | Testing strategy: coverage floor, fuzzing, real sandbox tests | Accepted |

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
