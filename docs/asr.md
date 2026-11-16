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
| Q-1 | Simplicity of use | A participant must be able to send a report with no training, on any device with a browser | Server-rendered HTML forms, no client application, responsive CSS, optional script ([ADR 0011](adr/0011-progressive-enhancement.md)) | Manual browser walkthrough; integration tests drive the same forms |
| Q-2 | Simplicity of operation | Bring-up for an exercise within minutes, on an isolated network, without a database server | Single static binary with embedded SQLite, first-run bootstrap ([ADR 0010](adr/0010-vetted-dependencies.md), [ADR 0012](adr/0012-sqlite-storage.md)) | `TestRunTLSBootstrapAndReload`, `make install` |
| Q-3 | Security | "Make it secure": confidentiality between groups, strong authentication, hardened process using SELinux, seccomp, systemd, Landlock, AppArmor and capabilities | Layered controls ([ADR 0005](adr/0005-authentication-and-sessions.md), [ADR 0015](adr/0015-argon2id.md), [ADR 0006](adr/0006-layered-sandbox.md), [ADR 0007](adr/0007-tls-by-default.md)); see [security.md](security.md) | `internal/sandbox` enforcement tests, `internal/web` security tests, `make verify-deploy` |
| Q-4 | Integrity / auditability | A report cannot be altered after sending; sender and time are trustworthy | Server-set sender and timestamp, immutable reports, audit log ([ADR 0008](adr/0008-immutable-reports.md)) | `TestReportFlow`, `TestUpdateReport` |
| Q-5 | Durability | An acknowledged report survives a crash or power loss | SQLite transactions with `synchronous=FULL` in WAL mode ([ADR 0012](adr/0012-sqlite-storage.md)) | `TestPersistence`, `TestSchemaVersion` (settings), `FuzzReportRoundTrip` |
| Q-6 | Portability | Target Linux; a development variant must run on macOS | Platform-specific code isolated behind build tags in `internal/sandbox`; everything else is portable Go ([ADR 0006](adr/0006-layered-sandbox.md)) | `GOOS=darwin go vet`, `make build-darwin`, macOS CI job |
| Q-7 | Configurability | Must run on a different port | `-addr`, `-port`, environment variables, Makefile `PORT`, low-port drop-in | `TestParseConfig` |
| Q-8 | Transport security | TLS support with tooling for self-signed and Let's Encrypt certificates | TLS required by default, reload on SIGHUP, two scripts ([ADR 0007](adr/0007-tls-by-default.md)) | `TestRunTLSBootstrapAndReload`, `TestEndToEndSandboxed` |
| Q-9 | Testability | Tests, code coverage with an 85 % floor, fuzzing | Pure domain package, injectable clock, in-process HTTP tests ([ADR 0009](adr/0009-testing-strategy.md)) | `make cover` (fails below the floor), `make fuzz` |
| Q-10 | Maintainability / supply chain | Small, auditable code base | Few, justified, pinned modules; no cgo; one binary; documented decisions ([ADR 0010](adr/0010-vetted-dependencies.md)) | `go.mod` lists three direct dependencies (one test-only); `CGO_ENABLED=0` build |
| Q-11 | Internationalisation | The interface is available in English and Swedish; a user chooses the language | One message catalogue with English keys, language per browser ([ADR 0013](adr/0013-internationalisation.md)) | `TestLanguageSelection`, `TestSwedishFlow`, `TestCatalogComplete`, `TestCatalogVerbs` |
| Q-12 | Accessibility | Usable with keyboard and assistive technology; sufficient contrast (target: WCAG 2.1 AA) | Semantic markup, focus, live regions, contrast-checked colours ([ADR 0014](adr/0014-accessibility-and-themes.md)) | `TestAccessibility`, `TestAuditDetects`, `TestThemeContrast` |
| Q-13 | Themes | Selectable themes: light, dark, gold, spring, autumn, camouflage, console green | Themes as sets of CSS custom properties, preference per browser ([ADR 0014](adr/0014-accessibility-and-themes.md)) | `TestThemeSelection`, `TestThemeContrast` |

## Constraints

| ID | Constraint |
| --- | --- |
| C-1 | Implementation language Go |
| C-2 | Web technology for the user interface |
| C-3 | Build and common tasks through a Makefile |
| C-4 | Documentation in `docs/`, including design, architecture, ASRs and ADRs |
| C-5 | Go modules and JavaScript are permitted; SQLite is the storage engine (added in the second iteration, replacing the earlier self-imposed "standard library only, no script" rule) |

## Assumed scale

Not stated in the brief; the design assumes one exercise at a time with up
to a few hundred groups and tens of thousands of reports per queue. A
queue view loads the matching rows before sorting them; see
[ADR 0012](adr/0012-sqlite-storage.md) for when to revisit this.
