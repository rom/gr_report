package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rom/gr_report/internal/model"
)

var t0 = time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seed(t *testing.T, s *Store) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.PutUser(model.User{ID: "u1", Username: "admin", DisplayName: "Admin", Role: model.RoleAdmin}))
	must(s.PutUser(model.User{ID: "g2", Username: "bravo", DisplayName: "bravo", Role: model.RoleGroup}))
	must(s.PutUser(model.User{ID: "g1", Username: "alpha", DisplayName: "Alpha", Role: model.RoleGroup}))
	must(s.PutUser(model.User{ID: "g3", Username: "gone", DisplayName: "Alpha", Role: model.RoleGroup, Disabled: true}))
	must(s.PutTemplate(model.Template{ID: "t2", Name: "sitrep", Fields: []model.Field{{Key: "k", Label: "K", Type: model.FieldSelect, Options: []string{"a"}}}}))
	must(s.PutTemplate(model.Template{ID: "t1", Name: "Minimal"}))
	must(s.PutTemplate(model.Template{ID: "t3", Name: "Minimal", Archived: true}))
	must(s.PutRequest(model.Request{ID: "q1", Subject: "first", TemplateID: "t1", Created: t0, Recipients: []string{"g1"}}))
	must(s.PutRequest(model.Request{ID: "q2", Subject: "second", TemplateID: "t1", Created: t0.Add(time.Hour)}))
	must(s.PutRequest(model.Request{ID: "q0", Subject: "same time", TemplateID: "t1", Created: t0}))
	must(s.PutReport(model.Report{ID: "r1", SenderID: "g1", ReceiverID: "leader", Status: model.StatusNew, Subject: "one", Fields: []model.FieldValue{{Key: "k", Value: "v"}}}))
	must(s.PutReport(model.Report{ID: "r2", SenderID: "g1", ReceiverID: "leader", Status: model.StatusFiled, Folder: "B", Subject: "two"}))
	must(s.PutReport(model.Report{ID: "r3", SenderID: "g2", ReceiverID: "leader", Status: model.StatusNew, Folder: "A", Subject: "three"}))
	must(s.PutReport(model.Report{ID: "r4", SenderID: "g2", ReceiverID: "g1", Status: model.StatusNew, Subject: "four"}))
}

