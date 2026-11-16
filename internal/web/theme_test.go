package web

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	// A theme block: `[data-theme="gold"] { ... }`, optionally preceded by
	// `:root, ` (the default) and possibly indented inside a media query.
	themeBlockRe = regexp.MustCompile(`(?m)^(\s*)(?::root, |:root)?\[data-theme="([a-z]+)"\] \{([^}]*)\}`)
	cssVarRe     = regexp.MustCompile(`--([a-z-]+):\s*(#[0-9a-fA-F]{6});`)
	varUseRe     = regexp.MustCompile(`var\(--([a-z-]+)\)`)
)

type palette map[string]string

// parseThemes returns the top-level theme blocks and the blocks inside
// media queries (used by the "auto" theme), in file order.
func parseThemes(t *testing.T, css string) (named map[string]palette, auto []palette) {
	t.Helper()
	named = map[string]palette{}
	for _, m := range themeBlockRe.FindAllStringSubmatch(css, -1) {
		p := palette{}
		for _, v := range cssVarRe.FindAllStringSubmatch(m[3], -1) {
			p[v[1]] = v[2]
		}
		if m[1] != "" || m[2] == "auto" {
			auto = append(auto, p)
			continue
		}
		if _, dup := named[m[2]]; dup {
			t.Errorf("theme %q defined twice", m[2])
		}
		named[m[2]] = p
	}
	return named, auto
}

func luminance(t *testing.T, hex string) float64 {
	t.Helper()
	n, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil {
		t.Fatalf("bad colour %q", hex)
	}
	lin := func(c uint64) float64 {
		v := float64(c) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(n>>16&0xff) + 0.7152*lin(n>>8&0xff) + 0.0722*lin(n&0xff)
}

// contrast is the WCAG 2.1 contrast ratio of two colours.
func contrast(t *testing.T, a, b string) float64 {
	la, lb := luminance(t, a), luminance(t, b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Foreground/background pairs that the stylesheet actually combines.
var (
	textPairs = [][2]string{
		{"ink", "bg"}, {"ink", "panel"}, {"ink", "row"}, {"ink", "warn-bg"}, {"ink", "ok-bg"},
		{"muted", "bg"}, {"muted", "panel"}, {"muted", "row"},
		{"accent", "bg"}, {"accent", "panel"}, {"accent", "row"}, {"accent", "warn-bg"}, {"accent", "ok-bg"},
		{"accent-ink", "accent"}, {"badge-ink", "new"},
		{"new", "bg"}, {"new", "panel"}, {"new", "row"}, {"ok", "panel"}, {"ok", "row"},
	}
	// Non-text contrast (WCAG 1.4.11): input borders and the focus ring.
	uiPairs = [][2]string{{"border", "panel"}, {"border", "bg"}, {"focus", "bg"}, {"focus", "panel"}, {"accent", "panel"}}
)

func checkPalette(t *testing.T, name string, p palette, textMin float64) {
	t.Helper()
	check := func(pairs [][2]string, min float64, kind string) {
		for _, pair := range pairs {
			fg, bg := p[pair[0]], p[pair[1]]
			if fg == "" || bg == "" {
				t.Errorf("theme %s: --%s or --%s is not defined", name, pair[0], pair[1])
				continue
			}
			if c := contrast(t, fg, bg); c < min {
				t.Errorf("theme %s: %s contrast of --%s (%s) on --%s (%s) is %.2f:1, need %.1f:1", name, kind, pair[0], fg, pair[1], bg, c, min)
			}
		}
	}
	check(textPairs, textMin, "text")
	check(uiPairs, 3, "non-text")
}

// TestThemeContrast verifies every theme against WCAG 2.1: 4.5:1 for text
// (level AA), 7:1 for the high contrast theme (level AAA) and 3:1 for
// borders and the focus indicator.
func TestThemeContrast(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	named, auto := parseThemes(t, css)

	for _, th := range Themes {
		if th.Name == "auto" {
			continue
		}
		p, ok := named[th.Name]
		if !ok {
			t.Errorf("theme %q has no block in app.css", th.Name)
			continue
		}
		min := 4.5
		if th.Name == "contrast" {
			min = 7
		}
		t.Run(th.Name, func(t *testing.T) { checkPalette(t, th.Name, p, min) })
	}
	for name := range named {
		if !validTheme(name) {
			t.Errorf("app.css defines theme %q that is not in web.Themes", name)
		}
	}

	// "auto" must resolve to exactly the dark and contrast palettes.
	if len(auto) != 2 {
		t.Fatalf("expected 2 media-query blocks for the auto theme, got %d", len(auto))
	}
	for i, want := range []string{"dark", "contrast"} {
		if fmt.Sprint(auto[i]) != fmt.Sprint(named[want]) {
			t.Errorf("auto block %d differs from the %s theme", i, want)
		}
	}
	if !strings.Contains(css, "@media (prefers-color-scheme: dark)") || !strings.Contains(css, "@media (prefers-contrast: more)") ||
		!strings.Contains(css, "@media (forced-colors: active)") || !strings.Contains(css, "@media (prefers-reduced-motion: reduce)") {
		t.Error("a user-preference media query is missing")
	}

	// Every colour variable the stylesheet uses is defined by every theme,
	// and nothing outside the theme blocks hard-codes a screen colour.
	used := map[string]bool{}
	for _, m := range varUseRe.FindAllStringSubmatch(css, -1) {
		used[m[1]] = true
	}
	for name, p := range named {
		for v := range used {
			if v == "font" || v == "line-width" {
				continue
			}
			if p[v] == "" {
				t.Errorf("theme %s does not define --%s", name, v)
			}
		}
	}
	rest := themeBlockRe.ReplaceAllString(css, "")
	if i := strings.Index(rest, "@media print"); i >= 0 {
		rest = rest[:i]
	}
	if m := regexp.MustCompile(`#[0-9a-fA-F]{3,6}\b`).FindString(rest); m != "" {
		t.Errorf("colour %s is hard-coded outside a theme", m)
	}
	if !strings.Contains(css, ":focus-visible { outline: 3px solid var(--focus)") {
		t.Error("no visible focus indicator")
	}
	// Browsers render placeholders too faint by default.
	if !strings.Contains(css, "::placeholder { color: var(--muted); opacity: 1; }") {
		t.Error("placeholder text does not use a checked colour")
	}
}

func TestContrastFormula(t *testing.T) {
	if c := contrast(t, "#000000", "#ffffff"); math.Abs(c-21) > 0.01 {
		t.Errorf("black on white = %.2f", c)
	}
	if c := contrast(t, "#777777", "#ffffff"); math.Abs(c-4.48) > 0.01 {
		t.Errorf("#777 on white = %.2f", c)
	}
}
