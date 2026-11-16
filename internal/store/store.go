// Package store keeps all application state in memory and persists every
// change to an append-only JSON-lines journal that is fsynced before the
// change becomes visible. On start the journal is replayed and compacted.
// See docs/adr/0003-journal-storage.md.
package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/rom/gr_report/internal/model"
)

// Errors returned by the store.
var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("username already exists")
	ErrLocked    = errors.New("data directory is in use by another process")
	ErrClosed    = errors.New("store is closed")
)

const (
	journalName = "journal.jsonl"
	lockName    = "lock"
	maxLine     = 1 << 20

	kindUser     = "user"
	kindTemplate = "template"
	kindRequest  = "request"
	kindReport   = "report"
)

type record struct {
	Kind  string          `json:"k"`
	Value json.RawMessage `json:"v"`
}

// Store is safe for concurrent use.
type Store struct {
	mu        sync.RWMutex
	journal   *os.File
	lock      *os.File
	users     map[string]model.User
	byName    map[string]string
	templates map[string]model.Template
	requests  map[string]model.Request
	reports   map[string]model.Report
}

func newStore() *Store {
	return &Store{
		users:     map[string]model.User{},
		byName:    map[string]string{},
		templates: map[string]model.Template{},
		requests:  map[string]model.Request{},
		reports:   map[string]model.Report{},
	}
}

