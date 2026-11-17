# 0023 – Times in the user's time zone, chosen per browser

* Status: Accepted
* Date: 2026-11-17
* ASRs: F-10, Q-1

## Context

All times were shown in UTC. Participants think in local time; an
exercise may also span zones, and a staff may prefer one common time.

## Decision

* Times are stored in UTC, as before, and **formatted per request** in the
  viewer's zone: the `gr_tz` cookie if set, else the server's `timezone`
  setting, else UTC.
* The zone is an IANA name (for example `Europe/Stockholm`), typed or
  picked from suggestions on the settings page; a button fills in the
  device's zone. Unknown names are ignored.
* A displayed time always says what it is: `…Z` in UTC, otherwise the
  zone's abbreviation (`CET`, `CEST`). Pages with lists state the zone.
* Input follows the same zone: date filters mean the user's calendar day,
  deadlines are typed in local time.
* The CSV export carries both the local time and an unambiguous UTC time.
* The time zone database is compiled into the binary (`time/tzdata`): the
  sandbox hides the system's copy and minimal systems may have none.
* Like language and theme, the choice is per browser
  ([ADR 0013](0013-internationalisation.md)).

## Alternatives considered

* **Convert in the browser with JavaScript.** Needs the script, breaks the
  no-script promise and cannot cover the CSV export.
* **Detect the zone automatically.** The server cannot know it without a
  script; the button offers it without making it silent.
* **One zone for the whole server.** Simple, but wrong for participants
  elsewhere; it remains available as the default.
* **Store local times.** Ambiguous at daylight-saving changes.

## Consequences

* Two people can see different clock times for the same report; the zone
  label makes that explicit.
* The binary grows by roughly 450 kB for the zone data, and zone rules are
  as fresh as the Go release it was built with.
* The audit log is shown in the viewer's zone; the system log stays UTC.
