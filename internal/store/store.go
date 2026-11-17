// Package store persists all application state in an SQLite database
// (pure-Go driver, no cgo). Writes run with synchronous=FULL, so a change
// is on disk before it is acknowledged. See docs/adr/0010-sqlite-storage.md.
//
// Write methods return errors. Read methods have no error result: a read
// can only fail when the database is closed, damaged or the disk is gone,
// and in that case they panic with a *Failure, which the HTTP layer turns
// into a 500 response.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"

	"github.com/rom/gr_report/internal/model"
)

// Errors returned by the store.
var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("username already exists")
	ErrLocked    = errors.New("data directory is in use by another process")
	ErrClosed    = errors.New("store is closed")
)

// Failure is the panic value of a failed read.
type Failure struct{ Err error }

func (f *Failure) Error() string { return "store: " + f.Err.Error() }
func (f *Failure) Unwrap() error { return f.Err }

// File names inside the data directory.
const (
	DBName   = "gr_report.db"
	lockName = "lock"
	// BackupDir and ArchiveDir are subdirectories of the data directory
	// for copies of the database.
	BackupDir  = "backups"
	ArchiveDir = "archive"
)

// migrations are applied in order; the index+1 is stored in user_version.
// Never edit an entry that has shipped: append a new one.
var migrations = []string{`
CREATE TABLE users (
	id           TEXT PRIMARY KEY,
	username     TEXT NOT NULL UNIQUE,
	display_name TEXT NOT NULL,
	role         TEXT NOT NULL,
	pass_hash    TEXT NOT NULL,
	disabled     INTEGER NOT NULL DEFAULT 0,
	must_change  INTEGER NOT NULL DEFAULT 0,
	created      INTEGER NOT NULL
) STRICT;
CREATE TABLE templates (
	id              TEXT PRIMARY KEY,
	name            TEXT NOT NULL,
	description     TEXT NOT NULL,
	default_subject TEXT NOT NULL,
	content_help    TEXT NOT NULL,
	fields          TEXT NOT NULL,
	created_by      TEXT NOT NULL,
	created         INTEGER NOT NULL,
	archived        INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE TABLE requests (
	id          TEXT PRIMARY KEY,
	subject     TEXT NOT NULL,
	body        TEXT NOT NULL,
	template_id TEXT NOT NULL,
	recipients  TEXT NOT NULL,
	created_by  TEXT NOT NULL,
	created     INTEGER NOT NULL,
	closed      INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE TABLE reports (
	id            TEXT PRIMARY KEY,
	ts            INTEGER NOT NULL,
	sender_id     TEXT NOT NULL,
	sender_name   TEXT NOT NULL,
	subject       TEXT NOT NULL,
	content       TEXT NOT NULL,
	receiver_id   TEXT NOT NULL,
	receiver_name TEXT NOT NULL,
	template_id   TEXT NOT NULL,
	template_name TEXT NOT NULL,
	fields        TEXT NOT NULL,
	request_id    TEXT NOT NULL,
	status        TEXT NOT NULL,
	folder        TEXT NOT NULL,
	search_text   TEXT NOT NULL
) STRICT;
CREATE INDEX reports_receiver ON reports (receiver_id, sender_id, status);
CREATE INDEX reports_sender   ON reports (sender_id);
CREATE INDEX reports_request  ON reports (request_id);
`, `
CREATE TABLE chat_messages (
	seq          INTEGER PRIMARY KEY AUTOINCREMENT,
	conv         TEXT NOT NULL,
	party_a      TEXT NOT NULL,
	party_b      TEXT NOT NULL,
	ts           INTEGER NOT NULL,
	sender_party TEXT NOT NULL,
	sender_name  TEXT NOT NULL,
	body         TEXT NOT NULL
) STRICT;
CREATE INDEX chat_conv ON chat_messages (conv, seq);
CREATE TABLE chat_read (
	party TEXT NOT NULL,
	conv  TEXT NOT NULL,
	seq   INTEGER NOT NULL,
	PRIMARY KEY (party, conv)
) STRICT;
`, `
ALTER TABLE chat_messages ADD COLUMN overseen INTEGER NOT NULL DEFAULT 0;
`, `
CREATE TABLE settings (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL,
	updated_by TEXT NOT NULL,
	updated_at INTEGER NOT NULL
) STRICT;
`, `
ALTER TABLE users    ADD COLUMN group_id      TEXT NOT NULL DEFAULT '';
ALTER TABLE requests ADD COLUMN due           INTEGER NOT NULL DEFAULT 0;
ALTER TABLE reports  ADD COLUMN author_id     TEXT NOT NULL DEFAULT '';
ALTER TABLE reports  ADD COLUMN author_name   TEXT NOT NULL DEFAULT '';
ALTER TABLE reports  ADD COLUMN assignee_id   TEXT NOT NULL DEFAULT '';
ALTER TABLE reports  ADD COLUMN assignee_name TEXT NOT NULL DEFAULT '';
CREATE TABLE report_seen (
	user_id   TEXT NOT NULL,
	report_id TEXT NOT NULL,
	PRIMARY KEY (user_id, report_id)
) STRICT, WITHOUT ROWID;
-- Everything that had been opened counts as seen by everyone, so an
-- upgrade does not turn the whole history unread.
INSERT INTO report_seen (user_id, report_id)
	SELECT u.id, r.id FROM users u, reports r WHERE r.status <> 'new';
CREATE TABLE drafts (
	id          TEXT PRIMARY KEY,
	user_id     TEXT NOT NULL,
	template_id TEXT NOT NULL,
	request_id  TEXT NOT NULL,
	receiver    TEXT NOT NULL,
	subject     TEXT NOT NULL,
	content     TEXT NOT NULL,
	fields      TEXT NOT NULL,
	updated     INTEGER NOT NULL,
	UNIQUE (user_id, template_id, request_id)
) STRICT;
CREATE TABLE saved_filters (
	id      TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	name    TEXT NOT NULL,
	query   TEXT NOT NULL
) STRICT;
CREATE INDEX saved_filters_user ON saved_filters (user_id);
CREATE TABLE audit (
	seq    INTEGER PRIMARY KEY AUTOINCREMENT,
	ts     INTEGER NOT NULL,
	user   TEXT NOT NULL,
	role   TEXT NOT NULL,
	ip     TEXT NOT NULL,
	event  TEXT NOT NULL,
	detail TEXT NOT NULL
) STRICT;
`}

