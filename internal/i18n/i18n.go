// Package i18n holds the message catalogue and the helpers to pick a
// language and translate messages. English text is the message key, so a
// missing translation degrades to English instead of to a blank.
package i18n

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Lang is a supported user interface language.
type Lang string

// Supported languages.
const (
	EN Lang = "en"
	SV Lang = "sv"
)

// Languages lists the supported languages in display order.
var Languages = []Lang{EN, SV}

// Name returns the name of the language in that language.
func (l Lang) Name() string {
	if l == SV {
		return "Svenska"
	}
	return "English"
}

// Parse converts a language code to a supported language.
func Parse(s string) (Lang, bool) {
	for _, l := range Languages {
		if string(l) == s {
			return l, true
		}
	}
	return "", false
}

// catalogs maps a language to its translations. English needs none.
var catalogs = map[Lang]map[string]string{SV: sv}

// Label marks a message argument that is itself a translatable message,
// such as the name of a form field.
type Label string

// Msg is a translatable message that can travel as an error.
type Msg struct {
	Key  string
	Args []any
}

// Errorf creates a translatable error. key is the English format string.
func Errorf(key string, args ...any) *Msg { return &Msg{Key: key, Args: args} }

// Error returns the English text.
func (m *Msg) Error() string { return T(EN, m.Key, m.Args...) }

// T translates key into lang and formats it with args. Arguments of type
// Label and *Msg are translated first.
func T(lang Lang, key string, args ...any) string {
	format := key
	if tr, ok := catalogs[lang][key]; ok {
		format = tr
	}
	if len(args) == 0 {
		return format
	}
	out := make([]any, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case Label:
			out[i] = T(lang, string(v))
		case *Msg:
			out[i] = T(lang, v.Key, v.Args...)
		default:
			out[i] = a
		}
	}
	return fmt.Sprintf(format, out...)
}

// TErr translates an error. Errors that are not a *Msg are returned as is.
func TErr(lang Lang, err error) string {
	var m *Msg
	if errors.As(err, &m) {
		return T(lang, m.Key, m.Args...)
	}
	return err.Error()
}

// Match picks the best supported language for an Accept-Language header
// value, or def when none matches.
func Match(accept string, def Lang) Lang {
	type pref struct {
		lang Lang
		q    float64
		pos  int
	}
	var prefs []pref
	for i, part := range strings.Split(accept, ",") {
		if i >= 20 {
			break // ignore absurdly long headers
		}
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f < 0 || f > 1 {
				continue
			}
			q = f
		}
		primary, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		if l, ok := Parse(primary); ok && q > 0 {
			prefs = append(prefs, pref{l, q, i})
		}
	}
	if len(prefs) == 0 {
		return def
	}
	sort.SliceStable(prefs, func(i, j int) bool {
		if prefs[i].q != prefs[j].q {
			return prefs[i].q > prefs[j].q
		}
		return prefs[i].pos < prefs[j].pos
	})
	return prefs[0].lang
}

// Keys returns the keys of the catalogue for lang, sorted (for tests).
func Keys(lang Lang) []string {
	keys := make([]string, 0, len(catalogs[lang]))
	for k := range catalogs[lang] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
