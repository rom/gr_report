# 0018 – One YAML configuration file for all settings

* Status: Accepted
* Date: 2026-11-16
* ASRs: Q-2, Q-7, Q-10

## Context

Settings were command line flags with environment variable defaults, and
the installed service read a systemd `EnvironmentFile`. As settings grew
(TLS paths, language, chat, oversight, sandbox) that file became a flat
list of `GR_REPORT_…` variables with ad hoc boolean spellings (`=1`,
`=off`, `=on`). A configuration file with all settings in one place was
requested.

## Decision

* **All settings live in one YAML file**, by default installed as
  `/etc/gr_report/gr_report.yaml` and named with `-config FILE` (or
  `GR_REPORT_CONFIG`). The shipped file contains every setting, each with a
  comment and its built-in default.
* **Grouped keys**: `listen`, `data_dir`, `tls.cert`, `tls.key`,
  `language`, `chat.enabled`, `chat.oversight`, `sandbox`,
  `development.insecure_http`, `development.demo_accounts`.
* **Precedence**: built-in default < file < environment variable < flag.
  Flags and environment variables remain as overrides, because they are
  what development (`make dev PORT=9000`), tests and containers use; they
  are no longer where an installation keeps its settings.
* **Strict parsing.** Unknown keys, duplicate keys, wrong types and more
  than one YAML document are errors that name the file and the key. A
  misspelt setting must not silently fall back to a default – for
  `chat.oversight` or `sandbox` that would be a safety problem.
* **Relative paths in the file are relative to the file**, not to the
  working directory of the process.
* **`-print-config`** prints the effective configuration, after all
  overrides, in the file's own format and exits; the output is itself a
  valid configuration file.
* **No reload.** The file is read once at start, before the sandbox is
  applied; the process needs no access to it afterwards. Changing a
  setting means restarting the service (certificate renewal keeps using
  `SIGHUP`).
* **No secrets in the file.** It holds paths and switches only, so it is
  world-readable (0644) like other files in `/etc`.
* Parser: `go.yaml.in/yaml/v3`, the maintained continuation of
  `gopkg.in/yaml.v3` (recorded in [ADR 0010](0010-vetted-dependencies.md)).

## Alternatives considered

* **Keep the environment file.** Flat, no structure, no comments tied to
  values in a standard way, and only usable under systemd.
* **TOML or JSON.** TOML would do equally well; JSON has no comments. YAML
  was asked for.
* **File only, no flags or environment.** Purer, but it would force a file
  for every development run and test, and make one-off overrides awkward.
* **Lenient parsing (ignore unknown keys).** Friendlier across versions,
  but hides typing mistakes in security-relevant settings.
* **Hand-written parser for a YAML subset.** Avoids a dependency, but YAML
  is not a format to re-implement.
* **Reload on SIGHUP.** Most settings (listen address, data directory,
  sandbox) cannot change in a running, sandboxed process; a partial reload
  would be confusing.

## Consequences

* An installation has one documented place for its settings; the systemd
  unit carries no configuration.
* A third runtime dependency. YAML's known surprises (for example `no`
  read as a boolean) are contained by the schema: every key has a fixed
  type and mismatches are errors.
* Upgrading to a version that removes or renames a key makes the old file
  an error at start, with the key named – deliberate.
* The previous `/etc/gr_report/gr_report.env` is no longer read by the
  unit; its values must be moved to the YAML file once.
