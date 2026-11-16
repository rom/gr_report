# 0014 – WCAG 2.1 AA as the bar; themes as checked colour sets

* Status: Accepted
* Date: 2026-11-16
* ASRs: Q-12, Q-13, Q-1

## Context

The tool must be accessible, offer good contrast, and provide themes:
light, dark, gold, spring, autumn, camouflage and console green. Decorative
themes easily break contrast; accessibility easily regresses unnoticed when
templates change.

## Decision

**Target: WCAG 2.1 level AA.** Concretely:

* Semantic HTML first: landmarks (`header`, `nav`, `main`, `aside`), one
  `h1` per page, ordered headings, real `label`s, `fieldset`/`legend` for
  groups, table headers with `scope`, named tables and landmarks.
* A skip link as the first focusable element; a clearly visible focus ring
  on every control; no positive `tabindex`; no keyboard traps.
* Errors are announced (`role="alert"`) and tied to their form
  (`aria-describedby`); confirmations and live updates use `role="status"`.
* Status is never shown by colour alone: tags carry the word, new rows are
  bold, sort direction has an arrow and `aria-sort`.
* Text can be zoomed and reflows down to phone width; sizes are relative.
* `prefers-reduced-motion`, `prefers-contrast` and `forced-colors` are
  honoured.
* Everything works without JavaScript and with a keyboard only.

**Themes are data.** A theme is one block of CSS custom properties selected
by `<html data-theme="…">`; no rule outside those blocks contains a colour.

| Theme | Character |
| --- | --- |
| `auto` (default) | follows the device: light, dark, or high contrast when the user asks for more contrast |
| `light`, `dark` | neutral |
| `contrast` | black/white/yellow, WCAG AAA (7:1), thicker lines |
| `gold` | dark with gold accents |
| `spring` | light greens with a magenta accent |
| `autumn` | warm cream, rust and brown |
| `camouflage` | olive and khaki |
| `console` | green on black, monospace |

The theme is a per-browser preference (cookie `gr_theme`), for the same
reason as the language ([ADR 0013](0013-internationalisation.md)). The
value is validated against the list before it reaches the page.

**Both are enforced by tests**, because good intentions do not survive
maintenance:

* `TestThemeContrast` parses the stylesheet and computes the WCAG contrast
  ratio of every foreground/background pair the stylesheet combines, for
  every theme: at least 4.5:1 for text (7:1 for `contrast`) and 3:1 for
  input borders and the focus ring. It also fails on a theme that lacks a
  variable or on a colour hard-coded outside a theme.
* `TestAccessibility` renders every page for every role, in both
  languages, with data and with validation errors, and audits the markup
  for the rules above. `TestAuditDetects` feeds the audit a deliberately
  bad page to prove each rule fires.

## Alternatives considered

* **Light and dark only, via `prefers-color-scheme`.** Does not meet the
  requirement for named themes and gives the user no override.
* **Free colour pickers.** Cannot be contrast-checked in advance.
* **A CSS framework with theming.** A dependency and a build step for what
  is 20 variables.
* **An accessibility scanner (axe, pa11y) in CI.** Would catch more, in a
  real browser. It needs Node and a browser in the pipeline; the in-process
  audit covers the rules that markup can prove and runs in milliseconds.
  Running a scanner is recommended before a release.

## Consequences

* Adding a theme is one CSS block and one line in `web.Themes`; the
  contrast test accepts or rejects it.
* A template change that drops a label, skips a heading level or adds an
  inline style fails the test suite.
* Automated checks cover structure and contrast, **not** the experience
  with a screen reader, nor whether texts are understandable. No
  conformance claim is made; a manual review with assistive technology is
  still needed before stating conformance.
* The `console` theme's saturated green on black passes the ratio test but
  is tiring for long reading; it is offered because it was asked for.
