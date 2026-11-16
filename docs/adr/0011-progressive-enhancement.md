# 0011 – Server-rendered HTML with one optional same-origin script

* Status: Accepted (supersedes [0002](0002-server-rendered-html-no-javascript.md))
* Date: 2026-11-16
* ASRs: C-2, C-5, Q-1, Q-3, Q-12

## Context

[ADR 0002](0002-server-rendered-html-no-javascript.md) forbade JavaScript
to make script injection impossible by policy. Its costs were known: the
queue did not announce new reports, the template editor needed a round trip
to add a row, and a theme could not be previewed. JavaScript is now
allowed.

## Decision

Keep server-rendered HTML forms as the application. Add **one** script file,
`/static/app.js`, as progressive enhancement:

* a notice (a polite live region) and an updated counter when new reports
  arrive, by polling `GET /api/new-count` every 30 seconds – without
  reloading the page and losing what the user is typing or has ticked;
* "select all" in the queue;
* "add field" in the template editor;
* live preview of the theme on the settings page.

Rules that keep the security argument of 0002 mostly intact:

* The Content-Security-Policy is `default-src 'none'; script-src 'self';
  style-src 'self'; img-src 'self'; connect-src 'self'; …`. No inline
  script, no inline event handlers, no inline styles, no `eval`, no
  third-party origin.
* The script is hand-written, has no dependencies and no build step, and
  never inserts HTML: it only toggles attributes and sets `textContent`.
* **Every function works without the script.** Controls that only make
  sense with scripting are rendered `hidden` and revealed by the script.
* Text shown by the script comes from the server-rendered page (already
  translated), not from strings in the script.

## Alternatives considered

* **A front-end framework / single-page application.** Needs a build
  toolchain and a dependency tree far larger than the application; makes
  the no-script fallback and accessibility harder, not easier.
* **htmx or a similar small library.** Attractive, but one more
  dependency for four small behaviours.
* **Server-sent events or WebSocket for live updates.** Long-lived
  connections per user complicate time-outs and limits; polling one tiny
  JSON document every 30 seconds is enough for this purpose.
* **Stay without JavaScript.** The usability and accessibility gains (no
  forced page refresh, announced updates) are worth one small file.

## Consequences

* A markup injection could now load a script only from the server's own
  origin, where nothing user-controlled is served as script; inline
  injection is still blocked by the policy. The guarantee is weaker than
  "no script can ever run" and relies on never serving user content from
  `/static/` or with a script content type.
* The 60-second `<meta refresh>` remains as the no-script way to watch the
  queue.
* The script has no automated tests; it is exercised by hand in a browser
  ([ADR 0009](0009-testing-strategy.md)). A test checks that pages contain
  no inline script, style or event handler.
