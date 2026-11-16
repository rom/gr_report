# 0007 – TLS required by default, terminated in the process

* Status: Accepted
* Date: 2026-11-16
* ASRs: Q-3, Q-7, Q-8

## Context

Passwords and reports cross a shared exercise network. The brief asks for
TLS support and scripts for self-signed and Let's Encrypt certificates.

## Decision

* The server refuses to start without a certificate and key unless
  `-insecure-http` is given explicitly. `-dev-seed` (demo accounts) is only
  accepted together with `-insecure-http`.
* TLS is terminated by the Go process itself (`crypto/tls`, minimum
  TLS 1.2, Go's default cipher selection). With TLS the server sends HSTS
  and uses `Secure`, `__Host-` cookies.
* The certificate is re-read on `SIGHUP`; a failed reload keeps the old
  one.
* Certificates are provisioned by two scripts outside the binary:
  `mkcert-selfsigned.sh` (openssl, ECDSA P-256, SANs) and
  `mkcert-letsencrypt.sh` (certbot, with a deploy hook that copies renewed
  files into place and reloads the service).
* The default port is 8443 so no privilege is needed; a systemd drop-in
  enables 443.

## Alternatives considered

* **Reverse proxy (nginx, Caddy) in front.** Another component to install
  and secure; hides client addresses from throttling; unnecessary for one
  small service.
* **Built-in ACME client.** Would require outbound connections from the
  server process – which the sandbox forbids on purpose – and the
  `x/crypto/acme` dependency.
* **Generating a self-signed certificate automatically at first start.**
  Convenient, but hides the trust decision; an explicit script that prints
  the fingerprint is clearer.
* **HTTP by default.** Insecure by default is the wrong default.

## Consequences

* Secure by default; the insecure mode is loud and explicit.
* The service never needs outbound network access.
* Renewal requires certbot's timer and port 80 (or a webroot).
* Self-signed certificates produce browser warnings; the operator must
  distribute the certificate or its fingerprint.
