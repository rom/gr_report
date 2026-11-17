package model

import (
	"strings"
	"testing"
	"time"
)

func TestRoleStatusFieldTypeValid(t *testing.T) {
	if !RoleAdmin.Valid() || !RoleLeader.Valid() || !RoleGroup.Valid() || Role("root").Valid() {
		t.Error("role validity")
	}
	if !StatusNew.Valid() || !StatusRead.Valid() || !StatusFiled.Valid() || Status("x").Valid() {
		t.Error("status validity")
	}
	if !FieldSelect.Valid() || FieldType("file").Valid() {
		t.Error("field type validity")
	}
}

func TestNewID(t *testing.T) {
	a, b := NewID(), NewID()
	if a == b || len(a) != 24 || !ValidID(a) {
		t.Errorf("bad ids %q %q", a, b)
	}
	for _, bad := range []string{"", "A", "a b", "../x", strings.Repeat("a", 41)} {
		if ValidID(bad) {
			t.Errorf("ValidID(%q) = true", bad)
		}
	}
}

func TestClean(t *testing.T) {
	if got := CleanLine("  a\tb\nc\x00 \xff "); got != "a b c  �" {
		t.Errorf("CleanLine = %q", got)
	}
	if got := CleanText(" a\r\nb\tc\x07\x1b[0m \n"); got != "a\nb\tc[0m" {
		t.Errorf("CleanText = %q", got)
	}
}

func TestUserValidate(t *testing.T) {
	u := User{Username: " Alpha-1 ", DisplayName: " Group\tAlpha ", Role: RoleGroup}
	if err := u.Validate(); err != nil {
		t.Fatal(err)
	}
	if u.Username != "alpha-1" || u.DisplayName != "Group Alpha" {
		t.Errorf("not normalised: %+v", u)
	}
	bad := []User{
		{Username: "a", DisplayName: "x", Role: RoleGroup},
		{Username: "-ab", DisplayName: "x", Role: RoleGroup},
		{Username: "a b", DisplayName: "x", Role: RoleGroup},
		{Username: "ok", DisplayName: "", Role: RoleGroup},
		{Username: "ok", DisplayName: strings.Repeat("x", MaxNameLen+1), Role: RoleGroup},
		{Username: "ok", DisplayName: "x", Role: "boss"},
	}
	for _, u := range bad {
		if err := u.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil", u)
		}
	}
}

