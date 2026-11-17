# 0026 – Live queue by re-fetching the page; notifications in the browser

* Status: Accepted (extends [0011](0011-progressive-enhancement.md))
* Date: 2026-11-17
* ASRs: F-12, F-16, Q-12

## Context

The leader's queue changed only on reload, and nothing drew attention to a
new report when the leader was looking at another window.

## Decision

* **Live queue.** Every 10 seconds the page script fetches *the same
  page* again, parses it into an inert document with `DOMParser`, and
  replaces three elements by ID: the table body, the counter line and the
  list of queues. Ticked check boxes and keyboard focus are carried over.
  It only runs on the first page of the list and pauses while the tab is
  hidden.
* There is **no separate JSON representation** of the queue: the HTML is
  rendered and escaped by the same template either way, so the two cannot
  drift and no client-side templating is needed. The script still never
  builds markup from strings.
* **Notifications.** When the polled counters rise, the script can show a
  desktop notification (Notifications API, after the user grants
  permission) and play a short tone generated with Web Audio. Both are off
  by default, switched on under Settings, stored in the browser, and work
  only while the site is open in a tab.
* The notification text is a fixed, translated sentence. It never contains
  a subject, a sender or message text, because it appears on lock screens
  and shared displays.
* The counter poll runs every 15 seconds (was 30).

## Alternatives considered

* **Server-sent events / WebSocket.** Instant, but long-lived connections
  against the server's time-outs; polling is adequate here
  ([ADR 0016](0016-chat.md)).
* **A JSON API and client-side rendering of rows.** A second rendering of
  the same data, to be kept escaped and translated in two places.
* **Web Push with a service worker.** Works with the tab closed, but needs
  a push service on the internet – unavailable on isolated exercise
  networks.
* **A sound file.** Another asset and a `media-src` entry in the CSP; a
  generated tone needs neither.

## Consequences

* A new report appears in an open queue within about 10 seconds.
* Each open queue page requests the page every 10 seconds; with many
  leaders and a very large queue this is the first thing to revisit.
* Browsers may block sound until the user has interacted with the page,
  and block notifications entirely by policy; the settings page reports a
  refusal.
* Without JavaScript the queue is as before, with the optional page
  refresh.
