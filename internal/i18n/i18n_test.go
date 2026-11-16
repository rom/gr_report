package i18n

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestParseAndName(t *testing.T) {
	if l, ok := Parse("sv"); !ok || l != SV || l.Name() != "Svenska" {
		t.Errorf("Parse(sv) = %q, %v", l, ok)
	}
	if l, ok := Parse("en"); !ok || l.Name() != "English" {
		t.Errorf("Parse(en) = %q, %v", l, ok)
	}
	for _, bad := range []string{"", "SV", "de", "sv-SE", "../sv"} {
		if _, ok := Parse(bad); ok {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
}

func TestT(t *testing.T) {
	if got := T(EN, "Log in"); got != "Log in" {
		t.Errorf("EN = %q", got)
	}
	if got := T(SV, "Log in"); got != "Logga in" {
		t.Errorf("SV = %q", got)
	}
	// A missing translation degrades to English, never to nothing.
	if got := T(SV, "No such key %d", 7); got != "No such key 7" {
		t.Errorf("fallback = %q", got)
	}
	if got := T(Lang("xx"), "Log in"); got != "Log in" {
		t.Errorf("unknown language = %q", got)
	}
	// Labels and nested messages are translated, plain strings are not.
	if got := T(SV, "%s is required", Label("subject")); got != "ämne måste anges" {
		t.Errorf("label arg = %q", got)
	}
	if got := T(SV, "%s is required", "subject"); got != "subject måste anges" {
		t.Errorf("plain arg = %q", got)
	}
	inner := Errorf("%s must be at most %d characters", Label("option"), 80)
	if got := T(SV, "field %q: %s", "Färg", inner); got != `fältet "Färg": alternativ får ha högst 80 tecken` {
		t.Errorf("nested = %q", got)
	}
	// A percent sign in a message without arguments is left alone.
	if got := T(EN, "100% done"); got != "100% done" {
		t.Errorf("no-arg format = %q", got)
	}
}

func TestMsgAndTErr(t *testing.T) {
	var err error = Errorf("%s is required", Label("subject"))
	if err.Error() != "subject is required" {
		t.Errorf("Error() = %q", err.Error())
	}
	wrapped := fmt.Errorf("context: %w", err)
	if got := TErr(SV, wrapped); got != "ämne måste anges" {
		t.Errorf("TErr wrapped = %q", got)
	}
	if got := TErr(SV, errors.New("plain")); got != "plain" {
		t.Errorf("TErr plain = %q", got)
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		accept string
		def    Lang
		want   Lang
	}{
		{"", EN, EN},
		{"", SV, SV},
		{"sv", EN, SV},
		{"sv-SE,sv;q=0.9,en;q=0.8", EN, SV},
		{"en-GB,en;q=0.9,sv;q=0.8", SV, EN},
		{"de,fr;q=0.9", SV, SV},
		{"de, sv;q=0.5, en;q=0.4", EN, SV},
		{"en;q=0.2, sv;q=0.7", EN, SV},
		{"sv;q=0, en;q=0.1", SV, EN},
		{"SV-fi", EN, SV},
		{"sv;q=nonsense, en", SV, EN},
		{"sv;q=7", EN, EN},
		{"*", SV, SV},
		{strings.Repeat("de,", 30) + "sv", EN, EN},
	}
	for _, c := range cases {
		if got := Match(c.accept, c.def); got != c.want {
			t.Errorf("Match(%q, %s) = %s, want %s", c.accept, c.def, got, c.want)
		}
	}
}

var verbRe = regexp.MustCompile(`%[a-z]`)

func verbs(s string) string {
	v := verbRe.FindAllString(s, -1)
	return strings.Join(v, " ")
}

// TestCatalogVerbs makes sure a translation takes the same arguments in the
// same order as its English key.
func TestCatalogVerbs(t *testing.T) {
	for _, key := range Keys(SV) {
		tr := sv[key]
		if strings.TrimSpace(tr) == "" {
			t.Errorf("empty translation for %q", key)
		}
		if verbs(key) != verbs(tr) {
			t.Errorf("format verbs differ:\n  en: %q\n  sv: %q", key, tr)
		}
	}
}

var keyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\$\.Tr? "((?:[^"\\]|\\.)*)"`),                                        // templates
	regexp.MustCompile(`\bc\.t\("((?:[^"\\]|\\.)*)"`),                                        // handlers
	regexp.MustCompile(`i18n\.Errorf\("((?:[^"\\]|\\.)*)"`),                                  // validation
	regexp.MustCompile(`i18n\.Label\("((?:[^"\\]|\\.)*)"\)`),                                 // field names
	regexp.MustCompile(`deny\(http\.\w+, "((?:[^"\\]|\\.)*)"\)`),                             // login
	regexp.MustCompile(`\{"[a-z]+", "((?:[^"\\]|\\.)*)"\},`),                                 // web.Themes
	regexp.MustCompile(`model\.Sort\w+: +"((?:[^"\\]|\\.)*)"`),                               // queue column labels
	regexp.MustCompile(`(?:Role\w+ +Role|Status\w+ +Status|Field\w+ +FieldType) += "(\w+)"`), // enum values
}

// usedKeys scans the sources for message keys.
func usedKeys(t *testing.T) map[string]string {
	t.Helper()
	used := map[string]string{}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if ext := filepath.Ext(path); ext != ".go" && ext != ".html" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, re := range keyPatterns {
			for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
				key, err := strconv.Unquote(`"` + m[1] + `"`)
				if err != nil {
					t.Errorf("%s: cannot unquote %q", path, m[1])
					continue
				}
				used[key] = path
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"Forbidden", "Not Found", "Bad Request", "Unprocessable Entity", "Internal Server Error", "Exercise leader", "yes", "no"} {
		used[k] = "(dynamic)"
	}
	return used
}

// TestCatalogComplete fails when a message used anywhere in the program has
// no Swedish translation, and when the catalogue holds unused entries.
func TestCatalogComplete(t *testing.T) {
	used := usedKeys(t)
	if len(used) < 200 {
		t.Fatalf("only %d keys found; the scanner is broken", len(used))
	}
	var missing []string
	for key, where := range used {
		if _, ok := sv[key]; !ok {
			missing = append(missing, fmt.Sprintf("%q (%s)", key, where))
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("no Swedish translation for %s", m)
	}
	for _, key := range Keys(SV) {
		if _, ok := used[key]; !ok {
			t.Errorf("unused catalogue entry %q", key)
		}
	}
}

// FuzzMatch checks that arbitrary Accept-Language headers never panic and
// always yield a supported language.
func FuzzMatch(f *testing.F) {
	f.Add("sv-SE,sv;q=0.9,en;q=0.8")
	f.Add(";;q=,,,=;q=1e309")
	f.Add("")
	f.Fuzz(func(t *testing.T, accept string) {
		for _, def := range Languages {
			if _, ok := Parse(string(Match(accept, def))); !ok {
				t.Fatalf("Match(%q) returned an unsupported language", accept)
			}
		}
	})
}
