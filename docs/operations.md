# Operations

## Requirements

* Linux, amd64 or arm64. Kernel 6.7 or newer gives the complete in-process
  sandbox (Landlock with network rules); older kernels work with
  `sandbox: auto`.
* systemd for the provided service unit.
* Go 1.24 or newer to build (modules are downloaded on the first build).
  The binary is static, includes SQLite and has no run-time dependencies.
* `openssl` (self-signed certificates) or `certbot` (Let's Encrypt).

## Install

```bash
sudo make install
```

installs

| Path | Content |
| --- | --- |
| `/usr/local/bin/gr_report` | the binary |
| `/etc/systemd/system/gr_report.service` | hardened service unit |
| `/etc/gr_report/gr_report.yaml` | all settings (kept on re-install) |
| `/etc/gr_report/tls/` | certificate and key, `root:gr_report 0750` |
| `/var/lib/gr_report/` | data, created by systemd, `gr_report 0700`; with `backups/` and `archive/` inside |
| user and group `gr_report` | unprivileged service account |

Then create a certificate (below) and start the service:

```bash
sudo systemctl enable --now gr_report
```

```bash
sudo cat /var/lib/gr_report/initial-admin-password
```

Log in as `admin`, change the password and delete that file.

## Configuration

All settings are in one YAML file, `/etc/gr_report/gr_report.yaml`. The
installed file lists every setting with an explanation and its default; the
service is started with `-config /etc/gr_report/gr_report.yaml`.

```yaml
listen: ":8443"
data_dir: /var/lib/gr_report
tls:
  cert: /etc/gr_report/tls/cert.pem
  key: /etc/gr_report/tls/key.pem
language: en
chat:
  enabled: true
  oversight: false
sandbox: strict
development:
  insecure_http: false
  demo_accounts: false
```

| Setting | Default | Meaning | Environment | Flag |
| --- | --- | --- | --- | --- |
| `listen` | `:8443` | Listen address `host:port` | `GR_REPORT_ADDR` | `-addr` |
| – | – | Port only; replaces the port of `listen` | `GR_REPORT_PORT` | `-port` |
| `data_dir` | `./data` | Database, lock, initial admin password | `GR_REPORT_DATA` | `-data` |
| `tls.cert` | – | Certificate chain (PEM) | `GR_REPORT_TLS_CERT` | `-tls-cert` |
| `tls.key` | – | Private key (PEM) | `GR_REPORT_TLS_KEY` | `-tls-key` |
| `language` | `en` | Default language, `en` or `sv` | `GR_REPORT_LANG` | `-lang` |
| `chat.enabled` | `true` | Chat between participants | `GR_REPORT_CHAT` | `-chat` |
| `chat.oversight` | `false` | Let the exercise leader read chat between groups (the initial state when the switch below is enabled) | `GR_REPORT_CHAT_OVERSIGHT` | `-chat-oversight` |
| `redirect_http` | – | Plain HTTP address that only redirects to HTTPS, e.g. `:8080` | `GR_REPORT_REDIRECT_HTTP` | `-redirect-http` |
| `timezone` | `UTC` | Time zone for users who have not chosen one | `GR_REPORT_TIMEZONE` | `-timezone` |
| `backup.interval` | `0` (off) | Automatic backup every so often, e.g. `1h` | `GR_REPORT_BACKUP_INTERVAL` | `-backup-interval` |
| `backup.keep` | `10` | Automatic backups to keep | `GR_REPORT_BACKUP_KEEP` | `-backup-keep` |
| – | – | Copy the database to a file and exit | – | `-backup FILE` |
| `chat.oversight_toggle` | `true` | Let the exercise leader switch oversight on and off on the chat page | `GR_REPORT_CHAT_OVERSIGHT_TOGGLE` | `-chat-oversight-toggle` |
| `sandbox` | `auto` | `auto`, `strict` or `off` | `GR_REPORT_SANDBOX` | `-sandbox` |
| `development.insecure_http` | `false` | Plain HTTP, development only | `GR_REPORT_INSECURE_HTTP` | `-insecure-http` |
| `development.demo_accounts` | `false` | Demo accounts; needs insecure HTTP | – | `-dev-seed` |

Rules:

* **Precedence:** built-in default, then the file, then an environment
  variable, then a flag. Environment variables and flags are meant for
  development and one-off overrides; keep an installation's settings in the
  file. Boolean environment variables accept `on`/`off`, `true`/`false`,
  `1`/`0`, `yes`/`no`.
* **Mistakes are errors.** An unknown or misspelt setting, a duplicate, or
  a value of the wrong type stops the server from starting, with a message
  naming the file and the setting.
* **Relative paths** in the file are relative to the file's directory.
* **Changes take effect at restart** (`systemctl restart gr_report`). Only
  the TLS certificate is re-read on `systemctl reload`.
* TLS is required: without `tls.cert` and `tls.key` the server refuses to
  start unless `development.insecure_http` is on.
* The file contains no secrets and can stay world-readable.

Check a file, and see what is in effect after all overrides, without
starting the server:

```bash
gr_report -config /etc/gr_report/gr_report.yaml -print-config
```

The file that was read is logged at start (`msg=configuration`).

**Upgrading from a version that used `/etc/gr_report/gr_report.env`:**
`make install` installs the new unit and `gr_report.yaml`. Copy your values
from the old file into the YAML file, then delete the old one; the unit no
longer reads it.

### Chat oversight

By default a conversation between two groups can be read by those two
groups only. With `chat.oversight: true` the exercise leaders can
read – not write – such conversations. The groups see a notice in the
conversation and a tag on every message the leader can read.

Oversight only covers messages sent while it is on. **Switch it on before
the exercise starts** if you want it from the beginning; messages written
earlier stay private and cannot be opened up afterwards. Switching it off
closes the leaders' view at once.

Who decides:

* With `chat.oversight_toggle: true` (the default) the **exercise leader**
  switches oversight on and off on the chat page, at any time and without a
  restart. That choice is stored in the database and survives restarts;
  `chat.oversight` is then only the state until the switch is first used.
  **This is the one setting whose effective value may differ from the
  configuration file.** The leader's chat page shows the current state and
  who set it; the start-up log shows it with its source; `-print-config`
  shows only the configured initial value.
* With `chat.oversight_toggle: false` **you** decide: `chat.oversight` is
  final, the switch disappears, and anything chosen with it earlier is
  ignored. Changing it then means editing the file and restarting.

Each change with the switch is logged as `event=chat_oversight_set`, and
every look by a leader is logged as `event=chat_oversight_view` (with
`live=true` when an open page received new messages).

Tell the participants in the exercise instructions whether oversight is
used.

### Redirecting HTTP to HTTPS

People type a host name without `https://`. With

```yaml
redirect_http: ":8080"
```

the server also listens for plain HTTP on that address and answers every
request with a permanent redirect to the same host and path on the HTTPS
port. Nothing else is served there. To use port 80, install the low-port
drop-in (see *Running on a different port*) and open the port in the
firewall. The example file ships with it switched off.

### Time zone

`timezone` is the zone times are shown in for users who have not chosen
their own under Settings – set it to the zone of the exercise, for example
`Europe/Stockholm`. Times are always stored in UTC, and the system log
stays in UTC. The time zone database is built into the program.

### Language

Each user picks English or Swedish under **Settings**; until then the
browser's language is used. The `language` setting decides what users see
when their browser asks for neither, and the language of the two starter
templates created on the very first start. Set it before the first start
if you want Swedish starter templates.

### Running on a different port

Set the port in the configuration and restart:

```bash
sudo sed -i 's/^listen:.*/listen: ":9443"/' /etc/gr_report/gr_report.yaml
```

```bash
sudo systemctl restart gr_report
```

Ad hoc: `gr_report -port 9443 …`, `GR_REPORT_PORT=9443 gr_report …`,
`make run PORT=9443` or `make dev PORT=9000`.

**Ports below 1024** (for example 443) need a capability. Install the
drop-in, which grants only `CAP_NET_BIND_SERVICE`; the process drops it
again as soon as the socket is open:

```bash
sudo install -D -m 0644 deploy/systemd/low-port.conf /etc/systemd/system/gr_report.service.d/low-port.conf
```

```bash
sudo systemctl daemon-reload
```

Then set `listen: ":443"` and restart. With SELinux also run
`setsebool -P gr_report_bind_low_ports on`.

With SELinux, a port that is neither labelled `http_port_t` (80, 443, 8008,
8443, …) nor `gr_report_port_t` must be labelled first:

```bash
sudo semanage port -a -t gr_report_port_t -p tcp 9443
```

A firewall, if any, must allow the port (`firewall-cmd --add-port=8443/tcp
--permanent`, `ufw allow 8443/tcp`).

## TLS certificates

The server reads the certificate at start and again on `systemctl reload
gr_report` (SIGHUP) – no restart, no dropped connections. If the new files
are invalid the old certificate stays in use and an error is logged.
TLS 1.2 is the minimum version.

The key should be owned by root and readable by the service group
(`root:gr_report`, mode `0640`), so the service cannot modify its own key.
Both scripts set this up.

### Self-signed

For closed exercise networks without public DNS:

```bash
sudo sh scripts/mkcert-selfsigned.sh -o /etc/gr_report/tls -n report.exercise.lan -a 192.0.2.10 -g gr_report
```

| Option | Meaning |
| --- | --- |
| `-o DIR` | output directory (default `./tls`) |
| `-n NAME` | primary host name (default `localhost`) |
| `-a LIST` | further names and IP addresses, comma separated |
| `-d DAYS` | validity (default 365) |
| `-g GROUP` | group that may read the key |
| `-f` | overwrite |

Browsers warn about self-signed certificates. Either distribute
`cert.pem` to the participants' devices as a trusted certificate, or tell
them the SHA-256 fingerprint the script prints so they can compare it
before accepting the warning.

### Let's Encrypt

Needs a public DNS name pointing at the server and port 80 reachable from
the internet during issuance and renewal.

```bash
sudo sh scripts/mkcert-letsencrypt.sh -n report.example.org -m admin@example.org
```

The script runs `certbot certonly --standalone` and installs a deploy hook
in `/etc/letsencrypt/renewal-hooks/deploy/` that copies each renewed
certificate to `/etc/gr_report/tls/` and reloads the service. Renewal
itself is done by certbot's own timer. Use `-s` to try it against the
staging environment first and `-w WEBROOT` if another web server owns
port 80.

## SELinux

```bash
sudo dnf install selinux-policy-devel
```

```bash
sudo make install-selinux
```

builds and loads the module in `deploy/selinux/` and labels the binary,
`/etc/gr_report` and `/var/lib/gr_report`. The service then runs in the
domain `gr_report_t`. Run `restorecon -R /etc/gr_report` after creating
certificates by hand.

## AppArmor

```bash
sudo make install-apparmor
```

loads `deploy/apparmor/usr.local.bin.gr_report`. Check with `aa-status`.

## Verifying the hardening

```bash
journalctl -u gr_report | grep sandbox
```

shows which in-process mechanisms are active, for example
`no_new_privs=true caps_dropped=true landlock_abi=7 landlock_net=true
seccomp=true`. With `sandbox: strict` the service does not start
unless all of them are.

```bash
systemd-analyze security gr_report
```

rates the unit (expected: about 1.1, "OK").

## Backup and restore

All state is the SQLite database `/var/lib/gr_report/gr_report.db`. While
the service runs there are also `gr_report.db-wal` and `gr_report.db-shm`
next to it; recent changes live in the `-wal` file, so **do not copy only
the `.db` file of a running server**. Use one of the following instead;
each writes a complete, consistent copy while the service keeps running.

| How | Where the copy goes |
| --- | --- |
| Automatically, every `backup.interval` (the shipped file says `1h`, keeping 24) | `/var/lib/gr_report/backups/auto-<UTC time>.db`; older ones beyond `backup.keep` are removed |
| An administrator presses **Backup → Make a backup now** | `/var/lib/gr_report/backups/manual-<UTC time>.db`; never removed automatically |
| From the command line | wherever you say |

```bash
sudo -u gr_report gr_report -config /etc/gr_report/gr_report.yaml -backup /var/lib/gr_report/backups/before-upgrade.db
```

The command needs read access to the data directory and write access to
the destination, and refuses to overwrite a file.

**The backups folder is on the same disk as the database.** It protects
against mistakes and a damaged database, not against losing the machine.
Copy the files elsewhere regularly, for example:

```bash
sudo rsync -a /var/lib/gr_report/backups/ backuphost:/srv/gr_report-backups/
```

Backups cannot be downloaded through the web interface, on purpose: they
contain password hashes, every report and every chat conversation.

**Restore:** stop the service, remove `gr_report.db-wal` and
`gr_report.db-shm`, put the backup in place as `gr_report.db` (owner
`gr_report`, mode `0600`), start the service:

```bash
sudo systemctl stop gr_report
```

```bash
sudo rm -f /var/lib/gr_report/gr_report.db-wal /var/lib/gr_report/gr_report.db-shm && sudo install -o gr_report -g gr_report -m 0600 /var/lib/gr_report/backups/auto-20261117-120000.db /var/lib/gr_report/gr_report.db
```

```bash
sudo systemctl start gr_report
```

The database can be inspected with `sqlite3`, for example:

```bash
sudo sqlite3 -readonly /var/lib/gr_report/gr_report.db "SELECT status, COUNT(*) FROM reports GROUP BY status"
```

## Starting a new exercise

An exercise leader or an administrator starts a new exercise under
**Exercise** in the web interface. That writes
`/var/lib/gr_report/archive/<UTC time>-<name>.db` – the whole database as
it was – and then removes reports, requests, chat, drafts and read
markers. Accounts, templates, saved filters, settings and the audit log
are kept. If the archive cannot be written, nothing is removed.

Archives are never deleted by the program; remove old ones yourself. To
look at an archived exercise, run a second instance on a copy of it:

```bash
mkdir /tmp/old-exercise && cp /var/lib/gr_report/archive/20261117-160000-spring-drill.db /tmp/old-exercise/gr_report.db
```

```bash
gr_report -data /tmp/old-exercise -insecure-http -addr 127.0.0.1:8081
```

To wipe everything including accounts, stop the service and remove the
content of the data directory; the next start creates a new administrator
password.

## Logs

The same audit events can be read by administrators in the web interface
under **Audit log** (the newest 100 000 are kept in the database).

The service logs to the journal. Lines with `msg=audit` record logins,
failed logins, account changes, sent reports and queue actions with user
name and client address:

```bash
journalctl -u gr_report | grep msg=audit
```

## Upgrade

```bash
git pull && sudo make install && sudo systemctl restart gr_report
```

Password hashes made by an earlier version (PBKDF2) keep working and are
converted to Argon2id the next time each user logs in.

Sessions are kept in memory, so users log in again after a restart. The
database schema is upgraded automatically at start (for example, the chat
tables are added to a database created by an earlier version); take a backup first. A
database that was written by a newer version is refused rather than
downgraded.

## Troubleshooting

| Symptom | Cause / fix |
| --- | --- |
| `TLS is required` at start | No certificate configured; create one or use `-insecure-http` for local development |
| `data directory is in use by another process` | A second instance uses the same `-data` directory |
| `schema version … is newer than this program supports` | The database was used with a newer gr_report; install that version or restore an older backup |
| `configuration file: … line 2: field lissten not found …` | A misspelt or unknown setting in `gr_report.yaml`; the message gives the line and the name |
| `listen (HTTP redirect): … permission denied` | `redirect_http` names a port below 1024 without the low-port drop-in |
| `unknown time zone` | `timezone` is not an IANA name such as `Europe/Stockholm` |
| `sandbox could not be fully applied` | `strict` on a kernel without Landlock network support (< 6.7); use `auto` |
| `listen … permission denied` | Port below 1024 without the drop-in, or SELinux port label missing |
| Lost the only administrator password | See below |

**Lost the only administrator password:** stop the service and copy the
password hash of an account whose password you know onto the `admin`
account:

```bash
sudo sqlite3 /var/lib/gr_report/gr_report.db "UPDATE users SET pass_hash = (SELECT pass_hash FROM users WHERE username = 'leader') WHERE username = 'admin'"
```

Start the service, log in as `admin` with that password and change it.
Keeping a second administrator account avoids this.
