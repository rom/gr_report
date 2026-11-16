# 0015 – Argon2id for password hashing

* Status: Accepted (supersedes the hashing part of [0005](0005-authentication-and-sessions.md))
* Date: 2026-11-16
* ASRs: Q-3

## Context

[ADR 0005](0005-authentication-and-sessions.md) chose PBKDF2-HMAC-SHA256
because it was the only password hash in the Go standard library and
third-party modules were not allowed. PBKDF2 needs almost no memory, so
password guessing against a stolen database parallelises cheaply on GPUs.
Modules are allowed since [ADR 0010](0010-vetted-dependencies.md).

## Decision

* New hashes use **Argon2id** from `golang.org/x/crypto/argon2`, stored in
  the PHC string format
  `$argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>` (16-byte random salt,
  32-byte key).
* **Parameters: 19 MiB memory, 2 passes, 1 lane** – the configuration
  recommended by the OWASP Password Storage Cheat Sheet. They are read back
  from each hash, so they can be raised later without invalidating
  anything.
* **At most 4 derivations run at once** (`auth.MaxConcurrent`); further
  logins queue. This caps the memory a burst of logins can claim at about
  76 MiB, well inside the 512 MiB limit of the service unit, however many
  requests arrive.
* **Existing PBKDF2 hashes keep verifying.** After a successful login with
  a hash that uses the old scheme, or Argon2id with weaker parameters than
  configured, the password is re-hashed and stored
  (`Hasher.NeedsRehash`). If that write fails the login still succeeds.
  There is no forced password change.
* Parameters read from a stored hash are bounded (memory ≤ 256 MiB,
  passes ≤ 16, lanes ≤ 16) so a damaged or manipulated row cannot make the
  server allocate without limit.
* Unknown usernames are verified against a dummy Argon2id hash with the
  default parameters, as before, to keep response times uniform.

## Alternatives considered

* **Larger parameters (RFC 9106: 64 MiB, 3 passes, 4 lanes).** Stronger per
  guess, but each login would hold 64 MiB; the concurrency cap would have
  to drop to keep the service inside its memory limit, making a login
  flood an easier way to delay real users. The OWASP setting is the
  accepted baseline and can be raised per deployment later.
* **scrypt** (also in `x/crypto`). Memory-hard too; Argon2id is the current
  recommendation and won the Password Hashing Competition.
* **bcrypt.** Not memory-hard and truncates long passwords.
* **Force every user to set a new password.** Unnecessary: the hash is
  upgraded the moment the user proves knowledge of the password.
* **Remove PBKDF2 verification.** Would lock out every existing account.

## Consequences

* A stolen database is far more expensive to attack with GPUs or ASICs.
* One more direct dependency, `golang.org/x/crypto`, maintained by the Go
  team (recorded in ADR 0010).
* A login holds about 19 MiB of memory for a fraction of a second;
  under a burst, logins beyond the fourth wait their turn.
* Accounts that never log in again keep their PBKDF2 hash (600 000
  iterations). The administrator can list them:
  `SELECT username FROM users WHERE pass_hash NOT LIKE '$argon2id$%'`, and
  reset their passwords if that matters.
* A known-answer case of the Argon2 reference implementation is part of
  the tests, so a wrong parameter order or encoding would be noticed.
