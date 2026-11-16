package store

import (
	"bytes"
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

func TestPersistenceAndCompaction(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	seed(t, s)
	for i := 0; i < 20; i++ {
		if _, err := s.UpdateReport("r1", func(r *model.Report) bool { r.Folder = strings.Repeat("f", i+1); return true }); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.Stat(filepath.Join(dir, journalName))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if err := s.PutTemplate(model.Template{ID: "t9"}); !errors.Is(err, ErrClosed) {
		t.Errorf("write after close: %v", err)
	}

	s2 := open(t, dir)
	after, _ := os.Stat(filepath.Join(dir, journalName))
	if after.Size() >= before.Size() {
		t.Errorf("journal not compacted: %d -> %d", before.Size(), after.Size())
	}
	if after.Mode().Perm() != 0o600 {
		t.Errorf("journal mode = %v", after.Mode().Perm())
	}
	if len(s2.Users()) != 4 || len(s2.Templates(true)) != 3 || len(s2.Requests()) != 3 || len(s2.Reports(nil)) != 4 {
		t.Error("state lost on reopen")
	}
	if r, _ := s2.Report("r1"); len(r.Folder) != 20 {
		t.Errorf("last write lost: %q", r.Folder)
	}
	if u, ok := s2.UserByUsername("bravo"); !ok || u.ID != "g2" {
		t.Error("username index not rebuilt")
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
	if err := os.Mkdir(filepath.Join(dir, journalName), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Error("Open with a directory as journal succeeded")
	}
}

func writeJournal(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, journalName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

const goodLine = `{"k":"template","v":{"id":"t1","name":"Minimal"}}` + "\n"

func TestTornAndCorruptJournal(t *testing.T) {
	// A torn final record (crash during write) is dropped.
	for _, tail := range []string{`{"k":"report","v":{"id":"r1","subj`, `{"k":"report","v":{"id":"r1"}`, "\x00\x00\x00"} {
		s := open(t, writeJournal(t, goodLine+"\n"+tail))
		if len(s.Templates(true)) != 1 || len(s.Reports(nil)) != 0 {
			t.Errorf("tail %q: unexpected state", tail)
		}
	}
	// Damage in the middle is never ignored.
	bad := []string{
		"not json\n" + goodLine,
		`{"k":"mystery","v":{}}` + "\n" + goodLine,
		`{"k":"user","v":{"id":"u1"}}` + "\n" + goodLine,
		`{"k":"user","v":[]}` + "\n" + goodLine,
		`{"k":"template","v":{"id":"BAD"}}` + "\n" + goodLine,
		`{"k":"template","v":7}` + "\n" + goodLine,
		`{"k":"request","v":{"id":""}}` + "\n" + goodLine,
		`{"k":"request","v":"x"}` + "\n" + goodLine,
		`{"k":"report","v":{"id":"a/b"}}` + "\n" + goodLine,
		`{"k":"report","v":true}` + "\n" + goodLine,
		`{"k":"user","v":{"id":"u1","username":"a"}}` + "\n" + `{"k":"user","v":{"id":"u2","username":"a"}}` + "\n" + goodLine,
		`{"k":"report","v":{"id":"r1","content":"` + strings.Repeat("x", maxLine) + `"}}` + "\n" + goodLine,
	}
	for i, content := range bad {
		if _, err := Open(writeJournal(t, content)); err == nil {
			t.Errorf("case %d: corrupt journal accepted", i)
		}
	}
	// Rename of a user inside the journal keeps the index consistent.
	s := open(t, writeJournal(t, `{"k":"user","v":{"id":"u1","username":"a"}}`+"\n"+`{"k":"user","v":{"id":"u1","username":"b"}}`+"\n"))
	if _, ok := s.UserByUsername("a"); ok {
		t.Error("stale username after replay")
	}
	if _, ok := s.UserByUsername("b"); !ok {
		t.Error("renamed user missing after replay")
	}
}

func TestRecordTooLong(t *testing.T) {
	s := open(t, t.TempDir())
	if err := s.PutReport(model.Report{ID: "r1", Content: strings.Repeat("x", maxLine)}); err == nil {
		t.Error("oversized record accepted")
	}
	if _, ok := s.Report("r1"); ok {
		t.Error("failed write became visible")
	}
}

func TestWriteFailureLeavesStateUntouched(t *testing.T) {
	s := open(t, t.TempDir())
	seed(t, s)
	s.journal.Close() // simulate an I/O error on the journal
	if err := s.PutUser(model.User{ID: "u7", Username: "new"}); err == nil {
		t.Error("PutUser succeeded without journal")
	}
	if err := s.PutTemplate(model.Template{ID: "t7"}); err == nil {
		t.Error("PutTemplate succeeded without journal")
	}
	if err := s.PutRequest(model.Request{ID: "q7"}); err == nil {
		t.Error("PutRequest succeeded without journal")
	}
	if err := s.PutReport(model.Report{ID: "r7"}); err == nil {
		t.Error("PutReport succeeded without journal")
	}
	if _, err := s.UpdateReport("r1", func(r *model.Report) bool { r.Folder = "x"; return true }); err == nil {
		t.Error("UpdateReport succeeded without journal")
	}
	if _, ok := s.User("u7"); ok {
		t.Error("user visible after failed write")
	}
	if r, _ := s.Report("r1"); r.Folder != "" {
		t.Error("report changed after failed write")
	}
}

// FuzzReplay feeds arbitrary bytes to the journal parser. It must never
// panic, and whatever state it accepts must survive a compaction round trip.
func FuzzReplay(f *testing.F) {
	f.Add([]byte(goodLine))
	f.Add([]byte(`{"k":"user","v":{"id":"u1","username":"a","role":"group"}}` + "\n" + `{"k":"report","v":{"id":"r1","status":"new"}}` + "\n"))
	f.Add([]byte(`{"k":"request","v":{"id":"q1","recipients":["a"]}}` + "\n{"))
	f.Add([]byte("\n\n\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		s := newStore()
		if err := s.replay(bytes.NewReader(data)); err != nil {
			return
		}
		dir := t.TempDir()
		if err := s.compact(dir); err != nil {
			t.Fatalf("compact: %v", err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, journalName))
		if err != nil {
			t.Fatal(err)
		}
		s2 := newStore()
		if err := s2.replay(bytes.NewReader(raw)); err != nil {
			t.Fatalf("compacted journal does not replay: %v", err)
		}
		if len(s2.users) != len(s.users) || len(s2.byName) != len(s.byName) || len(s2.templates) != len(s.templates) ||
			len(s2.requests) != len(s.requests) || len(s2.reports) != len(s.reports) {
			t.Error("state changed by compaction round trip")
		}
	})
}