func TestKeyFromLabelAndOptions(t *testing.T) {
	cases := map[string]string{
		"Needs & requests": "needs_requests", "  Åäö 12 ": "12", "": "", "__A__": "a",
		strings.Repeat("ab ", 30): strings.TrimSuffix(strings.Repeat("ab_", 14)[:40], "_"),
	}
	for in, want := range cases {
		if got := KeyFromLabel(in); got != want {
			t.Errorf("KeyFromLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if got := ParseOptions(" a, b ,,a, c\x00 "); strings.Join(got, "|") != "a|b|c" {
		t.Errorf("ParseOptions = %q", got)
	}
}

func sitrep() Template {
	return Template{ID: "t1", Name: "SITREP", Fields: []Field{
		{Label: "Location", Type: FieldText, Required: true},
		{Label: "When", Type: FieldDatetime},
		{Label: "Status", Type: FieldSelect, Required: true, Options: []string{"Green", "Red"}},
		{Label: "Count", Type: FieldNumber},
		{Label: "Help", Type: FieldCheckbox},
		{Label: "Notes", Type: FieldTextarea},
	}}
}

func TestTemplateValidate(t *testing.T) {
	tp := sitrep()
	tp.Fields = append(tp.Fields, Field{Label: "Status", Type: FieldText, Options: []string{"dropped"}}, Field{Label: "!!!", Type: FieldText})
	if err := tp.Validate(); err != nil {
		t.Fatal(err)
	}
	keys := ""
	for _, f := range tp.Fields {
		keys += f.Key + " "
	}
	if keys != "location when status count help notes status_2 field " {
		t.Errorf("keys = %q", keys)
	}
	if tp.Fields[6].Options != nil {
		t.Error("options kept on non-select field")
	}
	// Validation is idempotent.
	before := tp.Clone()
	if err := tp.Validate(); err != nil || tp.Fields[6].Key != before.Fields[6].Key {
		t.Errorf("second Validate changed keys: %v", err)
	}

	long := strings.Repeat("k", 40)
	dup := Template{Name: "x", Fields: []Field{{Key: long, Label: "a", Type: FieldText}, {Key: long, Label: "b", Type: FieldText}}}
	if err := dup.Validate(); err != nil || dup.Fields[1].Key != long[:38]+"_2" {
		t.Errorf("long duplicate key: %v %q", err, dup.Fields[1].Key)
	}

	many := make([]Field, MaxFields+1)
	opts := make([]string, MaxOptions+1)
	for i := range opts {
		opts[i] = strings.Repeat("o", i+1)
	}
	bad := []Template{
		{Name: ""},
		{Name: "x", Description: strings.Repeat("d", MaxDescriptionLen+1)},
		{Name: "x", DefaultSubject: strings.Repeat("d", MaxSubjectLen+1)},
		{Name: "x", ContentHelp: strings.Repeat("d", MaxSubjectLen+1)},
		{Name: "x", Fields: many},
		{Name: "x", Fields: []Field{{Label: "", Type: FieldText}}},
		{Name: "x", Fields: []Field{{Label: "a", Type: FieldText, Help: strings.Repeat("h", MaxSubjectLen+1)}}},
		{Name: "x", Fields: []Field{{Label: "a", Type: "blob"}}},
		{Name: "x", Fields: []Field{{Label: "a", Type: FieldText, Key: "Bad Key"}}},
		{Name: "x", Fields: []Field{{Label: "a", Type: FieldSelect, Options: []string{" "}}}},
		{Name: "x", Fields: []Field{{Label: "a", Type: FieldSelect, Options: opts}}},
		{Name: "x", Fields: []Field{{Label: "a", Type: FieldSelect, Options: []string{strings.Repeat("o", MaxNameLen+1)}}}},
	}
	for i, tp := range bad {
		if err := tp.Validate(); err == nil {
			t.Errorf("case %d: Validate = nil", i)
		}
	}
}

func TestRequestValidateAndAddressed(t *testing.T) {
	q := Request{Subject: " Status? ", Body: "a\r\nb", TemplateID: "t1", Recipients: []string{"g1"}}
	if err := q.Validate(); err != nil || q.Subject != "Status?" || q.Body != "a\nb" {
		t.Fatalf("Validate: %v %+v", err, q)
	}
	if !q.Addressed("g1") || q.Addressed("g2") {
		t.Error("Addressed with recipients")
	}
	q.Recipients = nil
	if !q.Addressed("g2") {
		t.Error("empty recipients must address all")
	}
	bad := []Request{
		{Subject: "", TemplateID: "t1"},
		{Subject: "s", Body: strings.Repeat("b", MaxContentLen+1), TemplateID: "t1"},
		{Subject: "s", TemplateID: ""},
		{Subject: "s", TemplateID: "t1", Recipients: []string{"../"}},
		{Subject: "s", TemplateID: "t1", Recipients: make([]string, MaxRecipients+1)},
	}
	for i, q := range bad {
		if err := q.Validate(); err == nil {
			t.Errorf("case %d: Validate = nil", i)
		}
	}
}

func TestCleanFolder(t *testing.T) {
	if f, err := CleanFolder(" Logistics\n"); err != nil || f != "Logistics" {
		t.Errorf("CleanFolder = %q, %v", f, err)
	}
	if _, err := CleanFolder(strings.Repeat("f", MaxFolderLen+1)); err == nil {
		t.Error("long folder accepted")
	}
}

func TestFillReport(t *testing.T) {
	tp := sitrep()
	if err := tp.Validate(); err != nil {
		t.Fatal(err)
	}
	good := map[string]string{"location": " Grid 1 ", "when": "2026-05-01T12:30", "status": "Red", "count": "-12,5", "help": "on", "notes": "a\r\nb"}
	var r Report
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if err := FillReport(&r, &tp, " Subject ", " Content\r\nline ", get(good)); err != nil {
		t.Fatal(err)
	}
	if r.Subject != "Subject" || r.Content != "Content\nline" || r.TemplateID != "t1" || r.TemplateName != "SITREP" {
		t.Errorf("report = %+v", r)
	}
	want := []string{"Grid 1", "2026-05-01T12:30", "Red", "-12,5", "yes", "a\nb"}
	for i, fv := range r.Fields {
		if fv.Value != want[i] || fv.Label != tp.Fields[i].Label {
			t.Errorf("field %d = %+v, want %q", i, fv, want[i])
		}
	}
	// Optional fields may be empty; an unchecked box is recorded as "no".
	var r2 Report
	if err := FillReport(&r2, &tp, "s", "c", get(map[string]string{"location": "x", "status": "Green"})); err != nil {
		t.Fatal(err)
	}
	if r2.Fields[4].Value != "no" || r2.Fields[1].Value != "" {
		t.Errorf("optional fields = %+v", r2.Fields)
	}

	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for kk, vv := range good {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	badInputs := []map[string]string{
		with("location", ""), with("when", "yesterday"), with("status", "Blue"), with("status", ""),
		with("count", "twelve"), with("count", "1e9"), with("notes", strings.Repeat("n", MaxFieldValueLen+1)),
	}
	for i, m := range badInputs {
		if err := FillReport(&Report{}, &tp, "s", "c", get(m)); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	for _, sc := range [][2]string{{"", "c"}, {"s", ""}, {strings.Repeat("s", MaxSubjectLen+1), "c"}, {"s", strings.Repeat("c", MaxContentLen+1)}} {
		if err := FillReport(&Report{}, &tp, sc[0], sc[1], get(good)); err == nil {
			t.Errorf("subject/content %d/%d accepted", len(sc[0]), len(sc[1]))
		}
	}
	must := Template{Name: "m", Fields: []Field{{Key: "ack", Label: "Ack", Type: FieldCheckbox, Required: true}}}
	if err := FillReport(&Report{}, &must, "s", "c", get(nil)); err == nil {
		t.Error("required checkbox not enforced")
	}
}

func TestClone(t *testing.T) {
	tp := sitrep()
	c := tp.Clone()
	c.Fields[2].Options[0] = "X"
	c.Fields[0].Label = "X"
	if tp.Fields[2].Options[0] != "Green" || tp.Fields[0].Label != "Location" {
		t.Error("Template.Clone is shallow")
	}
	q := Request{Recipients: []string{"a"}}
	qc := q.Clone()
	qc.Recipients[0] = "b"
	r := Report{Fields: []FieldValue{{Value: "a"}}}
	rc := r.Clone()
	rc.Fields[0].Value = "b"
	if q.Recipients[0] != "a" || r.Fields[0].Value != "a" {
		t.Error("Clone is shallow")
	}
}

func reports() []Report {
	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	return []Report{
		{ID: "a", Timestamp: t0, SenderID: "g1", SenderName: "Alpha", Subject: "bridge blocked", Content: "Need Fuel", ReceiverID: "leader", TemplateID: "t1", TemplateName: "SITREP", Status: StatusNew, Fields: []FieldValue{{Key: "loc", Value: "Grid 7"}}},
		{ID: "b", Timestamp: t0.Add(time.Hour), SenderID: "g2", SenderName: "bravo", Subject: "Arrived", Content: "all ok", ReceiverID: "leader", TemplateID: "t2", Status: StatusFiled, Folder: "Logistics", RequestID: "q1"},
		{ID: "c", Timestamp: t0.Add(26 * time.Hour), SenderID: "g1", SenderName: "Alpha", Subject: "arrived too", Content: "fuel ok", ReceiverID: "g2", TemplateID: "t1", Status: StatusRead},
		{ID: "d", Timestamp: t0, SenderID: "g2", SenderName: "bravo", Subject: "Zulu", Content: "", ReceiverID: "leader", Status: StatusRead},
	}
}

func ids(rs []Report) string {
	s := ""
	for _, r := range rs {
		s += r.ID
	}
	return s
}

func TestFilter(t *testing.T) {
	day := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		f    ReportFilter
		want string
	}{
		{ReportFilter{}, "abcd"},
		{ReportFilter{ReceiverID: "leader"}, "abd"},
		{ReportFilter{SenderID: "g1"}, "ac"},
		{ReportFilter{Status: StatusRead}, "cd"},
		{ReportFilter{TemplateID: "t1"}, "ac"},
		{ReportFilter{RequestID: "q1"}, "b"},
		{ReportFilter{Folder: "Logistics"}, "b"},
		{ReportFilter{Query: "FUEL"}, "ac"},
		{ReportFilter{Query: "fuel alpha  bridge"}, "a"},
		{ReportFilter{Query: "grid"}, "a"},
		{ReportFilter{Query: "sitrep"}, "a"},
		{ReportFilter{Query: "logistics"}, "b"},
		{ReportFilter{Query: "nothing"}, ""},
		{ReportFilter{From: day.Add(24 * time.Hour)}, "c"},
		{ReportFilter{To: day.Add(24 * time.Hour)}, "abd"},
		{ReportFilter{From: day, To: day.Add(11 * time.Hour)}, "ad"},
		{ReportFilter{Query: strings.Repeat("ok ", 50)}, "bc"},
	}
	for i, c := range cases {
		c.f.Prepare()
		got := ""
		for _, r := range reports() {
			if c.f.Match(&r) {
				got += r.ID
			}
		}
		if got != c.want {
			t.Errorf("case %d: got %q, want %q", i, got, c.want)
		}
	}
}

func TestSort(t *testing.T) {
	cases := []struct {
		key  string
		desc bool
		want string
	}{
		{SortTime, false, "adbc"}, {SortTime, true, "cbda"}, {"bogus", false, "adbc"},
		{SortSubject, false, "bcad"}, {SortSender, false, "acdb"}, {SortSender, true, "bdca"},
		{SortStatus, false, "adcb"}, {SortFolder, true, "bcda"},
	}
	for _, c := range cases {
		rs := reports()
		SortReports(rs, c.key, c.desc)
		if got := ids(rs); got != c.want {
			t.Errorf("sort %s desc=%v: got %q, want %q", c.key, c.desc, got, c.want)
		}
	}
}

func TestParseDay(t *testing.T) {
	if got := ParseDay(" 2026-05-01 "); got.IsZero() || got.Day() != 1 {
		t.Errorf("ParseDay = %v", got)
	}
	if !ParseDay("01/05/2026").IsZero() || !ParseDay("").IsZero() {
		t.Error("invalid day accepted")
	}
}

func TestPartyOverdueAndAssigneeFilter(t *testing.T) {
	group := User{ID: "g1", Role: RoleGroup}
	member := User{ID: "p1", Role: RoleGroup, GroupID: "g1"}
	leader := User{ID: "l1", Role: RoleLeader}
	admin := User{ID: "a1", Role: RoleAdmin}
	if group.Party() != "g1" || member.Party() != "g1" || leader.Party() != ReceiverLeader || admin.Party() != "" {
		t.Error("Party")
	}
	if group.Personal() || !member.Personal() || !leader.Personal() || !admin.Personal() {
		t.Error("Personal")
	}
	if ChatParty(&member) != "g1" {
		t.Error("ChatParty follows Party")
	}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	q := Request{Due: now}
	if q.Overdue(now) || !q.Overdue(now.Add(time.Second)) || (&Request{}).Overdue(now) {
		t.Error("Overdue")
	}
	q.Closed = true
	if q.Overdue(now.Add(time.Hour)) {
		t.Error("closed request overdue")
	}
	rs := []Report{{ID: "a", AssigneeID: "l1", AuthorName: "Anna Åkesson"}, {ID: "b"}, {ID: "c", AssigneeID: "l2"}}
	match := func(f ReportFilter) string {
		f.Prepare()
		out := ""
		for i := range rs {
			if f.Match(&rs[i]) {
				out += rs[i].ID
			}
		}
		return out
	}
	if got := match(ReportFilter{AssigneeID: "l1"}); got != "a" {
		t.Errorf("assignee l1 = %q", got)
	}
	if got := match(ReportFilter{AssigneeID: AssigneeNone}); got != "b" {
		t.Errorf("unassigned = %q", got)
	}
	if got := match(ReportFilter{Query: "ÅKESSON"}); got != "a" {
		t.Errorf("author search = %q", got)
	}
	if got := match(ReportFilter{ViewerID: "x", UnseenOnly: true}); got != "abc" {
		t.Errorf("viewer fields must not affect Match: %q", got)
	}
	if body, err := CleanChat(" hi\r\nthere "); err != nil || body != "hi\nthere" {
		t.Errorf("CleanChat = %q, %v", body, err)
	}
	if _, err := CleanChat("  "); err == nil {
		t.Error("empty chat message accepted")
	}
	if _, err := CleanChat(strings.Repeat("x", MaxChatLen+1)); err == nil {
		t.Error("oversized chat message accepted")
	}
	if conv, a, b := ChatConv("g2", "g1"); conv != "g1|g2" || a != "g1" || b != "g2" {
		t.Errorf("ChatConv = %q %q %q", conv, a, b)
	}
	if conv, a, b := ChatConv("g1", ChatAll); conv != ChatAll || a != "" || b != "" {
		t.Errorf("ChatConv all = %q %q %q", conv, a, b)
	}
}
