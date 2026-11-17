# 0027 – Optional plain HTTP listener that only redirects

* Status: Accepted (extends [0007](0007-tls-by-default.md))
* Date: 2026-11-17
* ASRs: F-17, Q-3

## Context

Typing the server's name without `https://` reached nothing, because the
server only listens with TLS.

## Decision

* `redirect_http` names a second listen address. A minimal handler there
  answers every request with `308 Permanent Redirect` to the same host and
  path on the HTTPS port; it serves nothing else and reads no request
  body.
* Off by default. It cannot be combined with insecure HTTP.
* The listener is opened before the sandbox is applied, like the main one.
* The target host is taken from the request and must be a plain host name
  or address; anything else gets 400.
* HSTS, already sent over HTTPS, makes browsers skip the plain request
  after the first visit.

## Alternatives considered

* **Leave it to a reverse proxy or the firewall.** Another component for
  one redirect.
* **Serve the application on both.** Insecure.
* **A configured public host name as redirect target.** Safer against a
  forged Host header, but one more setting that must match DNS; the forged
  header only affects the client that sent it.

## Consequences

* A first request typed without `https://` travels in clear text once –
  the URL, never credentials, since no page is served.
* Port 80 needs the low-port drop-in.