// Store is safe for concurrent use.
type Store struct {
	db   *sql.DB
	lock *os.File
	dir  string
}

// Open opens (or creates) the database in dir and brings its schema up to
// date. Only one process can have a data directory open at a time.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// Created now because the sandbox later forbids making directories.
	for _, sub := range []string{BackupDir, ArchiveDir} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, err
		}
	}
	lock, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, ErrLocked
	}
	path := filepath.Join(dir, DBName)
	// Create the file ourselves so it is private from the first byte.
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600); err != nil {
		lock.Close()
		return nil, err
	} else {
		f.Close()
	}
	q := url.Values{}
	for _, p := range []string{
		"journal_mode(WAL)",  // readers do not block the writer; online backup works
		"synchronous(FULL)",  // fsync before a commit returns
		"foreign_keys(ON)",   //
		"busy_timeout(5000)", //
		"temp_store(MEMORY)", // never create files outside the data directory
		"trusted_schema(OFF)",
	} {
		q.Add("_pragma", p)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?"+q.Encode())
	if err != nil {
		lock.Close()
		return nil, err
	}
	// One connection: SQLite has a single writer anyway, and it keeps the
	// semantics simple (no SQLITE_BUSY between our own goroutines).
	db.SetMaxOpenConns(1)
	s := &Store{db: db, lock: lock, dir: dir}
	if err := s.migrate(); err != nil {
		s.Close()
		return nil, fmt.Errorf("database %s: %w", path, err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("schema version %d is newer than this program supports (%d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	var ok string
	if err := s.db.QueryRow("PRAGMA quick_check").Scan(&ok); err != nil {
		return err
	}
	if ok != "ok" {
		return fmt.Errorf("integrity check failed: %s", ok)
	}
	return nil
}

// Close checkpoints and closes the database and releases the lock.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	s.lock.Close()
	return err
}

func (s *Store) conn() (*sql.DB, error) {
	if s.db == nil {
		return nil, ErrClosed
	}
	return s.db, nil
}

// must panics with a *Failure for a failed read.
func must(err error) {
	if err != nil {
		panic(&Failure{Err: err})
	}
}

func (s *Store) query(q string, args ...any) *sql.Rows {
	db, err := s.conn()
	must(err)
	rows, err := db.Query(q, args...)
	must(err)
	return rows
}

func (s *Store) exec(q string, args ...any) error {
	db, err := s.conn()
	if err != nil {
		return err
	}
	_, err = db.Exec(q, args...)
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // only slices of plain structs are passed
	}
	return string(b)
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func fromUnix(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

// ---- users ----

const userCols = "id, username, display_name, role, pass_hash, disabled, must_change, created, group_id"

func scanUser(sc interface{ Scan(...any) error }) (model.User, error) {
	var u model.User
	var created int64
	err := sc.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.PassHash, &u.Disabled, &u.MustChange, &created, &u.GroupID)
	u.Created = fromUnix(created)
	return u, err
}

// PutUser creates or replaces a user. Usernames are unique.
func (s *Store) PutUser(u model.User) error {
	if !model.ValidID(u.ID) || u.Username == "" {
		return errors.New("invalid user")
	}
	err := s.exec(`INSERT INTO users (`+userCols+`) VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET username=excluded.username, display_name=excluded.display_name,
		role=excluded.role, pass_hash=excluded.pass_hash, disabled=excluded.disabled,
		must_change=excluded.must_change, created=excluded.created, group_id=excluded.group_id`,
		u.ID, u.Username, u.DisplayName, string(u.Role), u.PassHash, b2i(u.Disabled), b2i(u.MustChange), unix(u.Created), u.GroupID)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: users.username") {
		return ErrDuplicate
	}
	return err
}

func (s *Store) oneUser(where string, arg any) (model.User, bool) {
	db, err := s.conn()
	must(err)
	u, err := scanUser(db.QueryRow("SELECT "+userCols+" FROM users WHERE "+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, false
	}
	must(err)
	return u, true
}

// User returns the user with the given ID.
func (s *Store) User(id string) (model.User, bool) { return s.oneUser("id = ?", id) }

// UserByUsername returns the user with the given username.
func (s *Store) UserByUsername(name string) (model.User, bool) {
	return s.oneUser("username = ?", strings.ToLower(name))
}

// Users returns all users sorted by role and display name.
func (s *Store) Users() []model.User {
	rows := s.query("SELECT " + userCols + " FROM users")
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		must(err)
		out = append(out, u)
	}
	must(rows.Err())
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		a, b := strings.ToLower(out[i].DisplayName), strings.ToLower(out[j].DisplayName)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Groups returns the enabled groups: the group accounts themselves, not
// the personal accounts of their members.
func (s *Store) Groups() []model.User {
	var out []model.User
	for _, u := range s.Users() {
		if u.Role == model.RoleGroup && !u.Disabled && u.GroupID == "" {
			out = append(out, u)
		}
	}
	return out
}

// ---- templates ----

const templateCols = "id, name, description, default_subject, content_help, fields, created_by, created, archived"

func scanTemplate(sc interface{ Scan(...any) error }) (model.Template, error) {
	var t model.Template
	var fields string
	var created int64
	if err := sc.Scan(&t.ID, &t.Name, &t.Description, &t.DefaultSubject, &t.ContentHelp, &fields, &t.CreatedBy, &created, &t.Archived); err != nil {
		return t, err
	}
	t.Created = fromUnix(created)
	return t, json.Unmarshal([]byte(fields), &t.Fields)
}

// PutTemplate creates or replaces a template.
func (s *Store) PutTemplate(t model.Template) error {
	if !model.ValidID(t.ID) {
		return errors.New("invalid template")
	}
	return s.exec(`INSERT OR REPLACE INTO templates (`+templateCols+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Name, t.Description, t.DefaultSubject, t.ContentHelp, toJSON(t.Fields), t.CreatedBy, unix(t.Created), b2i(t.Archived))
}

// Template returns the template with the given ID.
func (s *Store) Template(id string) (model.Template, bool) {
	db, err := s.conn()
	must(err)
	t, err := scanTemplate(db.QueryRow("SELECT "+templateCols+" FROM templates WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Template{}, false
	}
	must(err)
	return t, true
}

// Templates returns the templates sorted by name. Archived templates are
// included only when all is true.
func (s *Store) Templates(all bool) []model.Template {
	q := "SELECT " + templateCols + " FROM templates"
	if !all {
		q += " WHERE archived = 0"
	}
	rows := s.query(q)
	defer rows.Close()
	var out []model.Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		must(err)
		out = append(out, t)
	}
	must(rows.Err())
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ---- requests ----

const requestCols = "id, subject, body, template_id, recipients, created_by, created, closed, due"

func scanRequest(sc interface{ Scan(...any) error }) (model.Request, error) {
	var q model.Request
	var recipients string
	var created, due int64
	if err := sc.Scan(&q.ID, &q.Subject, &q.Body, &q.TemplateID, &recipients, &q.CreatedBy, &created, &q.Closed, &due); err != nil {
		return q, err
	}
	q.Created, q.Due = fromUnix(created), fromUnix(due)
	return q, json.Unmarshal([]byte(recipients), &q.Recipients)
}

// PutRequest creates or replaces an information request.
func (s *Store) PutRequest(q model.Request) error {
	if !model.ValidID(q.ID) {
		return errors.New("invalid request")
	}
	return s.exec(`INSERT OR REPLACE INTO requests (`+requestCols+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		q.ID, q.Subject, q.Body, q.TemplateID, toJSON(q.Recipients), q.CreatedBy, unix(q.Created), b2i(q.Closed), unix(q.Due))
}

// Request returns the request with the given ID.
func (s *Store) Request(id string) (model.Request, bool) {
	db, err := s.conn()
	must(err)
	q, err := scanRequest(db.QueryRow("SELECT "+requestCols+" FROM requests WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Request{}, false
	}
	must(err)
	return q, true
}

// Requests returns all requests, newest first.
func (s *Store) Requests() []model.Request {
	rows := s.query("SELECT " + requestCols + " FROM requests ORDER BY created DESC, id")
	defer rows.Close()
	var out []model.Request
	for rows.Next() {
		q, err := scanRequest(rows)
		must(err)
		out = append(out, q)
	}
	must(rows.Err())
	return out
}

// ---- reports ----

const reportCols = "id, ts, sender_id, sender_name, subject, content, receiver_id, receiver_name, " +
	"template_id, template_name, fields, request_id, status, folder, author_id, author_name, assignee_id, assignee_name"

// seenExpr counts whether a given user (one bound parameter) has opened the
// report of the current row.
const seenExpr = "(SELECT COUNT(*) FROM report_seen s WHERE s.user_id = ? AND s.report_id = reports.id)"

// scanReport scans reportCols, plus further columns into extra.
func scanReport(sc interface{ Scan(...any) error }, extra ...any) (model.Report, error) {
	var r model.Report
	var fields string
	var ts int64
	dest := append([]any{&r.ID, &ts, &r.SenderID, &r.SenderName, &r.Subject, &r.Content, &r.ReceiverID, &r.ReceiverName,
		&r.TemplateID, &r.TemplateName, &fields, &r.RequestID, &r.Status, &r.Folder,
		&r.AuthorID, &r.AuthorName, &r.AssigneeID, &r.AssigneeName}, extra...)
	if err := sc.Scan(dest...); err != nil {
		return r, err
	}
	r.Timestamp = fromUnix(ts)
	return r, json.Unmarshal([]byte(fields), &r.Fields)
}

// searchText is the lower-cased haystack for full text search. Lower-casing
// happens in Go because SQLite only folds ASCII (Å, Ä and Ö matter here).
func searchText(r *model.Report) string {
	parts := []string{r.Subject, r.Content, r.SenderName, r.TemplateName, r.Folder, r.AuthorName}
	for _, f := range r.Fields {
		parts = append(parts, f.Value)
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

const putReportSQL = `INSERT OR REPLACE INTO reports (` + reportCols + `, search_text) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

func reportArgs(r *model.Report) []any {
	return []any{r.ID, unix(r.Timestamp), r.SenderID, r.SenderName, r.Subject, r.Content, r.ReceiverID, r.ReceiverName,
		r.TemplateID, r.TemplateName, toJSON(r.Fields), r.RequestID, string(r.Status), r.Folder,
		r.AuthorID, r.AuthorName, r.AssigneeID, r.AssigneeName, searchText(r)}
}

// PutReport stores a report.
func (s *Store) PutReport(r model.Report) error {
	if !model.ValidID(r.ID) {
		return errors.New("invalid report")
	}
	return s.exec(putReportSQL, reportArgs(&r)...)
}

// Report returns the report with the given ID.
func (s *Store) Report(id string) (model.Report, bool) {
	db, err := s.conn()
	must(err)
	r, err := scanReport(db.QueryRow("SELECT "+reportCols+" FROM reports WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Report{}, false
	}
	must(err)
	return r, true
}

// Members returns the personal accounts that belong to a group.
func (s *Store) Members(groupID string) []model.User {
	var out []model.User
	for _, u := range s.Users() {
		if u.GroupID == groupID && groupID != "" {
			out = append(out, u)
		}
	}
	return out
}

// UpdateReport modifies a report in one transaction. If fn returns false
// nothing is written.
func (s *Store) UpdateReport(id string, fn func(*model.Report) bool) (model.Report, error) {
	db, err := s.conn()
	if err != nil {
		return model.Report{}, err
	}
	tx, err := db.Begin()
	if err != nil {
		return model.Report{}, err
	}
	defer func() { _ = tx.Rollback() }()
	cur, err := scanReport(tx.QueryRow("SELECT "+reportCols+" FROM reports WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Report{}, ErrNotFound
	}
	if err != nil {
		return model.Report{}, err
	}
	r := cur.Clone()
	if !fn(&r) {
		return cur, nil
	}
	r.ID = id
	if _, err := tx.Exec(putReportSQL, reportArgs(&r)...); err != nil {
		return model.Report{}, err
	}
	return r, tx.Commit()
}

// likeEscape escapes the LIKE wildcards in a search term.
var likeEscape = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Reports returns the reports matching f (nil matches all), unsorted. All
// values are bound parameters; user input never becomes SQL text.
func (s *Store) Reports(f *model.ReportFilter) []model.Report {
	var where []string
	var args []any
	if f != nil {
		eq := func(col, val string) {
			if val != "" {
				where = append(where, col+" = ?")
				args = append(args, val)
			}
		}
		eq("receiver_id", f.ReceiverID)
		eq("sender_id", f.SenderID)
		eq("status", string(f.Status))
		eq("template_id", f.TemplateID)
		eq("request_id", f.RequestID)
		eq("folder", f.Folder)
		if !f.From.IsZero() {
			where = append(where, "ts >= ?")
			args = append(args, f.From.UnixNano())
		}
		if !f.To.IsZero() {
			where = append(where, "ts < ?")
			args = append(args, f.To.UnixNano())
		}
		for _, term := range f.Terms() {
			where = append(where, `search_text LIKE ? ESCAPE '\'`)
			args = append(args, "%"+likeEscape.Replace(term)+"%")
		}
		switch f.AssigneeID {
		case "":
		case model.AssigneeNone:
			where = append(where, "assignee_id = ''")
		default:
			eq("assignee_id", f.AssigneeID)
		}
		if f.UnseenOnly && f.ViewerID != "" {
			where = append(where, seenExpr+" = 0")
			args = append(args, f.ViewerID)
		}
	}
	viewer := ""
	if f != nil {
		viewer = f.ViewerID
	}
	q := "SELECT " + reportCols + ", " + seenExpr + " FROM reports"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	rows := s.query(q, append([]any{viewer}, args...)...)
	defer rows.Close()
	var out []model.Report
	for rows.Next() {
		var seen int
		r, err := scanReport(rows, &seen)
		must(err)
		r.Unseen = viewer != "" && seen == 0
		out = append(out, r)
	}
	must(rows.Err())
	return out
}

// QueueStats summarises a receiver's queue.
type QueueStats struct {
	Total   map[string]int // per sender ID
	New     map[string]int // per sender ID
	Folders []string       // distinct folder names, sorted
}

// Stats computes, for a receiver's queue, the totals per sender, the number
// of reports the viewer has not opened per sender, and the folder list.
func (s *Store) Stats(receiverID, viewerID string) QueueStats {
	st := QueueStats{Total: map[string]int{}, New: map[string]int{}}
	rows := s.query(`SELECT sender_id, COUNT(*), COALESCE(SUM(`+seenExpr+` = 0), 0)
		FROM reports WHERE receiver_id = ? GROUP BY sender_id`, viewerID, receiverID)
	for rows.Next() {
		var sender string
		var total, unseen int
		must(rows.Scan(&sender, &total, &unseen))
		st.Total[sender] = total
		if unseen > 0 {
			st.New[sender] = unseen
		}
	}
	must(rows.Err())
	rows.Close()
	rows = s.query(`SELECT DISTINCT folder FROM reports WHERE receiver_id = ? AND folder <> ''`, receiverID)
	defer rows.Close()
	for rows.Next() {
		var folder string
		must(rows.Scan(&folder))
		st.Folders = append(st.Folders, folder)
	}
	must(rows.Err())
	sort.Strings(st.Folders)
	return st
}

// NewCount returns the number of reports in a receiver's queue that the
// viewer has not opened.
func (s *Store) NewCount(receiverID, viewerID string) int {
	db, err := s.conn()
	must(err)
	var n int
	must(db.QueryRow(`SELECT COUNT(*) FROM reports WHERE receiver_id = ? AND `+seenExpr+` = 0`, receiverID, viewerID).Scan(&n))
	return n
}

// MarkSeen records that a user has opened a report; with seen false it
// makes the report unread for that user again.
func (s *Store) MarkSeen(userID, reportID string, seen bool) error {
	if seen {
		return s.exec(`INSERT OR IGNORE INTO report_seen (user_id, report_id) VALUES (?,?)`, userID, reportID)
	}
	return s.exec(`DELETE FROM report_seen WHERE user_id = ? AND report_id = ?`, userID, reportID)
}

// ---- chat ----

// AddChatMessage stores a message and returns it with its sequence number.
// a and b are the two parties of a direct conversation ("" for ChatAll).
func (s *Store) AddChatMessage(m model.ChatMessage, a, b string) (model.ChatMessage, error) {
	db, err := s.conn()
	if err != nil {
		return m, err
	}
	if m.Conv == "" || m.SenderParty == "" || m.Body == "" {
		return m, errors.New("invalid chat message")
	}
	res, err := db.Exec(`INSERT INTO chat_messages (conv, party_a, party_b, ts, sender_party, sender_name, body, overseen) VALUES (?,?,?,?,?,?,?,?)`,
		m.Conv, a, b, unix(m.Timestamp), m.SenderParty, m.SenderName, m.Body, b2i(m.Overseen))
	if err != nil {
		return m, err
	}
	m.Seq, err = res.LastInsertId()
	return m, err
}

// ChatMessages returns up to limit messages of a conversation in
// chronological order: those after sequence number after when after > 0
// (the oldest such), otherwise the newest ones before sequence number
// before (0: the newest of all). With overseenOnly, only messages sent
// under leader oversight are returned.
func (s *Store) ChatMessages(conv string, after, before int64, limit int, overseenOnly bool) []model.ChatMessage {
	q := `SELECT seq, conv, ts, sender_party, sender_name, body, overseen FROM chat_messages WHERE conv = ?`
	args := []any{conv}
	if overseenOnly {
		q += ` AND overseen = 1`
	}
	switch {
	case after > 0:
		q += ` AND seq > ? ORDER BY seq ASC LIMIT ?`
		args = append(args, after, limit)
	case before > 0:
		q += ` AND seq < ? ORDER BY seq DESC LIMIT ?`
		args = append(args, before, limit)
	default:
		q += ` ORDER BY seq DESC LIMIT ?`
		args = append(args, limit)
	}
	rows := s.query(q, args...)
	defer rows.Close()
	var out []model.ChatMessage
	for rows.Next() {
		var m model.ChatMessage
		var ts int64
		must(rows.Scan(&m.Seq, &m.Conv, &ts, &m.SenderParty, &m.SenderName, &m.Body, &m.Overseen))
		m.Timestamp = fromUnix(ts)
		out = append(out, m)
	}
	must(rows.Err())
	if after <= 0 { // fetched newest first
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out
}

// MarkChatRead records that a party has seen a conversation up to seq. The
// marker never moves backwards.
func (s *Store) MarkChatRead(party, conv string, seq int64) error {
	return s.exec(`INSERT INTO chat_read (party, conv, seq) VALUES (?,?,?)
		ON CONFLICT(party, conv) DO UPDATE SET seq = MAX(seq, excluded.seq)`, party, conv, seq)
}

// ChatUnread returns, per conversation the party takes part in, the number
// of messages from others that the party has not seen.
func (s *Store) ChatUnread(party string) map[string]int {
	rows := s.query(`SELECT m.conv, COUNT(*) FROM chat_messages m
		LEFT JOIN chat_read r ON r.party = ? AND r.conv = m.conv
		WHERE (m.conv = ? OR m.party_a = ? OR m.party_b = ?)
		  AND m.sender_party <> ? AND m.seq > COALESCE(r.seq, 0)
		GROUP BY m.conv`, party, model.ChatAll, party, party, party)
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var conv string
		var n int
		must(rows.Scan(&conv, &n))
		out[conv] = n
	}
	must(rows.Err())
	return out
}

// OverseenConv is a group-to-group conversation that holds messages sent
// under leader oversight.
type OverseenConv struct {
	A, B  string // the two group IDs
	Count int    // number of overseen messages
}

// ChatOverseen lists the conversations with overseen messages.
func (s *Store) ChatOverseen() []OverseenConv {
	rows := s.query(`SELECT party_a, party_b, COUNT(*) FROM chat_messages WHERE overseen = 1 GROUP BY conv ORDER BY MAX(seq) DESC`)
	defer rows.Close()
	var out []OverseenConv
	for rows.Next() {
		var c OverseenConv
		must(rows.Scan(&c.A, &c.B, &c.Count))
		out = append(out, c)
	}
	must(rows.Err())
	return out
}

// ---- settings changed at run time ----

// Setting is a value that is changed in the user interface rather than in
// the configuration file, with who changed it and when.
type Setting struct {
	Value     string
	UpdatedBy string // username
	UpdatedAt time.Time
}

// Setting returns a stored setting.
func (s *Store) Setting(key string) (Setting, bool) {
	db, err := s.conn()
	must(err)
	var v Setting
	var at int64
	err = db.QueryRow("SELECT value, updated_by, updated_at FROM settings WHERE key = ?", key).Scan(&v.Value, &v.UpdatedBy, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Setting{}, false
	}
	must(err)
	v.UpdatedAt = fromUnix(at)
	return v, true
}

// PutSetting stores a setting.
func (s *Store) PutSetting(key string, v Setting) error {
	if key == "" {
		return errors.New("invalid setting")
	}
	return s.exec(`INSERT OR REPLACE INTO settings (key, value, updated_by, updated_at) VALUES (?,?,?,?)`,
		key, v.Value, v.UpdatedBy, unix(v.UpdatedAt))
}

// ---- drafts ----

func scanDraft(sc interface{ Scan(...any) error }) (model.Draft, error) {
	var d model.Draft
	var fields string
	var updated int64
	if err := sc.Scan(&d.ID, &d.UserID, &d.TemplateID, &d.RequestID, &d.Receiver, &d.Subject, &d.Content, &fields, &updated); err != nil {
		return d, err
	}
	d.Updated = fromUnix(updated)
	return d, json.Unmarshal([]byte(fields), &d.Values)
}

const draftCols = "id, user_id, template_id, request_id, receiver, subject, content, fields, updated"

// ErrTooMany is returned when a per-user limit is reached.
var ErrTooMany = errors.New("limit reached")

// PutDraft saves a draft. A user has one draft per template and request;
// saving again replaces it and keeps its ID.
func (s *Store) PutDraft(d model.Draft) (model.Draft, error) {
	db, err := s.conn()
	if err != nil {
		return d, err
	}
	if !model.ValidID(d.ID) || d.UserID == "" || d.TemplateID == "" {
		return d, errors.New("invalid draft")
	}
	tx, err := db.Begin()
	if err != nil {
		return d, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	err = tx.QueryRow(`SELECT id FROM drafts WHERE user_id = ? AND template_id = ? AND request_id = ?`,
		d.UserID, d.TemplateID, d.RequestID).Scan(&existing)
	switch {
	case err == nil:
		d.ID = existing
	case errors.Is(err, sql.ErrNoRows):
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM drafts WHERE user_id = ?`, d.UserID).Scan(&n); err != nil {
			return d, err
		}
		if n >= model.MaxDrafts {
			return d, ErrTooMany
		}
	default:
		return d, err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO drafts (`+draftCols+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		d.ID, d.UserID, d.TemplateID, d.RequestID, d.Receiver, d.Subject, d.Content, toJSON(d.Values), unix(d.Updated)); err != nil {
		return d, err
	}
	return d, tx.Commit()
}

// Draft returns a user's draft for a template and request, if any.
func (s *Store) Draft(userID, templateID, requestID string) (model.Draft, bool) {
	db, err := s.conn()
	must(err)
	d, err := scanDraft(db.QueryRow("SELECT "+draftCols+" FROM drafts WHERE user_id = ? AND template_id = ? AND request_id = ?",
		userID, templateID, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Draft{}, false
	}
	must(err)
	return d, true
}

// Drafts returns a user's drafts, most recently saved first.
func (s *Store) Drafts(userID string) []model.Draft {
	rows := s.query("SELECT "+draftCols+" FROM drafts WHERE user_id = ? ORDER BY updated DESC, id", userID)
	defer rows.Close()
	var out []model.Draft
	for rows.Next() {
		d, err := scanDraft(rows)
		must(err)
		out = append(out, d)
	}
	must(rows.Err())
	return out
}

// DeleteDraft removes a user's draft. Deleting one that does not exist, or
// that belongs to someone else, does nothing.
func (s *Store) DeleteDraft(userID, id string) error {
	return s.exec(`DELETE FROM drafts WHERE user_id = ? AND id = ?`, userID, id)
}

// ---- saved filters ----

// PutSavedFilter stores a named queue view for a user.
func (s *Store) PutSavedFilter(f model.SavedFilter) error {
	db, err := s.conn()
	if err != nil {
		return err
	}
	if !model.ValidID(f.ID) || f.UserID == "" || f.Name == "" {
		return errors.New("invalid saved filter")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM saved_filters WHERE user_id = ? AND id <> ?`, f.UserID, f.ID).Scan(&n); err != nil {
		return err
	}
	if n >= model.MaxSavedFilters {
		return ErrTooMany
	}
	return s.exec(`INSERT OR REPLACE INTO saved_filters (id, user_id, name, query) VALUES (?,?,?,?)`, f.ID, f.UserID, f.Name, f.Query)
}

// SavedFilters returns a user's saved filters sorted by name.
func (s *Store) SavedFilters(userID string) []model.SavedFilter {
	rows := s.query(`SELECT id, user_id, name, query FROM saved_filters WHERE user_id = ?`, userID)
	defer rows.Close()
	var out []model.SavedFilter
	for rows.Next() {
		var f model.SavedFilter
		must(rows.Scan(&f.ID, &f.UserID, &f.Name, &f.Query))
		out = append(out, f)
	}
	must(rows.Err())
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// DeleteSavedFilter removes one of a user's saved filters.
func (s *Store) DeleteSavedFilter(userID, id string) error {
	return s.exec(`DELETE FROM saved_filters WHERE user_id = ? AND id = ?`, userID, id)
}

// ---- audit log ----

// MaxAudit is the number of audit entries kept; older ones are removed.
const MaxAudit = 100_000

// AddAudit appends an entry to the audit log.
func (s *Store) AddAudit(e model.AuditEntry) error {
	db, err := s.conn()
	if err != nil {
		return err
	}
	res, err := db.Exec(`INSERT INTO audit (ts, user, role, ip, event, detail) VALUES (?,?,?,?,?,?)`,
		unix(e.Time), e.User, e.Role, e.IP, e.Event, e.Detail)
	if err != nil {
		return err
	}
	// Trim now and then rather than on every insert.
	if seq, err := res.LastInsertId(); err == nil && seq%500 == 0 {
		_, err = db.Exec(`DELETE FROM audit WHERE seq <= ?`, seq-MaxAudit)
		return err
	}
	return nil
}

// Audit returns up to limit audit entries, newest first, older than
// sequence number before (0: from the newest). A non-empty query selects
// entries whose user, event, address or detail contains it.
func (s *Store) Audit(query string, before int64, limit int) []model.AuditEntry {
	q := `SELECT seq, ts, user, role, ip, event, detail FROM audit WHERE 1=1`
	var args []any
	if before > 0 {
		q += ` AND seq < ?`
		args = append(args, before)
	}
	if query != "" {
		pat := "%" + likeEscape.Replace(strings.ToLower(query)) + "%"
		q += ` AND (lower(user) LIKE ? ESCAPE '\' OR lower(event) LIKE ? ESCAPE '\' OR ip LIKE ? ESCAPE '\' OR lower(detail) LIKE ? ESCAPE '\')`
		args = append(args, pat, pat, pat, pat)
	}
	q += ` ORDER BY seq DESC LIMIT ?`
	rows := s.query(q, append(args, limit)...)
	defer rows.Close()
	var out []model.AuditEntry
	for rows.Next() {
		var e model.AuditEntry
		var ts int64
		must(rows.Scan(&e.Seq, &ts, &e.User, &e.Role, &e.IP, &e.Event, &e.Detail))
		e.Time = fromUnix(ts)
		out = append(out, e)
	}
	must(rows.Err())
	return out
}

// ---- backup, archive, new exercise ----

// CopyInfo describes a copy of the database in BackupDir or ArchiveDir.
type CopyInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
}

var copyNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}\.db$`)

// CopyTo writes a consistent copy of the whole database to a new file in
// the given subdirectory (BackupDir or ArchiveDir) of the data directory.
// It works while the server is in use.
func (s *Store) CopyTo(subdir, name string) (CopyInfo, error) {
	if subdir != BackupDir && subdir != ArchiveDir {
		return CopyInfo{}, errors.New("invalid directory")
	}
	if !copyNameRe.MatchString(name) {
		return CopyInfo{}, errors.New("invalid file name")
	}
	path := filepath.Join(s.dir, subdir, name)
	if _, err := os.Stat(path); err == nil {
		return CopyInfo{}, fmt.Errorf("%s already exists", name)
	}
	// VACUUM INTO writes a compact, transactionally consistent copy.
	if err := s.exec(`VACUUM INTO ?`, path); err != nil {
		os.Remove(path)
		return CopyInfo{}, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return CopyInfo{}, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return CopyInfo{}, err
	}
	return CopyInfo{Name: name, Size: fi.Size(), ModTime: fi.ModTime()}, nil
}

// Copies lists the database copies in a subdirectory, newest first.
func (s *Store) Copies(subdir string) ([]CopyInfo, error) {
	if subdir != BackupDir && subdir != ArchiveDir {
		return nil, errors.New("invalid directory")
	}
	entries, err := os.ReadDir(filepath.Join(s.dir, subdir))
	if err != nil {
		return nil, err
	}
	var out []CopyInfo
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil || !fi.Mode().IsRegular() || !copyNameRe.MatchString(e.Name()) {
			continue
		}
		out = append(out, CopyInfo{Name: e.Name(), Size: fi.Size(), ModTime: fi.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Name > out[j].Name
	})
	return out, nil
}

// PruneCopies removes all but the newest keep copies whose name starts
// with prefix in a subdirectory, and returns how many were removed.
func (s *Store) PruneCopies(subdir, prefix string, keep int) (int, error) {
	copies, err := s.Copies(subdir)
	if err != nil {
		return 0, err
	}
	removed, kept := 0, 0
	for _, c := range copies {
		if !strings.HasPrefix(c.Name, prefix) {
			continue
		}
		if kept < keep {
			kept++
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, subdir, c.Name)); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// ResetExercise removes everything that belongs to one exercise: reports,
// requests, chat, drafts and read markers. Accounts, templates, saved
// filters, settings and the audit log stay.
func (s *Store) ResetExercise() error {
	db, err := s.conn()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"reports", "requests", "report_seen", "drafts", "chat_messages", "chat_read"} {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ExerciseCounts tells how much a reset would remove.
type ExerciseCounts struct{ Reports, Requests, ChatMessages, Drafts int }

// Counts returns the size of the current exercise.
func (s *Store) Counts() ExerciseCounts {
	db, err := s.conn()
	must(err)
	var c ExerciseCounts
	must(db.QueryRow(`SELECT (SELECT COUNT(*) FROM reports), (SELECT COUNT(*) FROM requests),
		(SELECT COUNT(*) FROM chat_messages), (SELECT COUNT(*) FROM drafts)`).Scan(&c.Reports, &c.Requests, &c.ChatMessages, &c.Drafts))
	return c
}

// BackupFile copies the database of a data directory to dest without
// taking the directory lock, so it can run next to a live server (used by
// the -backup command line option).
func BackupFile(dataDir, dest string) error {
	src := filepath.Join(dataDir, DBName)
	if _, err := os.Stat(src); err != nil {
		return err
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(src)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`VACUUM INTO ?`, dest); err != nil {
		os.Remove(dest)
		return err
	}
	return os.Chmod(dest, 0o600)
}