func TestCRUDAndOrdering(t *testing.T) {
	s := open(t, t.TempDir())
	seed(t, s)

	if u, ok := s.UserByUsername("ALPHA"); !ok || u.ID != "g1" {
		t.Errorf("UserByUsername = %+v, %v", u, ok)
	}
	if _, ok := s.UserByUsername("nobody"); ok {
		t.Error("unknown user found")
	}
	if u, ok := s.User("u1"); !ok || u.Username != "admin" {
		t.Error("User(u1)")
	}
	names := ""
	for _, u := range s.Users() {
		names += u.ID + " "
	}
	if names != "u1 g1 g3 g2 " {
		t.Errorf("Users order = %q", names)
	}
	if g := s.Groups(); len(g) != 2 || g[0].ID != "g1" || g[1].ID != "g2" {
		t.Errorf("Groups = %+v", g)
	}

	if err := s.PutUser(model.User{ID: "u9", Username: "alpha"}); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate username: %v", err)
	}
	// Renaming frees the old username.
	if err := s.PutUser(model.User{ID: "g1", Username: "alfa", DisplayName: "Alpha", Role: model.RoleGroup}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.UserByUsername("alpha"); ok {
		t.Error("old username still resolves")
	}
	if err := s.PutUser(model.User{ID: "u9", Username: "alpha", Role: model.RoleGroup}); err != nil {
		t.Errorf("freed username not reusable: %v", err)
	}

	if ts := s.Templates(false); len(ts) != 2 || ts[0].ID != "t1" || ts[1].ID != "t2" {
		t.Errorf("Templates(false) = %+v", ts)
	}
	if ts := s.Templates(true); len(ts) != 3 || ts[1].ID != "t3" {
		t.Errorf("Templates(true) = %+v", ts)
	}
	tp, ok := s.Template("t2")
	if !ok {
		t.Fatal("Template(t2)")
	}
	tp.Fields[0].Options[0] = "mutated"
	if again, _ := s.Template("t2"); again.Fields[0].Options[0] != "a" {
		t.Error("store state reachable through returned template")
	}

	if qs := s.Requests(); len(qs) != 3 || qs[0].ID != "q2" || qs[1].ID != "q0" || qs[2].ID != "q1" {
		t.Errorf("Requests order = %+v", qs)
	}
	if q, ok := s.Request("q1"); !ok || q.Subject != "first" {
		t.Error("Request(q1)")
	}

	if rs := s.Reports(nil); len(rs) != 4 {
		t.Errorf("Reports(nil) = %d", len(rs))
	}
	if rs := s.Reports(&model.ReportFilter{ReceiverID: "leader", Query: "t"}); len(rs) != 2 {
		t.Errorf("filtered = %d", len(rs))
	}
	if r, ok := s.Report("r1"); !ok || r.Fields[0].Value != "v" {
		t.Error("Report(r1)")
	}
	st := s.Stats("leader")
	if st.Total["g1"] != 2 || st.New["g1"] != 1 || st.New["g2"] != 1 || strings.Join(st.Folders, ",") != "A,B" {
		t.Errorf("Stats = %+v", st)
	}

	for _, err := range []error{
		s.PutUser(model.User{ID: "bad id", Username: "x"}), s.PutUser(model.User{ID: "ok"}),
		s.PutTemplate(model.Template{ID: ""}), s.PutRequest(model.Request{ID: "../x"}), s.PutReport(model.Report{ID: "X"}),
	} {
		if err == nil {
			t.Error("invalid entity accepted")
		}
	}
}

func TestUpdateReport(t *testing.T) {
	s := open(t, t.TempDir())
	seed(t, s)
	r, err := s.UpdateReport("r1", func(r *model.Report) bool {
		r.Status, r.Folder, r.ID = model.StatusFiled, "X", "hijack"
		return true
	})
	if err != nil || r.ID != "r1" || r.Status != model.StatusFiled {
		t.Fatalf("UpdateReport = %+v, %v", r, err)
	}
	r, err = s.UpdateReport("r1", func(r *model.Report) bool {
		r.Folder = "discarded"
		return false
	})
	if err != nil || r.Folder != "X" {
		t.Errorf("declined update changed state: %+v, %v", r, err)
	}
	if _, err := s.UpdateReport("nope", func(*model.Report) bool { return true }); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing report: %v", err)
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	seed(t, s)
	if _, err := s.UpdateReport("r1", func(r *model.Report) bool { r.Folder = "kept"; return true }); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if err := s.PutTemplate(model.Template{ID: "t9"}); !errors.Is(err, ErrClosed) {
		t.Errorf("write after close: %v", err)
	}
	if _, err := s.UpdateReport("r1", func(*model.Report) bool { return true }); !errors.Is(err, ErrClosed) {
		t.Errorf("update after close: %v", err)
	}

	s2 := open(t, dir)
	if fi, err := os.Stat(filepath.Join(dir, DBName)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("database file: %v %v", fi, err)
	}
	if len(s2.Users()) != 4 || len(s2.Templates(true)) != 3 || len(s2.Requests()) != 3 || len(s2.Reports(nil)) != 4 {
		t.Error("state lost on reopen")
	}
	r, _ := s2.Report("r1")
	if r.Folder != "kept" || len(r.Fields) != 1 || r.Fields[0].Value != "v" {
		t.Errorf("report after reopen = %+v", r)
	}
	tp, _ := s2.Template("t2")
	if len(tp.Fields) != 1 || tp.Fields[0].Options[0] != "a" {
		t.Errorf("template after reopen = %+v", tp)
	}
	q, _ := s2.Request("q1")
	if len(q.Recipients) != 1 || !q.Created.Equal(t0) || q.Created.Location() != time.UTC {
		t.Errorf("request after reopen = %+v", q)
	}
	if u, ok := s2.UserByUsername("bravo"); !ok || u.ID != "g2" {
		t.Error("user lost")
	}
}

