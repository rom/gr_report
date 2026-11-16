package model

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzFillReport checks that arbitrary user input never panics and that an
// accepted report respects every size limit and is clean UTF-8.
func FuzzFillReport(f *testing.F) {
	f.Add("subject", "content", "Grid 1", "2026-05-01T12:30", "Red", "12", "on", "notes")
	f.Add("", "", "", "", "", "", "", "")
	f.Add("s\x00\xff", "c\r\n\x1b[2J", "x", "9999-99-99T99:99", "Blue", "1e9", "", strings.Repeat("n", 5000))
	tp := sitrep()
	if err := tp.Validate(); err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, subject, content, loc, when, status, count, help, notes string) {
		in := map[string]string{"location": loc, "when": when, "status": status, "count": count, "help": help, "notes": notes}
		var r Report
		if err := FillReport(&r, &tp, subject, content, func(k string) string { return in[k] }); err != nil {
			return
		}
		if n := utf8.RuneCountInString(r.Subject); n < 1 || n > MaxSubjectLen || strings.ContainsAny(r.Subject, "\r\n\x00") {
			t.Errorf("bad subject %q", r.Subject)
		}
		if n := utf8.RuneCountInString(r.Content); n < 1 || n > MaxContentLen || strings.ContainsAny(r.Content, "\r\x00") {
			t.Errorf("bad content %q", r.Content)
		}
		if len(r.Fields) != len(tp.Fields) {
			t.Fatalf("got %d fields", len(r.Fields))
		}
		for _, fv := range r.Fields {
			if !utf8.ValidString(fv.Value) || utf8.RuneCountInString(fv.Value) > MaxFieldValueLen {
				t.Errorf("bad field value %q", fv.Value)
			}
		}
		if v := r.Fields[2].Value; v != "Green" && v != "Red" {
			t.Errorf("select accepted %q", v)
		}
		if v := r.Fields[4].Value; v != "yes" && v != "no" {
			t.Errorf("checkbox value %q", v)
		}
	})
}

// FuzzTemplateValidate checks that a validated template has unique, well
// formed keys and that validation is idempotent.
func FuzzTemplateValidate(f *testing.F) {
	f.Add("SITREP", "desc", "Location", "text", "", "Status", "select", "a,b", "status")
	f.Add("", "", "", "", "", "", "", "", "")
	f.Add("x", "y", "!!!", "checkbox", "", "!!!", "blob", ",,", "Bad Key")
	f.Fuzz(func(t *testing.T, name, desc, l1, t1, k1, l2, t2, o2, k2 string) {
		tp := Template{Name: name, Description: desc, Fields: []Field{
			{Key: k1, Label: l1, Type: FieldType(t1)},
			{Key: k2, Label: l2, Type: FieldType(t2), Options: ParseOptions(o2)},
			{Label: l1, Type: FieldText},
		}}
		if err := tp.Validate(); err != nil {
			return
		}
		seen := map[string]bool{}
		for _, fd := range tp.Fields {
			if !keyRe.MatchString(fd.Key) || seen[fd.Key] {
				t.Errorf("bad or duplicate key %q", fd.Key)
			}
			seen[fd.Key] = true
			if fd.Type == FieldSelect && len(fd.Options) == 0 {
				t.Error("select without options")
			}
		}
		again := tp.Clone()
		if err := again.Validate(); err != nil {
			t.Fatalf("not idempotent: %v", err)
		}
		for i := range tp.Fields {
			if tp.Fields[i].Key != again.Fields[i].Key {
				t.Errorf("key changed: %q -> %q", tp.Fields[i].Key, again.Fields[i].Key)
			}
		}
	})
}

// FuzzFilter checks search and sort with arbitrary queries.
func FuzzFilter(f *testing.F) {
	f.Add("fuel alpha", "subject", true)
	f.Add("", "", false)
	f.Add("\xff\x00 A", "status", false)
	f.Fuzz(func(t *testing.T, query, key string, desc bool) {
		flt := ReportFilter{Query: query}
		flt.Prepare()
		if len(flt.terms) > MaxQueryTerms {
			t.Fatal("too many terms")
		}
		var out []Report
		for _, r := range reports() {
			if flt.Match(&r) {
				out = append(out, r)
			}
		}
		if strings.TrimSpace(query) == "" && len(out) != len(reports()) {
			t.Error("empty query must match all")
		}
		SortReports(out, key, desc)
		SortReports(out, key, !desc)
	})
}

func FuzzClean(f *testing.F) {
	f.Add("a\r\nb\x00\xff")
	f.Fuzz(func(t *testing.T, s string) {
		line, text := CleanLine(s), CleanText(s)
		if !utf8.ValidString(line) || !utf8.ValidString(text) {
			t.Error("invalid UTF-8 after cleaning")
		}
		if strings.ContainsAny(line, "\r\n\t\x00") || strings.ContainsAny(text, "\r\x00") {
			t.Errorf("control characters survived: %q %q", line, text)
		}
		if CleanLine(line) != line || CleanText(text) != text {
			t.Error("cleaning is not idempotent")
		}
	})
}
