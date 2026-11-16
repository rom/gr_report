# Architecturally significant requirements (ASR)

Requirements that shaped the architecture. *Source* refers to the project
brief unless stated otherwise. Each ASR names the decisions that address it
and how it is verified.

## Functional drivers

| ID | Requirement | Addressed by | Verified by |
| --- | --- | --- | --- |
| F-1 | Groups participating in an exercise send reports to an exercise leader | Roles `group` and `leader`; `ReceiverLeader` default receiver ([ADR 0004](adr/0004-roles-and-group-accounts.md)) | `TestReportFlow` |
| F-2 | A minimal report contains timestamp, sender, subject, content and receiver; the receiver defaults to the exercise leader | These five are fields of every `Report`, independent of the template ([ADR 0008](adr/0008-immutable-reports.md)) | `TestReportFlow`, `TestFillReport` |
| F-3 | Groups select a template for their report | Template picker on the group home page; templates add fields on top of the minimal report | `TestReportFlow`, `TestTemplates` |
| F-4 | The exercise leader sends a question / information request with an attached template | `Request` entity with mandatory `TemplateID`; answers are bound to request, template and leader | `TestRequestFlow` |
| F-5 | An administrator or the exercise leader creates templates | Template routes open to `leader` and `admin` only | `TestRoleAccess`, `TestTemplates` |
| F-6 | The leader has a queue of incoming reports and can filter, sort, search and file them; by default one queue per group | Queue view with per-sender queues, `ReportFilter`, `SortReports`, status and folder | `TestReportFlow`, `TestQueuePagination`, `TestFilter`, `TestSort` |

## Quality attribute drivers

| ID | Quality | Requirement | Addressed by | Verified by |
| --- | --- | --- | --- | --- |
| Q-1 | Simplicity of use | A participant must be able to send a report with no training, on any device with a browser | Server-rendered HTML forms, no client application, responsive CSS ([ADR 0002](adr/0002-server-rendered-html-no-javascript.md)) | Manual browser walkthrough; integration tests drive the same forms |
| Q-2 | Simplicity of operation | Bring-up for an exercise within minutes, on an isolated network, without a database server | Single static binary, file storage, first-run bootstrap ([ADR 0001](adr/0001-go-standard-library-only.md), [ADR 0003](adr/0003-journal-storage.md)) | `TestRunTLSBootstrapAndReload`, `make install` |
| Q-3 | Security | "Make it secure": confidentiality between groups, strong authentication, hardened process using SELinux, seccomp, systemd, Landlock, AppArmor and capabilities | Layered controls ([ADR 0005](adr/0005-authentication-and-sessions.md), [ADR 0006](adr/0006-layered-sandbox.md), [ADR 0007](adr/0007-tls-by-default.md)); see [security.md](security.md) | `internal/sandbox` enforcement tests, `internal/web` security tests, `make verify-deploy` |
| Q-4 | Integrity / auditability | A report cannot be altered after sending; sender and time are trustworthy | Server-set sender and timestamp, immutable reports, append-only journal, audit log ([ADR 0008](adr/0008-immutable-reports.md)) | `TestReportFlow`, `TestUpdateReport` |
| Q-5 | Durability | An acknowledged report survives a crash or power loss | Journal record is fsynced before the change becomes visible; torn final record tolerated ([ADR 0003](adr/0003-journal-storage.md)) | `TestPersistenceAndCompaction`, `TestTornAndCorruptJournal`, `FuzzReplay` |
| Q-6 | Portability | Target Linux; a development variant must run on macOS | Platform-specific code isolated behind build tags in `internal/sandbox`; everything else is portable Go ([ADR 0006](adr/0006-layered-sandbox.md)) | `GOOS=darwin go vet`, `make build-darwin`, macOS CI job |
| Q-7 | Configurability | Must run on a different port | `-addr`, `-port`, environment variables, Makefile `PORT`, low-port drop-in | `TestParseConfig` |
| Q-8 | Transport security | TLS support with tooling for self-signed and Let's Encrypt certificates | TLS required by default, reload on SIGHUP, two scripts ([ADR 0007](adr/0007-tls-by-default.md)) | `TestRunTLSBootstrapAndReload`, `TestEndToEndSandboxed` |
| Q-9 | Testability | Tests, code coverage with an 85 % floor, fuzzing | Pure domain package, injectable clock, in-process HTTP tests ([ADR 0009](adr/0009-testing-strategy.md)) | `make cover` (fails below the floor), `make fuzz` |
| Q-10 | Maintainability / supply chain | Small, auditable code base | No third-party modules, one binary, documented decisions ([ADR 0001](adr/0001-go-standard-library-only.md)) | `go.mod` has no `require` |

## Constraints

| ID | Constraint |
| --- | --- |
| C-1 | Implementation language Go |
| C-2 | Web technology for the user interface |
| C-3 | Build and common tasks through a Makefile |
| C-4 | Documentation in `docs/`, including design, architecture, ASRs and ADRs |

## Assumed scale

Not stated in the brief; the design assumes one exercise at a time with up
to a few hundred groups and tens of thousands of reports. All state is held
in memory (a report is bounded by the 256 KiB request limit and is typically under 2 KiB). See
[ADR 0003](adr/0003-journal-storage.md) for when to revisit this.
