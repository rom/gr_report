# 0002 – Server-rendered HTML without JavaScript

* Status: Superseded by [0011](0011-progressive-enhancement.md)
* Date: 2026-11-16
* ASRs: C-2, Q-1, Q-3, Q-10

## Context

Participants use whatever device is at hand. The interaction is forms and
lists. Cross-site scripting is the classic way one participant could attack
another or the leader in a tool that displays user-written text.

## Decision

Render every page on the server with `html/template` and plain HTML forms.
Ship no JavaScript. Send a Content-Security-Policy of `default-src 'none'`
that only allows same-origin stylesheets and images.

## Alternatives considered

* **Single-page application with a JSON API.** More responsive, but needs a
  build toolchain and a large dependency tree, doubles the validation
  logic, and requires a CSP that allows scripts.
* **Progressive enhancement with a little script** (live queue updates,
  dynamic template editor). Useful, but each script weakens the
  "no script can run here" guarantee. Deferred.

## Consequences

* With no script allowed by policy, an escaping mistake cannot become
  script execution in a current browser.
* Works in any browser, including with scripting disabled.
* The queue does not update live; an optional 60-second
  `<meta http-equiv="refresh">` compensates.
* The template editor adds field rows by saving and re-rendering instead of
  dynamically.
* Every action is a full page load; fine at this scale.
