# 0013 – English and Swedish from one message catalogue; language per browser

* Status: Accepted
* Date: 2026-11-16
* ASRs: Q-11, Q-1

## Context

The user interface must be available in English and Swedish. Group
accounts are shared by several people ([ADR 0004](0004-roles-and-group-accounts.md)),
who may not want the same language.

## Decision

* **English text is the message key.** `i18n.T(lang, "Log in")` looks the
  key up in the catalogue of the language; a missing entry yields the
  English text, never a blank or a symbolic key. The Swedish catalogue is a
  Go map in `internal/i18n/sv.go`.
* **Format arguments** use `fmt` verbs. Arguments of type `i18n.Label`
  (for example the name of a form field) and nested messages are translated
  before formatting.
* **Validation errors are data.** `model` and `auth` return `*i18n.Msg`
  (key and arguments) instead of formatted strings; the HTTP layer
  translates them for the current request. `Error()` gives English, so logs
  and tests are stable.
* **Language of a request**: the `gr_lang` cookie if set, else the best
  match of the browser's `Accept-Language`, else the server default
  (`-lang`, `GR_REPORT_LANG`). The choice is made on the settings page,
  which is reachable without logging in.
* **The preference is stored per browser, not per account**, because
  accounts are shared.
* Pages carry `<html lang>` and `Content-Language`.
* **Not translated**: content written by users (reports, requests,
  template names and labels), and timestamps, which are ISO dates in UTC.
  The starter templates are created once, in the server's default language.
* **Completeness is enforced by tests**: the sources are scanned for keys;
  a key without a Swedish translation, an unused translation, or a
  translation whose format verbs differ from its key fails the test suite.

## Alternatives considered

* **`golang.org/x/text/message` with extracted catalogues.** Powerful
  (plural rules, number formatting) but a heavier workflow for two
  languages and about 290 messages. Plural forms are avoided in the texts
  instead ("report(s)").
* **Symbolic keys (`login.title`).** Robust against rewording of the
  English text, but unreadable templates and a blank or cryptic UI when an
  entry is missing.
* **Language stored on the account.** Wrong for shared accounts.
* **Language in the URL (`/sv/…`).** Good for public sites and search
  engines; irrelevant here and it would double every route.

## Consequences

* Adding a language is one map plus one line in `i18n.Languages`; the tests
  then list everything that needs translating.
* Rewording an English message changes its key; the completeness test
  points at the Swedish entry to update.
* Messages with counts are phrased to avoid plural forms.
* Stored values that are shown to users in several places (role, status,
  field type, "Exercise leader") stay English in the database and are
  translated on output.
