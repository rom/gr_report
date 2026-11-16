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
`}

// Store is safe for concurrent use.
type Store struct {
	db   *sql.DB
	lock *os.File
}

// Open opens (or creates) the database in dir and brings its schema up to
// date. Only one process can have a data directory open at a time.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
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
	s := &Store{db: db, lock: lock}
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

const userCols = "id, username, display_name, role, pass_hash, disabled, must_change, created"

func scanUser(sc interface{ Scan(...any) error }) (model.User, error) {
	var u model.User
	var created int64
	err := sc.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.PassHash, &u.Disabled, &u.MustChange, &created)
	u.Created = fromUnix(created)
	return u, err
}

// PutUser creates or replaces a user. Usernames are unique.
func (s *Store) PutUser(u model.User) error {
	if !model.ValidID(u.ID) || u.Username == "" {
		return errors.New("invalid user")
	}
	err := s.exec(`INSERT INTO users (`+userCols+`) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET username=excluded.username, display_name=excluded.display_name,
		role=excluded.role, pass_hash=excluded.pass_hash, disabled=excluded.disabled,
		must_change=excluded.must_change, created=excluded.created`,
		u.ID, u.Username, u.DisplayName, string(u.Role), u.PassHash, b2i(u.Disabled), b2i(u.MustChange), unix(u.Created))
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

// Groups returns all enabled accounts with the group role.
func (s *Store) Groups() []model.User {
	var out []model.User
	for _, u := range s.Users() {
		if u.Role == model.RoleGroup && !u.Disabled {
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

const requestCols = "id, subject, body, template_id, recipients, created_by, created, closed"

func scanRequest(sc interface{ Scan(...any) error }) (model.Request, error) {
	var q model.Request
	var recipients string
	var created int64
	if err := sc.Scan(&q.ID, &q.Subject, &q.Body, &q.TemplateID, &recipients, &q.CreatedBy, &created, &q.Closed); err != nil {
		return q, err
	}
	q.Created = fromUnix(created)
	return q, json.Unmarshal([]byte(recipients), &q.Recipients)
}

// PutRequest creates or replaces an information request.
func (s *Store) PutRequest(q model.Request) error {
	if !model.ValidID(q.ID) {
		return errors.New("invalid request")
	}
	return s.exec(`INSERT OR REPLACE INTO requests (`+requestCols+`) VALUES (?,?,?,?,?,?,?,?)`,
		q.ID, q.Subject, q.Body, q.TemplateID, toJSON(q.Recipients), q.CreatedBy, unix(q.Created), b2i(q.Closed))
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
	"template_id, template_name, fields, request_id, status, folder"

func scanReport(sc interface{ Scan(...any) error }) (model.Report, error) {
	var r model.Report
	var fields string
	var ts int64
	if err := sc.Scan(&r.ID, &ts, &r.SenderID, &r.SenderName, &r.Subject, &r.Content, &r.ReceiverID, &r.ReceiverName,
		&r.TemplateID, &r.TemplateName, &fields, &r.RequestID, &r.Status, &r.Folder); err != nil {
		return r, err
	}
	r.Timestamp = fromUnix(ts)
	return r, json.Unmarshal([]byte(fields), &r.Fields)
}

// searchText is the lower-cased haystack for full text search. Lower-casing
// happens in Go because SQLite only folds ASCII (Å, Ä and Ö matter here).
func searchText(r *model.Report) string {
	parts := []string{r.Subject, r.Content, r.SenderName, r.TemplateName, r.Folder}
	for _, f := range r.Fields {
		parts = append(parts, f.Value)
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

const putReportSQL = `INSERT OR REPLACE INTO reports (` + reportCols + `, search_text) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

func reportArgs(r *model.Report) []any {
	return []any{r.ID, unix(r.Timestamp), r.SenderID, r.SenderName, r.Subject, r.Content, r.ReceiverID, r.ReceiverName,
		r.TemplateID, r.TemplateName, toJSON(r.Fields), r.RequestID, string(r.Status), r.Folder, searchText(r)}
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
	}
	q := "SELECT " + reportCols + " FROM reports"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	rows := s.query(q, args...)
	defer rows.Close()
	var out []model.Report
	for rows.Next() {
		r, err := scanReport(rows)
		must(err)
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

// Stats computes per-sender counters and the folder list for a receiver.
func (s *Store) Stats(receiverID string) QueueStats {
	st := QueueStats{Total: map[string]int{}, New: map[string]int{}}
	rows := s.query(`SELECT sender_id, COUNT(*), COALESCE(SUM(status = 'new'), 0)
		FROM reports WHERE receiver_id = ? GROUP BY sender_id`, receiverID)
	for rows.Next() {
		var sender string
		var total, unread int
		must(rows.Scan(&sender, &total, &unread))
		st.Total[sender] = total
		if unread > 0 {
			st.New[sender] = unread
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

// NewCount returns the number of new reports in a receiver's queue.
func (s *Store) NewCount(receiverID string) int {
	db, err := s.conn()
	must(err)
	var n int
	must(db.QueryRow(`SELECT COUNT(*) FROM reports WHERE receiver_id = ? AND status = 'new'`, receiverID).Scan(&n))
	return n
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
	res, err := db.Exec(`INSERT INTO chat_messages (conv, party_a, party_b, ts, sender_party, sender_name, body) VALUES (?,?,?,?,?,?,?)`,
		m.Conv, a, b, unix(m.Timestamp), m.SenderParty, m.SenderName, m.Body)
	if err != nil {
		return m, err
	}
	m.Seq, err = res.LastInsertId()
	return m, err
}

// ChatMessages returns up to limit messages of a conversation in
// chronological order: those after sequence number after when after > 0
// (the oldest such), otherwise the newest ones before sequence number
// before (0: the newest of all).
func (s *Store) ChatMessages(conv string, after, before int64, limit int) []model.ChatMessage {
	q := `SELECT seq, conv, ts, sender_party, sender_name, body FROM chat_messages WHERE conv = ?`
	args := []any{conv}
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
		must(rows.Scan(&m.Seq, &m.Conv, &ts, &m.SenderParty, &m.SenderName, &m.Body))
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
