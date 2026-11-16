# Operations

## Requirements

* Linux, amd64 or arm64. Kernel 6.7 or newer gives the complete in-process
  sandbox (Landlock with network rules); older kernels work with
  `GR_REPORT_SANDBOX=auto`.
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
| `/etc/gr_report/gr_report.env` | configuration (kept on re-install) |
| `/etc/gr_report/tls/` | certificate and key, `root:gr_report 0750` |
| `/var/lib/gr_report/` | data, created by systemd, `gr_report 0700` |
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

Every setting is a flag and an environment variable; flags win. The service
reads `/etc/gr_report/gr_report.env`.

| Flag | Environment | Default | Meaning |
| --- | --- | --- | --- |
| `-addr` | `GR_REPORT_ADDR` | `:8443` | Listen address `host:port` |
| `-port` | `GR_REPORT_PORT` | – | Port; overrides the port of `-addr` |
| `-data` | `GR_REPORT_DATA` | `./data` | Data directory |
| `-tls-cert` | `GR_REPORT_TLS_CERT` | – | Certificate chain (PEM) |
| `-tls-key` | `GR_REPORT_TLS_KEY` | – | Private key (PEM) |
| `-sandbox` | `GR_REPORT_SANDBOX` | `auto` | `auto`, `strict` or `off` |
| `-lang` | `GR_REPORT_LANG` | `en` | Default language, `en` or `sv` |
| `-chat` | `GR_REPORT_CHAT` | on | Chat between participants; `-chat=false` or `GR_REPORT_CHAT=off` removes it |
| `-chat-oversight` | `GR_REPORT_CHAT_OVERSIGHT=on` | off | Let the exercise leader read chat between groups |
| `-insecure-http` | `GR_REPORT_INSECURE_HTTP=1` | off | Plain HTTP, development only |
| `-dev-seed` | – | off | Demo accounts; only with `-insecure-http` |
| `-version` | – | | Print version |

Without `-tls-cert`/`-tls-key` the server refuses to start unless
`-insecure-http` is given.

### Chat oversight

By default a conversation between two groups can be read by those two
groups only. With `GR_REPORT_CHAT_OVERSIGHT=on` the exercise leaders can
read – not write – such conversations. The groups see a notice in the
conversation and a tag on every message the leader can read.

Oversight only covers messages sent while it is on. **Switch it on before
the exercise starts** if you want it from the beginning; messages written
earlier stay private and cannot be opened up afterwards. Switching it off
closes the leaders' view at once. The setting takes effect at start, so
changing it means restarting the service; its state is logged at start, and
every look by a leader is logged as `event=chat_oversight_view`.

Tell the participants in the exercise instructions whether oversight is
used.

### Language

Each user picks English or Swedish under **Settings**; until then the
browser's language is used. `-lang` / `GR_REPORT_LANG` sets what users see
when their browser asks for neither, and the language of the two starter
templates created on the very first start. Set it before the first start
if you want Swedish starter templates.

### Running on a different port

Set the port in the configuration and restart:

```bash
sudo sed -i 's/^GR_REPORT_ADDR=.*/GR_REPORT_ADDR=:9443/' /etc/gr_report/gr_report.env
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

Then set `GR_REPORT_ADDR=:443` and restart. With SELinux also run
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
seccomp=true`. With `GR_REPORT_SANDBOX=strict` the service does not start
unless all of them are.

```bash
systemd-analyze security gr_report
```

rates the unit (expected: about 1.1, "OK").

## Backup and restore

All state is the SQLite database `/var/lib/gr_report/gr_report.db`. While
the service runs there are also `gr_report.db-wal` and `gr_report.db-shm`
next to it; recent changes live in the `-wal` file, so **do not copy only
the `.db` file of a running server**.

Online backup, consistent and without stopping the service (needs the
`sqlite3` command line tool):

```bash
sudo sqlite3 /var/lib/gr_report/gr_report.db ".backup '/backup/gr_report-$(date +%F).db'"
```

Or stop the service and copy the file:

```bash
sudo systemctl stop gr_report && sudo cp /var/lib/gr_report/gr_report.db /backup/ && sudo systemctl start gr_report
```

Restore: stop the service, remove `gr_report.db-wal` and
`gr_report.db-shm`, put the backup in place as `gr_report.db` (owner
`gr_report`, mode `0600`), start the service.

The database contains password hashes and all chat conversations – treat
backups as sensitive. It can
be inspected with `sqlite3`, for example:

```bash
sudo sqlite3 -readonly /var/lib/gr_report/gr_report.db "SELECT status, COUNT(*) FROM reports GROUP BY status"
```

To start a fresh exercise, stop the service and remove the content of the
data directory; the next start creates a new administrator password.

## Logs

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