// Open loads (or creates) the store in dir. Only one process can have a
// data directory open at a time.
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
	s := newStore()
	s.lock = lock
	path := filepath.Join(dir, journalName)
	if f, err := os.Open(path); err == nil {
		err = s.replay(f)
		f.Close()
		if err != nil {
			lock.Close()
			return nil, fmt.Errorf("replay %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		lock.Close()
		return nil, err
	}
	if err := s.compact(dir); err != nil {
		lock.Close()
		return nil, fmt.Errorf("compact journal: %w", err)
	}
	s.journal, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		lock.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the journal and the directory lock.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.journal == nil {
		return nil
	}
	err := s.journal.Close()
	s.journal = nil
	s.lock.Close()
	return err
}

// replay applies all journal records from r. A damaged final line (torn
// write after a crash) is ignored; damage anywhere else is an error.
func (s *Store) replay(r io.Reader) error {
	br := bufio.NewReaderSize(r, 64<<10)
	for n := 1; ; n++ {
		line, err := readLine(br)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("line %d: %w", n, err)
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if aerr := s.applyLine(line); aerr != nil {
			if _, perr := br.Peek(1); perr == io.EOF {
				return nil // torn last record
			}
			return fmt.Errorf("line %d: %w", n, aerr)
		}
	}
}

func readLine(br *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := br.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > maxLine {
			return nil, errors.New("record too long")
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF && len(line) > 0 {
			return line, nil
		}
		return line, err
	}
}

func (s *Store) applyLine(line []byte) error {
	var rec record
	if err := json.Unmarshal(line, &rec); err != nil {
		return err
	}
	switch rec.Kind {
	case kindUser:
		var v model.User
		if err := json.Unmarshal(rec.Value, &v); err != nil {
			return err
		}
		if !model.ValidID(v.ID) || v.Username == "" {
			return errors.New("invalid user record")
		}
		if old, ok := s.users[v.ID]; ok {
			delete(s.byName, old.Username)
		}
		if other, ok := s.byName[v.Username]; ok && other != v.ID {
			return errors.New("duplicate username in journal")
		}
		s.users[v.ID] = v
		s.byName[v.Username] = v.ID
	case kindTemplate:
		var v model.Template
		if err := json.Unmarshal(rec.Value, &v); err != nil {
			return err
		}
		if !model.ValidID(v.ID) {
			return errors.New("invalid template record")
		}
		s.templates[v.ID] = v
	case kindRequest:
		var v model.Request
		if err := json.Unmarshal(rec.Value, &v); err != nil {
			return err
		}
		if !model.ValidID(v.ID) {
			return errors.New("invalid request record")
		}
		s.requests[v.ID] = v
	case kindReport:
		var v model.Report
		if err := json.Unmarshal(rec.Value, &v); err != nil {
			return err
		}
		if !model.ValidID(v.ID) {
			return errors.New("invalid report record")
		}
		s.reports[v.ID] = v
	default:
		return fmt.Errorf("unknown record kind %q", rec.Kind)
	}
	return nil
}

func encode(kind string, v any) ([]byte, error) {
	val, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	line, err := json.Marshal(record{Kind: kind, Value: val})
	if err != nil {
		return nil, err
	}
	if len(line)+1 > maxLine {
		return nil, errors.New("record too long")
	}
	return append(line, '\n'), nil
}

// compact rewrites the journal so it holds exactly one record per entity.
func (s *Store) compact(dir string) error {
	tmp, err := os.CreateTemp(dir, journalName+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	w := bufio.NewWriter(tmp)
	write := func(kind string, v any) {
		if err != nil {
			return
		}
		var line []byte
		if line, err = encode(kind, v); err == nil {
			_, err = w.Write(line)
		}
	}
	for _, id := range sortedKeys(s.users) {
		write(kindUser, s.users[id])
	}
	for _, id := range sortedKeys(s.templates) {
		write(kindTemplate, s.templates[id])
	}
	for _, id := range sortedKeys(s.requests) {
		write(kindRequest, s.requests[id])
	}
	for _, id := range sortedKeys(s.reports) {
		write(kindReport, s.reports[id])
	}
	if err == nil {
		err = w.Flush()
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, journalName)); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// append makes the record durable. The caller holds s.mu for writing and
// updates the in-memory state only when append succeeds.
func (s *Store) append(kind string, v any) error {
	if s.journal == nil {
		return ErrClosed
	}
	line, err := encode(kind, v)
	if err != nil {
		return err
	}
	if _, err := s.journal.Write(line); err != nil {
		return err
	}
	return s.journal.Sync()
}

// ---- users ----

// PutUser creates or replaces a user. Usernames are unique.
func (s *Store) PutUser(u model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !model.ValidID(u.ID) || u.Username == "" {
		return errors.New("invalid user")
	}
	if other, ok := s.byName[u.Username]; ok && other != u.ID {
		return ErrDuplicate
	}
	if err := s.append(kindUser, u); err != nil {
		return err
	}
	if old, ok := s.users[u.ID]; ok {
		delete(s.byName, old.Username)
	}
	s.users[u.ID] = u
	s.byName[u.Username] = u.ID
	return nil
}

// User returns the user with the given ID.
func (s *Store) User(id string) (model.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	return u, ok
}

// UserByUsername returns the user with the given (lower-case) username.
func (s *Store) UserByUsername(name string) (model.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[s.byName[strings.ToLower(name)]]
	return u, ok
}

// Users returns all users sorted by role and display name.
func (s *Store) Users() []model.User {
	s.mu.RLock()
	out := make([]model.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	s.mu.RUnlock()
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

// PutTemplate creates or replaces a template.
func (s *Store) PutTemplate(t model.Template) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !model.ValidID(t.ID) {
		return errors.New("invalid template")
	}
	t = t.Clone()
	if err := s.append(kindTemplate, t); err != nil {
		return err
	}
	s.templates[t.ID] = t
	return nil
}

// Template returns the template with the given ID.
func (s *Store) Template(id string) (model.Template, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.templates[id]
	return t.Clone(), ok
}

// Templates returns the templates sorted by name. Archived templates are
// included only when all is true.
func (s *Store) Templates(all bool) []model.Template {
	s.mu.RLock()
	out := make([]model.Template, 0, len(s.templates))
	for _, t := range s.templates {
		if all || !t.Archived {
			out = append(out, t.Clone())
		}
	}
	s.mu.RUnlock()
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

// PutRequest creates or replaces an information request.
func (s *Store) PutRequest(q model.Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !model.ValidID(q.ID) {
		return errors.New("invalid request")
	}
	q = q.Clone()
	if err := s.append(kindRequest, q); err != nil {
		return err
	}
	s.requests[q.ID] = q
	return nil
}

// Request returns the request with the given ID.
func (s *Store) Request(id string) (model.Request, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	q, ok := s.requests[id]
	return q.Clone(), ok
}

// Requests returns all requests, newest first.
func (s *Store) Requests() []model.Request {
	s.mu.RLock()
	out := make([]model.Request, 0, len(s.requests))
	for _, q := range s.requests {
		out = append(out, q.Clone())
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.After(out[j].Created)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ---- reports ----

// PutReport stores a report.
func (s *Store) PutReport(r model.Report) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !model.ValidID(r.ID) {
		return errors.New("invalid report")
	}
	r = r.Clone()
	if err := s.append(kindReport, r); err != nil {
		return err
	}
	s.reports[r.ID] = r
	return nil
}

// Report returns the report with the given ID.
func (s *Store) Report(id string) (model.Report, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.reports[id]
	return r.Clone(), ok
}

// UpdateReport atomically modifies a report. If fn returns false nothing is
// written.
func (s *Store) UpdateReport(id string, fn func(*model.Report) bool) (model.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.reports[id]
	if !ok {
		return model.Report{}, ErrNotFound
	}
	r := cur.Clone()
	if !fn(&r) {
		return cur.Clone(), nil
	}
	r.ID = id
	if err := s.append(kindReport, r); err != nil {
		return model.Report{}, err
	}
	s.reports[id] = r
	return r.Clone(), nil
}

// Reports returns the reports matching f (nil matches all), unsorted.
func (s *Store) Reports(f *model.ReportFilter) []model.Report {
	if f != nil {
		f.Prepare()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []model.Report
	for _, r := range s.reports {
		if f == nil || f.Match(&r) {
			out = append(out, r.Clone())
		}
	}
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
	folders := map[string]bool{}
	s.mu.RLock()
	for _, r := range s.reports {
		if r.ReceiverID != receiverID {
			continue
		}
		st.Total[r.SenderID]++
		if r.Status == model.StatusNew {
			st.New[r.SenderID]++
		}
		if r.Folder != "" {
			folders[r.Folder] = true
		}
	}
	s.mu.RUnlock()
	st.Folders = sortedKeys(folders)
	return st
}
