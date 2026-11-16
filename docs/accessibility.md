# Languages, themes and accessibility

## Languages

The interface is available in **English** and **Swedish**.

* Until you choose, the language of your browser is used (the
  `Accept-Language` header). If the browser asks for neither language, the
  server's default applies (`-lang`, see [operations.md](operations.md)).
* Choose under **Settings** – also reachable from the login page
  ("Language and appearance"). The choice is stored in the browser, so
  people sharing a group account can each use their own language.
* Translated: all texts of the interface, including error messages.
  Not translated: what users write (reports, requests, template names and
  field labels) and timestamps, which are shown as `YYYY-MM-DD hh:mm:ssZ`
  in UTC.

Adding a language is described in [development.md](development.md).

## Themes

Choose under **Settings**; the page shows a sample of each.

| Theme | Description |
| --- | --- |
| System default | Follows the device: light, dark, or high contrast when the device asks for increased contrast |
| Light | Dark text on light grey |
| Dark | Light text on dark grey |
| High contrast | White and yellow on black, thicker lines; contrast at least 7:1 |
| Gold | Dark with gold accents |
| Spring | Light greens with a magenta accent |
| Autumn | Cream, rust and brown |
| Camouflage | Olive and khaki |
| Console green | Green on black, monospace type |

Like the language, the theme is stored per browser.

## Contrast

Every theme is checked automatically against WCAG 2.1 on each build:

| What | Requirement | Themes |
| --- | --- | --- |
| Text on its background (all combinations the stylesheet uses) | at least 4.5:1 (level AA) | all |
| The same, high contrast theme | at least 7:1 (level AAA) | High contrast |
| Borders of input fields, focus indicator | at least 3:1 | all |

The check computes the ratios from the colours in the stylesheet
(`TestThemeContrast`), so a new or changed theme cannot be merged with
insufficient contrast. Placeholder text uses a checked colour instead of
the browser's faint default.

## Accessibility

The target is **WCAG 2.1 level AA**. What is built in:

**Keyboard**

* Everything can be done with the keyboard alone.
* The first Tab stop on every page is *Skip to main content*.
* The focused control always has a clearly visible outline.
* The tab order follows the visual order.

**Screen readers and structure**

* Each page has a descriptive title, one main heading and ordered
  sub-headings; regions (navigation, queues, search, main content) are
  named landmarks.
* Every input has a label; groups of options are announced with their
  question; required fields are marked in text and in code.
* Tables have a name and column headers; the sorted column announces its
  direction.
* Errors are announced when the page loads and are linked to the form they
  belong to; confirmations and the "new reports have arrived" notice are
  announced politely, without moving focus.
* The page language is declared, so a screen reader uses the right voice.
* The chat message list is a log region: new messages are announced
  without moving focus. The list can be scrolled with the keyboard.

**Seeing**

* Status is never shown by colour alone: it is always written out
  ("new", "read", "filed"), and new reports are also bold.
* Text can be enlarged to 200 % and the layout reflows to one column on
  narrow screens; nothing requires horizontal scrolling of the page.
* The system settings for dark mode, increased contrast, forced colours
  (Windows high contrast) and reduced motion are respected.

**Robustness**

* All functions work without JavaScript. The script only adds conveniences.
* Nothing times out while you type, except the session itself (2 hours
  without activity).

### How this is verified

Automatically, on every build (`make test`):

* every page is rendered for every role, in both languages, with data and
  with validation errors, and its markup is checked against the rules
  above (`TestAccessibility`);
* the audit itself is tested against a deliberately broken page
  (`TestAuditDetects`);
* contrast is computed for every theme (`TestThemeContrast`).

By hand, in a browser: keyboard navigation and the skip link, each theme,
the system dark and increased-contrast settings, operation with JavaScript
disabled.

### Known limitations

* **No formal conformance claim.** Automated checks cover structure and
  contrast. The tool has not been reviewed with screen readers by users of
  assistive technology, and no browser-based scanner (such as axe) has been
  run against it. Do both before stating conformance.
* The optional auto-refresh of the queue reloads the page every minute,
  which moves a screen reader back to the top. It is off by default; with
  JavaScript the polite notice makes it unnecessary.
* The session ends after 2 hours without activity without a warning
  beforehand; an unsent report is then lost.
* Date and time fields use the browser's native controls, whose
  accessibility differs between browsers.
* The Console green theme passes the contrast ratio but saturated green on
  black is tiring to read for long periods.

Please report accessibility problems as issues on the repository.
