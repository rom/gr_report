# gr_report

A small, self-contained web tool for trainings and exercises in which groups
of people report to an exercise leader.

* **Groups** write reports from a template and send them to the exercise
  leader (the default receiver) or to another group.
* The **exercise leader** works through a queue of incoming reports – one
  queue per group – and can filter, sort, search and file them. The leader
  can also send a question / information request with a report template
  attached for the answer.
* The **administrator** or the exercise leader creates report templates. The
  administrator manages the accounts.
* Groups can have **personal accounts** for their members; reports can be
  saved as **drafts**, requests can have **deadlines**, lists can be
  **exported** as CSV, and times are shown in each user's **time zone**.
* The leader's queue updates live, with per-person read state, assignment
  to a leader, saved filters and optional desktop or sound notifications.
* Administrators can create accounts in bulk, read the **audit log**, make
  **backups**, and start a **new exercise** (archive and reset) from the
  interface.
* Groups and the exercise leader can **chat**: one conversation for
  everyone, and direct conversations between two groups or between a group
  and the exercise leader.

Every report carries at least a timestamp, the sender, a subject, the
content and the receiver.

* The interface is available in **English and Swedish**, follows
  **WCAG 2.1 AA** practices, and comes with **themes**: light, dark, high
  contrast, gold, spring, autumn, camouflage and console green.

One static Go binary with an embedded SQLite database – no database server
and no build step for the web interface, which works with or without
JavaScript. Linux is the target platform (systemd, Landlock, seccomp,
capabilities, SELinux, AppArmor); macOS works for development.

## Quick start (development)

```bash
make dev
```

Open <http://127.0.0.1:8080> and log in as `leader`, `alpha`, `bravo` or
`admin` with the password `exercise-dev-1`. Use another port with
`make dev PORT=9000`.

## Quick start (TLS)

```bash
make run PORT=8443
```

This creates a self-signed certificate in `./tls` and serves
<https://127.0.0.1:8443>. The one-time administrator password is written to
`./data/initial-admin-password`.

## Production install (Linux)

```bash
sudo make install
```

```bash
sudo sh scripts/mkcert-selfsigned.sh -o /etc/gr_report/tls -n report.example.org -g gr_report
```

```bash
sudo systemctl enable --now gr_report
```

All settings are in `/etc/gr_report/gr_report.yaml`. See
[docs/operations.md](docs/operations.md) for the settings, Let's Encrypt,
ports, SELinux/AppArmor and backup.

## Documentation

| Document | Content |
| --- | --- |
| [docs/user-guide.md](docs/user-guide.md) | How groups, leaders and administrators use the tool |
| [docs/accessibility.md](docs/accessibility.md) | Languages, themes, contrast and accessibility |
| [docs/operations.md](docs/operations.md) | Installation, configuration, ports, TLS, backup |
| [docs/security.md](docs/security.md) | Threat model and the security layers |
| [docs/architecture.md](docs/architecture.md) | Architecture: context, components, data, runtime |
| [docs/design.md](docs/design.md) | Detailed design of each component |
| [docs/asr.md](docs/asr.md) | Architecturally significant requirements |
| [docs/adr/](docs/adr/README.md) | Architecture decision records |
| [docs/development.md](docs/development.md) | Building, layout, macOS development |
| [docs/testing.md](docs/testing.md) | Tests, coverage floor, fuzzing |

## Quality gates

```bash
make check
```

runs lint, the test suite with an 85 % coverage floor (including the
accessibility audit, the theme contrast check and the translation
completeness check), all fuzz targets, validation of the deployment files
and the macOS cross-build.