func TestLock(t *testing.T) {
	dir := t.TempDir()
	open(t, dir)
	if _, err := Open(dir); !errors.Is(err, ErrLocked) {
		t.Errorf("second Open: %v", err)
	}
}

func TestOpenErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(file); err == nil {
		t.Error("Open on a regular file succeeded")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, DBName), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Error("Open with a directory as database succeeded")
	}
	// A file that is not a database must not be silently replaced.
	dir = t.TempDir()
	garbage := strings.Repeat("this is not a database ", 100)
	if err := os.WriteFile(filepath.Join(dir, DBName), []byte(garbage), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Error("garbage database accepted")
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, DBName)); string(raw) != garbage {
		t.Error("garbage database was modified")
	}
	// The directory lock is released when Open fails.
	if err := os.Remove(filepath.Join(dir, DBName)); err != nil {
		t.Fatal(err)
	}
	open(t, dir)
}

func TestSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != len(migrations) {
		t.Fatalf("user_version = %d, %v", v, err)
	}
	var mode, sync string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q, %v", mode, err)
	}
	if err := s.db.QueryRow("PRAGMA synchronous").Scan(&sync); err != nil || sync != "2" {
		t.Errorf("synchronous = %q (want 2 = FULL), %v", sync, err)
	}
	// A database written by a newer version is refused, not downgraded.
	if _, err := s.db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Errorf("newer schema: %v", err)
	}
	// A broken migration rolls back completely.
	dir2 := t.TempDir()
	saved := migrations
	migrations = append(append([]string{}, saved...), "CREATE TABLE extra (id TEXT); THIS IS NOT SQL;")
	_, err := Open(dir2)
	migrations = saved
	if err == nil {
		t.Fatal("broken migration accepted")
	}
	s2 := open(t, dir2)
	var n int
	if err := s2.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = 'extra'").Scan(&n); err != nil || n != 0 {
		t.Errorf("failed migration left a table behind: %d, %v", n, err)
	}
}

func TestSearch(t *testing.T) {
	s := open(t, t.TempDir())
	put := func(id, subject, content, folder string, fields ...string) {
		t.Helper()
		r := model.Report{ID: id, Subject: subject, Content: content, Folder: folder, SenderName: "Grupp Örnen",
			TemplateName: "Lägesrapport", ReceiverID: "leader", Status: model.StatusNew, Timestamp: t0}
		for _, f := range fields {
			r.Fields = append(r.Fields, model.FieldValue{Key: "k", Label: "L", Value: f})
		}
		if err := s.PutReport(r); err != nil {
			t.Fatal(err)
		}
	}
	put("a", "Bron är AVSTÄNGD", "Behöver bränsle", "", "Ruta 7")
	put("b", "100% klart", "under_score och back\\slash", "Logistik")
	put("c", "Ankomst", "allt väl", "")
	cases := map[string]string{
		"avstängd": "a", "BRÄNSLE bron": "a", "ruta": "a", "örnen": "abc", "lägesrapport": "abc", "logistik": "b",
		"100%": "b", "%": "b", "_": "b", "under_score": "b", `back\slash`: "b", "x%": "", "a_k": "",
		"'; DROP TABLE reports; --": "", "nomatch": "", "": "abc",
	}
	for q, want := range cases {
		got := ""
		rs := s.Reports(&model.ReportFilter{Query: q})
		model.SortReports(rs, model.SortTime, false)
		for _, r := range rs {
			got += r.ID
		}
		if got != want {
			t.Errorf("search %q = %q, want %q", q, got, want)
		}
	}
	// Filing changes what the folder search finds.
	if _, err := s.UpdateReport("c", func(r *model.Report) bool { r.Folder = "Ärenden"; return true }); err != nil {
		t.Fatal(err)
	}
	if rs := s.Reports(&model.ReportFilter{Query: "ärenden"}); len(rs) != 1 || rs[0].ID != "c" {
		t.Errorf("folder not searchable after update: %+v", rs)
	}
	from, to := t0.Add(-time.Hour), t0.Add(time.Hour)
	if rs := s.Reports(&model.ReportFilter{From: from, To: to, Status: model.StatusNew, Folder: "Logistik"}); len(rs) != 1 {
		t.Errorf("combined filter = %d", len(rs))
	}
	if rs := s.Reports(&model.ReportFilter{From: to}); len(rs) != 0 {
		t.Errorf("From filter = %d", len(rs))
	}
	if rs := s.Reports(&model.ReportFilter{To: from}); len(rs) != 0 {
		t.Errorf("To filter = %d", len(rs))
	}
	if n := s.NewCount("leader"); n != 3 {
		t.Errorf("NewCount = %d", n)
	}
}

