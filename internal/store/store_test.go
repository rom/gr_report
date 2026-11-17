package store

import (
	"errors"
	"fmt"
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
	st := s.Stats("leader", "viewer")
	// Nothing has been opened by this viewer, whatever the shared status.
	if st.Total["g1"] != 2 || st.New["g1"] != 2 || st.New["g2"] != 1 || strings.Join(st.Folders, ",") != "A,B" {
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
	if n := s.NewCount("leader", "viewer"); n != 3 {
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
		"Stats": func() { s.Stats("leader", "v") }, "NewCount": func() { s.NewCount("leader", "v") },
		"Draft": func() { s.Draft("u", "t", "") }, "Drafts": func() { s.Drafts("u") }, "SavedFilters": func() { s.SavedFilters("u") },
		"Audit": func() { s.Audit("", 0, 10) }, "Counts": func() { s.Counts() },
		"ChatMessages": func() { s.ChatMessages("all", 0, 0, 1, false) }, "ChatUnread": func() { s.ChatUnread("g1") },
		"ChatOverseen": func() { s.ChatOverseen() }, "Setting": func() { s.Setting("k") },
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

	all := s.ChatMessages(model.ChatAll, 0, 0, 10, false)
	if len(all) != 2 || all[0].Body != "hello all" || all[1].Body != "welcome" || !all[0].Timestamp.Equal(t0) || all[1].SenderName != "Leader" {
		t.Errorf("all = %+v", all)
	}
	if got := s.ChatMessages(c12, 0, 0, 10, false); len(got) != 2 || got[0].Body != "psst bravo" || got[1].SenderParty != "g2" {
		t.Errorf("direct = %+v", got)
	}
	if got := s.ChatMessages(model.ChatAll, 0, 0, 1, false); len(got) != 1 || got[0].Seq != m2.Seq {
		t.Errorf("limit keeps the newest: %+v", got)
	}
	if got := s.ChatMessages(model.ChatAll, m1.Seq, 0, 10, false); len(got) != 1 || got[0].Seq != m2.Seq {
		t.Errorf("after = %+v", got)
	}
	if got := s.ChatMessages(model.ChatAll, 0, m2.Seq, 10, false); len(got) != 1 || got[0].Seq != m1.Seq {
		t.Errorf("before = %+v", got)
	}
	if got := s.ChatMessages("nobody|here", 0, 0, 10, false); len(got) != 0 {
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

	// Oversight: the flag is stored per message and selects what a leader
	// may see; messages sent before it was switched on stay private.
	if got := s.ChatOverseen(); len(got) != 0 {
		t.Errorf("overseen conversations before any: %+v", got)
	}
	if _, err := s.AddChatMessage(model.ChatMessage{Conv: c12, Timestamp: t0, SenderParty: "g1", SenderName: "Alpha", Body: "watched", Overseen: true}, "g1", "g2"); err != nil {
		t.Fatal(err)
	}
	if got := s.ChatMessages(c12, 0, 0, 10, true); len(got) != 1 || got[0].Body != "watched" || !got[0].Overseen {
		t.Errorf("overseen only = %+v", got)
	}
	if got := s.ChatMessages(c12, 0, 0, 10, false); len(got) != 3 || got[0].Overseen || !got[2].Overseen {
		t.Errorf("all with flags = %+v", got)
	}
	if got := s.ChatOverseen(); len(got) != 1 || got[0].A != "g1" || got[0].B != "g2" || got[0].Count != 1 {
		t.Errorf("ChatOverseen = %+v", got)
	}

	for _, bad := range []model.ChatMessage{{}, {Conv: "all", Body: "x"}, {Conv: "all", SenderParty: "g1"}} {
		if _, err := s.AddChatMessage(bad, "", ""); err == nil {
			t.Errorf("invalid message %+v accepted", bad)
		}
	}
	// Messages and read markers survive a restart.
	s.Close()
	s2 := open(t, dir)
	if got := s2.ChatMessages(model.ChatAll, 0, 0, 10, false); len(got) != 3 || got[2].Body != "new one" {
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

func TestSettings(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	if _, ok := s.Setting("chat_oversight"); ok {
		t.Error("unset setting found")
	}
	if err := s.PutSetting("chat_oversight", Setting{Value: "on", UpdatedBy: "leader", UpdatedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSetting("chat_oversight", Setting{Value: "off", UpdatedBy: "leader2", UpdatedAt: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSetting("", Setting{Value: "x"}); err == nil {
		t.Error("empty key accepted")
	}
	s.Close()
	s2 := open(t, dir)
	v, ok := s2.Setting("chat_oversight")
	if !ok || v.Value != "off" || v.UpdatedBy != "leader2" || !v.UpdatedAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("setting after reopen = %+v, %v", v, ok)
	}
	s2.Close()
	if err := s2.PutSetting("k", Setting{Value: "v"}); !errors.Is(err, ErrClosed) {
		t.Errorf("write on closed store: %v", err)
	}
}

// A database created by an earlier version is upgraded in place, and chat
// messages from before oversight existed are not exposed by the upgrade.
func TestMigrationFromEarlierVersion(t *testing.T) {
	dir := t.TempDir()
	saved := migrations
	migrations = saved[:2]
	s, err := Open(dir)
	migrations = saved
	if err != nil {
		t.Fatal(err)
	}
	// Rows as the old schema stored them.
	for _, q := range []string{
		`INSERT INTO users (id, username, display_name, role, pass_hash, created) VALUES ('g1','alpha','Alpha','group','x',1), ('g2','bravo','Bravo','group','x',1)`,
		`INSERT INTO reports (id, ts, sender_id, sender_name, subject, content, receiver_id, receiver_name, template_id, template_name, fields, request_id, status, folder, search_text)
			VALUES ('r1',1,'g1','A','one','c','leader','L','','','null','','new','','x')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	// A chat message written before the oversight column existed.
	if _, err := s.db.Exec(`INSERT INTO chat_messages (conv, party_a, party_b, ts, sender_party, sender_name, body) VALUES ('g1|g2','g1','g2',1,'g1','A','old and private')`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2 := open(t, dir)
	if len(s2.Reports(nil)) != 1 || len(s2.Users()) != 2 {
		t.Error("data lost in the upgrade")
	}
	conv, a, b := model.ChatConv("g1", "g2")
	if _, err := s2.AddChatMessage(model.ChatMessage{Conv: conv, Timestamp: t0, SenderParty: "g1", SenderName: "A", Body: "hi"}, a, b); err != nil {
		t.Errorf("chat unusable after upgrade: %v", err)
	}
	if got := s2.ChatMessages(conv, 0, 0, 10, true); len(got) != 0 {
		t.Errorf("messages from before the upgrade became overseen: %+v", got)
	}
	if got := s2.ChatMessages(conv, 0, 0, 10, false); len(got) != 2 || got[0].Body != "old and private" {
		t.Errorf("history after upgrade: %+v", got)
	}
	// A database from before run-time settings existed gains the table.
	dir3 := t.TempDir()
	migrations = saved[:3]
	s4, err := Open(dir3)
	migrations = saved
	if err != nil {
		t.Fatal(err)
	}
	s4.Close()
	if err := open(t, dir3).PutSetting("k", Setting{Value: "v"}); err != nil {
		t.Errorf("settings unusable after upgrade: %v", err)
	}
	// Opening with only the first migration applied also works.
	dir1 := t.TempDir()
	migrations = saved[:1]
	s3, err := Open(dir1)
	migrations = saved
	if err != nil {
		t.Fatal(err)
	}
	s3.Close()
	open(t, dir1)
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

func TestSeenAndAssignment(t *testing.T) {
	s := open(t, t.TempDir())
	seed(t, s)
	ids := func(f model.ReportFilter) string {
		rs := s.Reports(&f)
		model.SortReports(rs, model.SortSubject, false)
		out := ""
		for _, r := range rs {
			out += r.ID
			if r.Unseen {
				out += "*"
			}
			out += " "
		}
		return out
	}
	// Nothing has been opened: everything is unseen for a viewer, and
	// Unseen is only reported when a viewer is named.
	if got := ids(model.ReportFilter{ReceiverID: "leader", ViewerID: "l1"}); got != "r1* r3* r2* " {
		t.Errorf("fresh queue = %q", got)
	}
	if got := ids(model.ReportFilter{ReceiverID: "leader"}); got != "r1 r3 r2 " {
		t.Errorf("without viewer = %q", got)
	}
	// Read state is per user.
	for _, id := range []string{"r1", "r1", "r3"} {
		if err := s.MarkSeen("l1", id, true); err != nil {
			t.Fatal(err)
		}
	}
	if got := ids(model.ReportFilter{ReceiverID: "leader", ViewerID: "l1"}); got != "r1 r3 r2* " {
		t.Errorf("after l1 read = %q", got)
	}
	if got := ids(model.ReportFilter{ReceiverID: "leader", ViewerID: "l2"}); got != "r1* r3* r2* " {
		t.Errorf("l2 unaffected = %q", got)
	}
	if got := ids(model.ReportFilter{ReceiverID: "leader", ViewerID: "l1", UnseenOnly: true}); got != "r2* " {
		t.Errorf("unseen only = %q", got)
	}
	if n1, n2 := s.NewCount("leader", "l1"), s.NewCount("leader", "l2"); n1 != 1 || n2 != 3 {
		t.Errorf("NewCount l1=%d l2=%d", n1, n2)
	}
	st := s.Stats("leader", "l1")
	if st.Total["g1"] != 2 || st.New["g1"] != 1 || st.New["g2"] != 0 || st.Total["g2"] != 1 {
		t.Errorf("Stats = %+v", st)
	}
	if err := s.MarkSeen("l1", "r1", false); err != nil {
		t.Fatal(err)
	}
	if n := s.NewCount("leader", "l1"); n != 2 {
		t.Errorf("after marking unread: %d", n)
	}

	// Assignment and author travel with the report and can be filtered.
	if _, err := s.UpdateReport("r1", func(r *model.Report) bool { r.AssigneeID, r.AssigneeName = "l1", "Leader One"; return true }); err != nil {
		t.Fatal(err)
	}
	if err := s.PutReport(model.Report{ID: "r9", SenderID: "g1", ReceiverID: "leader", Subject: "nine", AuthorID: "p1", AuthorName: "Anna Åkesson", Status: model.StatusNew}); err != nil {
		t.Fatal(err)
	}
	if got := ids(model.ReportFilter{AssigneeID: "l1"}); got != "r1 " {
		t.Errorf("assigned to l1 = %q", got)
	}
	if got := ids(model.ReportFilter{ReceiverID: "leader", AssigneeID: model.AssigneeNone}); got != "r9 r3 r2 " {
		t.Errorf("unassigned = %q", got)
	}
	if got := ids(model.ReportFilter{Query: "åkesson"}); got != "r9 " {
		t.Errorf("search by author = %q", got)
	}
	r, _ := s.Report("r9")
	if r.AuthorID != "p1" || r.AuthorName != "Anna Åkesson" || r.Unseen {
		t.Errorf("report = %+v", r)
	}
	if r1, _ := s.Report("r1"); r1.AssigneeName != "Leader One" {
		t.Errorf("assignee lost: %+v", r1)
	}
	s.Close()
	if err := s.MarkSeen("l1", "r1", true); !errors.Is(err, ErrClosed) {
		t.Errorf("MarkSeen on closed store: %v", err)
	}
}

func TestGroupsAndMembers(t *testing.T) {
	s := open(t, t.TempDir())
	seed(t, s)
	for _, u := range []model.User{
		{ID: "p1", Username: "anna", DisplayName: "Anna", Role: model.RoleGroup, GroupID: "g1"},
		{ID: "p2", Username: "bo", DisplayName: "Bo", Role: model.RoleGroup, GroupID: "g1"},
	} {
		if err := s.PutUser(u); err != nil {
			t.Fatal(err)
		}
	}
	// Members are not groups of their own.
	if g := s.Groups(); len(g) != 2 || g[0].ID != "g1" || g[1].ID != "g2" {
		t.Errorf("Groups = %+v", g)
	}
	if m := s.Members("g1"); len(m) != 2 || m[0].ID != "p1" || m[1].GroupID != "g1" {
		t.Errorf("Members(g1) = %+v", m)
	}
	if m := s.Members("g2"); len(m) != 0 {
		t.Errorf("Members(g2) = %+v", m)
	}
	if m := s.Members(""); len(m) != 0 {
		t.Errorf("Members(\"\") = %+v", m)
	}
	anna, _ := s.UserByUsername("anna")
	group, _ := s.User("g1")
	leader := model.User{ID: "l1", Role: model.RoleLeader}
	admin, _ := s.User("u1")
	if anna.Party() != "g1" || group.Party() != "g1" || leader.Party() != model.ReceiverLeader || admin.Party() != "" {
		t.Error("Party")
	}
	if !anna.Personal() || group.Personal() || !leader.Personal() {
		t.Error("Personal")
	}
}

func TestRequestDue(t *testing.T) {
	s := open(t, t.TempDir())
	due := t0.Add(2 * time.Hour)
	if err := s.PutRequest(model.Request{ID: "q1", Subject: "s", TemplateID: "t1", Created: t0, Due: due}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRequest(model.Request{ID: "q2", Subject: "s", TemplateID: "t1", Created: t0}); err != nil {
		t.Fatal(err)
	}
	q1, _ := s.Request("q1")
	q2, _ := s.Request("q2")
	if !q1.Due.Equal(due) || !q2.Due.IsZero() {
		t.Errorf("due: %v %v", q1.Due, q2.Due)
	}
	if q1.Overdue(due) || !q1.Overdue(due.Add(time.Second)) || q2.Overdue(due.Add(time.Hour)) {
		t.Error("Overdue")
	}
	q1.Closed = true
	if q1.Overdue(due.Add(time.Hour)) {
		t.Error("a closed request is never overdue")
	}
}

func TestDrafts(t *testing.T) {
	s := open(t, t.TempDir())
	d, err := s.PutDraft(model.Draft{ID: "d1", UserID: "u1", TemplateID: "t1", Subject: "half", Content: "written",
		Values: map[string]string{"loc": "Åre"}, Receiver: "leader", Updated: t0})
	if err != nil || d.ID != "d1" {
		t.Fatal(d, err)
	}
	// Saving again for the same template and request replaces it.
	d, err = s.PutDraft(model.Draft{ID: "d2", UserID: "u1", TemplateID: "t1", Subject: "more", Updated: t0.Add(time.Minute)})
	if err != nil || d.ID != "d1" {
		t.Fatalf("replace: %+v, %v", d, err)
	}
	if _, err := s.PutDraft(model.Draft{ID: "d3", UserID: "u1", TemplateID: "t1", RequestID: "q1", Subject: "answer", Updated: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutDraft(model.Draft{ID: "d4", UserID: "u2", TemplateID: "t1", Subject: "other user", Updated: t0}); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Draft("u1", "t1", "")
	if !ok || got.Subject != "more" || got.Values != nil || !got.Updated.Equal(t0.Add(time.Minute)) {
		t.Errorf("Draft = %+v, %v", got, ok)
	}
	if _, ok := s.Draft("u1", "t2", ""); ok {
		t.Error("draft for another template found")
	}
	list := s.Drafts("u1")
	if len(list) != 2 || list[0].ID != "d3" || list[1].ID != "d1" {
		t.Errorf("Drafts = %+v", list)
	}
	// Only the owner can delete.
	if err := s.DeleteDraft("u2", "d1"); err != nil {
		t.Fatal(err)
	}
	if len(s.Drafts("u1")) != 2 {
		t.Error("another user deleted a draft")
	}
	if err := s.DeleteDraft("u1", "d1"); err != nil || len(s.Drafts("u1")) != 1 {
		t.Errorf("delete: %v", err)
	}
	for i := 0; i < model.MaxDrafts+2; i++ {
		_, err = s.PutDraft(model.Draft{ID: model.NewID(), UserID: "u9", TemplateID: fmt.Sprint("t", i), Updated: t0})
	}
	if !errors.Is(err, ErrTooMany) || len(s.Drafts("u9")) != model.MaxDrafts {
		t.Errorf("limit: %v, %d drafts", err, len(s.Drafts("u9")))
	}
	// At the limit an existing draft can still be saved.
	if _, err := s.PutDraft(model.Draft{ID: model.NewID(), UserID: "u9", TemplateID: "t0", Subject: "again", Updated: t0}); err != nil {
		t.Errorf("update at the limit: %v", err)
	}
	for _, bad := range []model.Draft{{ID: "bad id", UserID: "u", TemplateID: "t"}, {ID: "d", TemplateID: "t"}, {ID: "d", UserID: "u"}} {
		if _, err := s.PutDraft(bad); err == nil {
			t.Errorf("invalid draft %+v accepted", bad)
		}
	}
	s.Close()
	if _, err := s.PutDraft(model.Draft{ID: "d1", UserID: "u1", TemplateID: "t1"}); !errors.Is(err, ErrClosed) {
		t.Errorf("closed store: %v", err)
	}
}

func TestSavedFilters(t *testing.T) {
	s := open(t, t.TempDir())
	for _, f := range []model.SavedFilter{
		{ID: "f1", UserID: "u1", Name: "urgent", Query: "status=new"}, {ID: "f2", UserID: "u1", Name: "Alpha", Query: "group=g1"},
		{ID: "f3", UserID: "u2", Name: "mine", Query: "q=x"},
	} {
		if err := s.PutSavedFilter(f); err != nil {
			t.Fatal(err)
		}
	}
	list := s.SavedFilters("u1")
	if len(list) != 2 || list[0].Name != "Alpha" || list[1].Query != "status=new" {
		t.Errorf("SavedFilters = %+v", list)
	}
	if err := s.DeleteSavedFilter("u2", "f1"); err != nil || len(s.SavedFilters("u1")) != 2 {
		t.Error("another user deleted a filter")
	}
	if err := s.DeleteSavedFilter("u1", "f1"); err != nil || len(s.SavedFilters("u1")) != 1 {
		t.Error("delete failed")
	}
	var err error
	for i := 0; i < model.MaxSavedFilters+2; i++ {
		err = s.PutSavedFilter(model.SavedFilter{ID: model.NewID(), UserID: "u9", Name: fmt.Sprint("n", i), Query: "q"})
	}
	if !errors.Is(err, ErrTooMany) || len(s.SavedFilters("u9")) != model.MaxSavedFilters {
		t.Errorf("limit: %v, %d", err, len(s.SavedFilters("u9")))
	}
	for _, bad := range []model.SavedFilter{{ID: "X Y", UserID: "u", Name: "n"}, {ID: "f", Name: "n"}, {ID: "f", UserID: "u"}} {
		if err := s.PutSavedFilter(bad); err == nil {
			t.Errorf("invalid filter %+v accepted", bad)
		}
	}
	s.Close()
	if err := s.PutSavedFilter(model.SavedFilter{ID: "f1", UserID: "u1", Name: "n"}); !errors.Is(err, ErrClosed) {
		t.Errorf("closed store: %v", err)
	}
}

func TestAudit(t *testing.T) {
	s := open(t, t.TempDir())
	for i, e := range []model.AuditEntry{
		{Time: t0, User: "alpha", Role: "group", IP: "10.0.0.1", Event: "login"},
		{Time: t0.Add(time.Minute), User: "Leader", Role: "leader", IP: "10.0.0.2", Event: "chat_oversight_set", Detail: "oversight=on"},
		{Time: t0.Add(2 * time.Minute), User: "alpha", Role: "group", IP: "10.0.0.1", Event: "report_sent", Detail: "report=abc 100%_done"},
	} {
		if err := s.AddAudit(e); err != nil {
			t.Fatal(i, err)
		}
	}
	all := s.Audit("", 0, 10)
	if len(all) != 3 || all[0].Event != "report_sent" || all[2].Event != "login" || !all[1].Time.Equal(t0.Add(time.Minute)) || all[1].IP != "10.0.0.2" {
		t.Errorf("Audit = %+v", all)
	}
	for q, want := range map[string]int{"alpha": 2, "LEADER": 1, "oversight": 1, "10.0.0.2": 1, "report=abc": 1, "100%_": 1, "%": 1, "nomatch": 0, "'; DROP TABLE audit;--": 0} {
		if got := len(s.Audit(q, 0, 10)); got != want {
			t.Errorf("Audit(%q) = %d entries, want %d", q, got, want)
		}
	}
	if page := s.Audit("", all[0].Seq, 1); len(page) != 1 || page[0].Seq != all[1].Seq {
		t.Errorf("paging = %+v", page)
	}
	if got := s.Audit("", 0, 2); len(got) != 2 {
		t.Errorf("limit = %d", len(got))
	}
	s.Close()
	if err := s.AddAudit(model.AuditEntry{Event: "x"}); !errors.Is(err, ErrClosed) {
		t.Errorf("closed store: %v", err)
	}
}

func TestCopiesAndReset(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	seed(t, s)
	if _, err := s.PutDraft(model.Draft{ID: "d1", UserID: "g1", TemplateID: "t1", Updated: t0}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddChatMessage(model.ChatMessage{Conv: "all", SenderParty: "g1", SenderName: "A", Body: "hi", Timestamp: t0}, "", ""); err != nil {
		t.Fatal(err)
	}
	_ = s.MarkSeen("u1", "r1", true)
	_ = s.AddAudit(model.AuditEntry{Event: "kept", Time: t0})
	_ = s.PutSavedFilter(model.SavedFilter{ID: "f1", UserID: "u1", Name: "n", Query: "q"})
	if c := s.Counts(); c.Reports != 4 || c.Requests != 3 || c.ChatMessages != 1 || c.Drafts != 1 {
		t.Errorf("Counts = %+v", c)
	}

	// A copy is a complete, consistent database of its own.
	info, err := s.CopyTo(ArchiveDir, "exercise-1.db")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ArchiveDir, "exercise-1.db")
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 || fi.Size() != info.Size || info.Size == 0 {
		t.Fatalf("copy: %v %v %+v", fi, err, info)
	}
	if _, err := s.CopyTo(ArchiveDir, "exercise-1.db"); err == nil {
		t.Error("existing copy overwritten")
	}
	for _, bad := range []string{"", "x", "../evil.db", "a/b.db", ".hidden.db", "x.db.txt", strings.Repeat("a", 120) + ".db"} {
		if _, err := s.CopyTo(BackupDir, bad); err == nil {
			t.Errorf("file name %q accepted", bad)
		}
	}
	if _, err := s.CopyTo("elsewhere", "x.db"); err == nil {
		t.Error("arbitrary directory accepted")
	}
	if _, err := s.Copies("elsewhere"); err == nil {
		t.Error("listing of an arbitrary directory accepted")
	}

	// Reset removes the exercise but keeps accounts, templates, saved
	// filters and the audit log.
	if err := s.ResetExercise(); err != nil {
		t.Fatal(err)
	}
	if c := s.Counts(); c != (ExerciseCounts{}) {
		t.Errorf("after reset: %+v", c)
	}
	if len(s.Users()) != 4 || len(s.Templates(true)) != 3 || len(s.SavedFilters("u1")) != 1 || len(s.Audit("", 0, 10)) != 1 {
		t.Error("reset removed more than the exercise")
	}
	if n := s.NewCount("leader", "u1"); n != 0 {
		t.Errorf("read markers left behind: %d", n)
	}
	// The archive still holds what was removed.
	arch := t.TempDir()
	if err := os.Rename(path, filepath.Join(arch, DBName)); err != nil {
		t.Fatal(err)
	}
	old := open(t, arch)
	if c := old.Counts(); c.Reports != 4 || c.ChatMessages != 1 {
		t.Errorf("archive content: %+v", c)
	}

	// Backups: listing newest first, pruning by prefix.
	for i, name := range []string{"auto-1.db", "auto-2.db", "auto-3.db", "manual-1.db"} {
		if _, err := s.CopyTo(BackupDir, name); err != nil {
			t.Fatal(err)
		}
		ts := t0.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(filepath.Join(dir, BackupDir, name), ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, BackupDir, "notes.txt"), []byte("x"), 0o600)
	list, err := s.Copies(BackupDir)
	if err != nil || len(list) != 4 || list[0].Name != "manual-1.db" || list[3].Name != "auto-1.db" {
		t.Errorf("Copies = %+v, %v", list, err)
	}
	if n, err := s.PruneCopies(BackupDir, "auto-", 1); err != nil || n != 2 {
		t.Errorf("prune removed %d, %v", n, err)
	}
	list, _ = s.Copies(BackupDir)
	if len(list) != 2 || list[0].Name != "manual-1.db" || list[1].Name != "auto-3.db" {
		t.Errorf("after prune = %+v", list)
	}

	// Command line backup next to a live server.
	dest := filepath.Join(t.TempDir(), "cli.db")
	if err := BackupFile(dir, dest); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dest); err != nil || fi.Mode().Perm() != 0o600 || fi.Size() == 0 {
		t.Errorf("cli backup: %v %v", fi, err)
	}
	if err := BackupFile(dir, dest); err == nil {
		t.Error("cli backup overwrote a file")
	}
	if err := BackupFile(t.TempDir(), filepath.Join(t.TempDir(), "x.db")); err == nil {
		t.Error("backup of an empty directory succeeded")
	}
	if err := BackupFile(dir, "/nonexistent-dir/x.db"); err == nil {
		t.Error("backup to a missing directory succeeded")
	}
	s.Close()
	if _, err := s.CopyTo(BackupDir, "late.db"); !errors.Is(err, ErrClosed) {
		t.Errorf("copy on closed store: %v", err)
	}
	if err := s.ResetExercise(); !errors.Is(err, ErrClosed) {
		t.Errorf("reset on closed store: %v", err)
	}
}

// A database from before personal accounts, deadlines and read markers is
// upgraded without making the handled history unread.
func TestMigrationToVersion5(t *testing.T) {
	dir := t.TempDir()
	saved := migrations
	migrations = saved[:4]
	s, err := Open(dir)
	migrations = saved
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, username, display_name, role, pass_hash, created) VALUES ('l1','leader','L','leader','x',1)`,
		`INSERT INTO requests (id, subject, body, template_id, recipients, created_by, created) VALUES ('q1','s','b','t1','null','l1',1)`,
		`INSERT INTO reports (id, ts, sender_id, sender_name, subject, content, receiver_id, receiver_name, template_id, template_name, fields, request_id, status, folder, search_text)
			VALUES ('r1',1,'g1','A','old read','c','leader','L','','','null','','read','','x'), ('r2',2,'g1','A','old new','c','leader','L','','','null','','new','','x')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s2 := open(t, dir)
	rs := s2.Reports(&model.ReportFilter{ReceiverID: "leader", ViewerID: "l1"})
	model.SortReports(rs, model.SortTime, false)
	if len(rs) != 2 || rs[0].Unseen || !rs[1].Unseen || rs[0].AuthorID != "" || rs[0].AssigneeID != "" {
		t.Errorf("after upgrade: %+v", rs)
	}
	if q, _ := s2.Request("q1"); !q.Due.IsZero() {
		t.Error("old request gained a deadline")
	}
	if u, _ := s2.User("l1"); u.GroupID != "" {
		t.Error("old user gained a group")
	}
}
