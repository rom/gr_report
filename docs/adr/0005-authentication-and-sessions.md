# 0005 – Passwords with PBKDF2, server-side in-memory sessions

* Status: Accepted
* Date: 2026-11-16
* ASRs: Q-2, Q-3

## Context

Exercise networks are often isolated, so external identity providers
cannot be assumed. Accounts are created shortly before an exercise and
credentials are handed out on paper.

## Decision

* Username and password. New and reset accounts get a generated 16
  character one-time password that must be changed at first login. The
  first administrator password is written to a root-readable file, never
  to the log.
* Hash with PBKDF2-HMAC-SHA256, 600 000 iterations, per-password salt;
  parameters are stored in the hash string.
* Throttle failed logins per username+address and per address with
  exponential lock-out.
* Sessions are opaque 256-bit random tokens in an `HttpOnly`,
  `SameSite=Strict` cookie (`Secure` and `__Host-` prefixed with TLS). The
  server keeps `SHA-256(token) → session` in memory with idle (2 h) and
  absolute (12 h) time-outs.
* CSRF protection: per-session token in each form, plus
  `Sec-Fetch-Site`/`Origin` verification.

## Alternatives considered

* **OIDC / SSO.** Not available offline; too much set-up.
* **Signed stateless cookies (JWT).** Cannot be revoked on password reset
  or disable without server state anyway.
* **Persisting sessions in the database.** Survives restarts but writes
  secrets-equivalents to disk and adds churn. Not worth it for an
  exercise-length service.
* **Argon2id / bcrypt.** Not in the standard library. Third-party modules
  are allowed since [ADR 0010](0010-vetted-dependencies.md), so moving to
  Argon2id (`golang.org/x/crypto`) is a reasonable follow-up; the hash
  format already carries its algorithm name to allow a gradual change.
* **TOTP second factor.** Out of proportion for shared group accounts.

## Consequences

* Works with no external service.
* Restarting the server logs everyone out.
* Sessions can be revoked immediately; password change, reset and disable
  do so.
* Each login costs roughly 0.1–0.3 s of CPU; throttling keeps that from
  becoming a denial-of-service lever.