func TestReadFailurePanics(t *testing.T) {
	s := open(t, t.TempDir())
	seed(t, s)
	s.Close()
	reads := map[string]func(){
		"User": func() { s.User("u1") }, "UserByUsername": func() { s.UserByUsername("a") }, "Users": func() { s.Users() },
		"Template": func() { s.Template("t1") }, "Templates": func() { s.Templates(true) },
		"Request": func() { s.Request("q1") }, "Requests": func() { s.Requests() },
		"Report": func() { s.Report("r1") }, "Reports": func() { s.Reports(nil) },
		"Stats": func() { s.Stats("leader") }, "NewCount": func() { s.NewCount("leader") },
		"ChatMessages": func() { s.ChatMessages("all", 0, 0, 1) }, "ChatUnread": func() { s.ChatUnread("g1") },
	}
	for name, fn := range reads {
		func() {
			defer func() {
				f, ok := recover().(*Failure)
				if !ok || !errors.Is(f, ErrClosed) || !strings.Contains(f.Error(), "closed") {
					t.Errorf("%s on a closed store: recovered %v", name, f)
				}
			}()
			fn()
		}()
	}
	for _, err := range []error{s.PutUser(model.User{ID: "u7", Username: "new"}), s.PutTemplate(model.Template{ID: "t7"}),
		s.PutRequest(model.Request{ID: "q7"}), s.PutReport(model.Report{ID: "r7"})} {
		if !errors.Is(err, ErrClosed) {
			t.Errorf("write on a closed store: %v", err)
		}
	}
}

func TestCorruptRowsFailLoudly(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	seed(t, s)
	for _, q := range []string{
		"UPDATE reports SET fields = 'not json' WHERE id = 'r1'",
		"UPDATE templates SET fields = '{' WHERE id = 't1'",
		"UPDATE requests SET recipients = '7' WHERE id = 'q1'",
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for name, fn := range map[string]func(){
		"Report": func() { s.Report("r1") }, "Reports": func() { s.Reports(nil) },
		"Template": func() { s.Template("t1") }, "Templates": func() { s.Templates(true) },
		"Request": func() { s.Request("q1") }, "Requests": func() { s.Requests() },
	} {
		func() {
			defer func() {
				if _, ok := recover().(*Failure); !ok {
					t.Errorf("%s returned damaged data without failing", name)
				}
			}()
			fn()
		}()
	}
	if _, err := s.UpdateReport("r1", func(*model.Report) bool { return true }); err == nil {
		t.Error("UpdateReport on a damaged row succeeded")
	}
}

func TestChat(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	send := func(party, name, peer, body string) model.ChatMessage {
		t.Helper()
		conv, a, b := model.ChatConv(party, peer)
		m, err := s.AddChatMessage(model.ChatMessage{Conv: conv, Timestamp: t0, SenderParty: party, SenderName: name, Body: body}, a, b)
		if err != nil || m.Seq == 0 {
			t.Fatalf("AddChatMessage: %+v, %v", m, err)
		}
		return m
	}
	m1 := send("g1", "Alpha", model.ChatAll, "hello all")
	m2 := send("leader", "Leader", model.ChatAll, "welcome")
	send("g1", "Alpha", "g2", "psst bravo")
	m4 := send("g2", "Bravo", "g1", "yes?")
	send("g2", "Bravo", "leader", "question")
	if m2.Seq <= m1.Seq {
		t.Error("sequence numbers do not increase")
	}
	// Both sides of a direct conversation use the same key.
	c12, _, _ := model.ChatConv("g1", "g2")
	c21, _, _ := model.ChatConv("g2", "g1")
	cl2, _, _ := model.ChatConv("leader", "g2")
	if c12 != c21 || c12 == cl2 {
		t.Fatalf("conversation keys: %q %q %q", c12, c21, cl2)
	}

	all := s.ChatMessages(model.ChatAll, 0, 0, 10)
	if len(all) != 2 || all[0].Body != "hello all" || all[1].Body != "welcome" || !all[0].Timestamp.Equal(t0) || all[1].SenderName != "Leader" {
		t.Errorf("all = %+v", all)
	}
	if got := s.ChatMessages(c12, 0, 0, 10); len(got) != 2 || got[0].Body != "psst bravo" || got[1].SenderParty != "g2" {
		t.Errorf("direct = %+v", got)
	}
	if got := s.ChatMessages(model.ChatAll, 0, 0, 1); len(got) != 1 || got[0].Seq != m2.Seq {
		t.Errorf("limit keeps the newest: %+v", got)
	}
	if got := s.ChatMessages(model.ChatAll, m1.Seq, 0, 10); len(got) != 1 || got[0].Seq != m2.Seq {
		t.Errorf("after = %+v", got)
	}
	if got := s.ChatMessages(model.ChatAll, 0, m2.Seq, 10); len(got) != 1 || got[0].Seq != m1.Seq {
		t.Errorf("before = %+v", got)
	}
	if got := s.ChatMessages("nobody|here", 0, 0, 10); len(got) != 0 {
		t.Errorf("unknown conversation = %+v", got)
	}

	// Unread: only messages from others, only in the party's conversations.
	want := func(party string, exp map[string]int) {
		t.Helper()
		got := s.ChatUnread(party)
		if len(got) != len(exp) {
			t.Errorf("unread %s = %v, want %v", party, got, exp)
		}
		for k, v := range exp {
			if got[k] != v {
				t.Errorf("unread %s[%s] = %d, want %d", party, k, got[k], v)
			}
		}
	}
	want("g1", map[string]int{model.ChatAll: 1, c12: 1})
	want("g2", map[string]int{model.ChatAll: 2, c12: 1})
	want("leader", map[string]int{model.ChatAll: 1, cl2: 1})
	want("g3", map[string]int{model.ChatAll: 2})

	if err := s.MarkChatRead("g1", c12, m4.Seq); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkChatRead("g1", model.ChatAll, m2.Seq); err != nil {
		t.Fatal(err)
	}
	want("g1", map[string]int{})
	want("g2", map[string]int{model.ChatAll: 2, c12: 1}) // the other side is unaffected
	// The marker never moves backwards.
	if err := s.MarkChatRead("g1", model.ChatAll, 1); err != nil {
		t.Fatal(err)
	}
	want("g1", map[string]int{})
	send("leader", "Leader", model.ChatAll, "new one")
	want("g1", map[string]int{model.ChatAll: 1})

	for _, bad := range []model.ChatMessage{{}, {Conv: "all", Body: "x"}, {Conv: "all", SenderParty: "g1"}} {
		if _, err := s.AddChatMessage(bad, "", ""); err == nil {
			t.Errorf("invalid message %+v accepted", bad)
		}
	}
	// Messages and read markers survive a restart.
	s.Close()
	s2 := open(t, dir)
	if got := s2.ChatMessages(model.ChatAll, 0, 0, 10); len(got) != 3 || got[2].Body != "new one" {
		t.Errorf("after reopen: %+v", got)
	}
	if got := s2.ChatUnread("g1"); got[model.ChatAll] != 1 || len(got) != 1 {
		t.Errorf("unread after reopen: %v", got)
	}
	s2.Close()
	if _, err := s2.AddChatMessage(model.ChatMessage{Conv: "all", SenderParty: "g1", Body: "x"}, "", ""); !errors.Is(err, ErrClosed) {
		t.Errorf("write on closed store: %v", err)
	}
	if err := s2.MarkChatRead("g1", "all", 1); !errors.Is(err, ErrClosed) {
		t.Errorf("mark on closed store: %v", err)
	}
}

// A database created before the chat existed is upgraded in place.
func TestMigrationFromVersion1(t *testing.T) {
	dir := t.TempDir()
	saved := migrations
	migrations = saved[:1]
	s, err := Open(dir)
	migrations = saved
	if err != nil {
		t.Fatal(err)
	}
	seed(t, s)
	s.Close()
	s2 := open(t, dir)
	if len(s2.Reports(nil)) != 4 || len(s2.Users()) != 4 {
		t.Error("data lost in the upgrade")
	}
	conv, a, b := model.ChatConv("g1", "g2")
	if _, err := s2.AddChatMessage(model.ChatMessage{Conv: conv, Timestamp: t0, SenderParty: "g1", SenderName: "A", Body: "hi"}, a, b); err != nil {
		t.Errorf("chat unusable after upgrade: %v", err)
	}
}

// FuzzReportRoundTrip stores arbitrary text and searches with arbitrary
// queries. Whatever goes in must come out byte for byte, a report must be
// found by its own words, and no input may be interpreted as SQL.
func FuzzReportRoundTrip(f *testing.F) {
	f.Add("subject", "content", "folder", "value", "subject")
	f.Add("Bron är AVSTÄNGD", "100% _ \\ '", "x'); DROP TABLE reports;--", "\"", "%")
	f.Add("a\x00b", "\xff", "", "", "_")
	dir := f.TempDir()
	s, err := Open(dir)
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { s.Close() })
	f.Fuzz(func(t *testing.T, subject, content, folder, value, query string) {
		in := model.Report{ID: "fuzz", Timestamp: t0, SenderID: "g1", SenderName: "Alpha", ReceiverID: "leader", Status: model.StatusNew,
			Subject: model.CleanLine(subject), Content: model.CleanText(content), Folder: model.CleanLine(folder),
			Fields: []model.FieldValue{{Key: "k", Label: "L", Value: model.CleanText(value)}}}
		if err := s.PutReport(in); err != nil {
			t.Fatal(err)
		}
		out, ok := s.Report("fuzz")
		if !ok || out.Subject != in.Subject || out.Content != in.Content || out.Folder != in.Folder ||
			out.Fields[0].Value != in.Fields[0].Value || !out.Timestamp.Equal(in.Timestamp) {
			t.Fatalf("round trip changed the report:\n in  %+v\n out %+v", in, out)
		}
		flt := model.ReportFilter{Query: query}
		got := s.Reports(&flt)
		// The SQL search must agree with the reference implementation.
		ref := model.ReportFilter{Query: query}
		ref.Prepare()
		if want := ref.Match(&in); want != (len(got) == 1) {
			t.Fatalf("query %q: SQL found %d, reference says match=%v (report %+v)", query, len(got), want, in)
		}
		var n int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM reports").Scan(&n); err != nil || n != 1 {
			t.Fatalf("table damaged: %d rows, %v", n, err)
		}
	})
}
