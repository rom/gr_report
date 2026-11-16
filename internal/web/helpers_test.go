package web

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rom/gr_report/internal/auth"
	"github.com/rom/gr_report/internal/model"
	"github.com/rom/gr_report/internal/store"
)

// fastHasher keeps the tests quick: 64 KiB and one pass instead of 19 MiB.
var fastHasher = auth.Hasher{MemoryKiB: 64, Time: 1}

type env struct {
	t   testing.TB
	srv *Server
	ts  *httptest.Server
	st  *store.Store
	dir string
	id  map[string]string // username or template name -> ID
}

func pw(name string) string { return "password-" + name }

func newEnv(t testing.TB) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &env{t: t, st: st, dir: dir, id: map[string]string{}}
	for _, u := range []struct {
		name, display string
		role          model.Role
	}{{"admin", "Administrator", model.RoleAdmin}, {"leader", "Exercise leader", model.RoleLeader},
		{"alpha", "Group Alpha", model.RoleGroup}, {"bravo", "Group Bravo", model.RoleGroup}} {
		hash, err := fastHasher.Hash(pw(u.name))
		if err != nil {
			t.Fatal(err)
		}
		id := model.NewID()
		e.id[u.name] = id
		if err := st.PutUser(model.User{ID: id, Username: u.name, DisplayName: u.display, Role: u.role, PassHash: hash}); err != nil {
			t.Fatal(err)
		}
	}
	minimal := model.Template{ID: model.NewID(), Name: "Minimal report"}
	sitrep := model.Template{ID: model.NewID(), Name: "SITREP", DefaultSubject: "SITREP", Fields: []model.Field{
		{Label: "Location", Type: model.FieldText, Required: true},
		{Label: "Status", Type: model.FieldSelect, Options: []string{"Green", "Red"}},
		{Label: "Help needed", Type: model.FieldCheckbox},
		{Label: "Notes", Type: model.FieldTextarea},
		{Label: "When", Type: model.FieldDatetime},
		{Label: "Count", Type: model.FieldNumber},
	}}
	for _, tp := range []model.Template{minimal, sitrep} {
		if err := tp.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := st.PutTemplate(tp); err != nil {
			t.Fatal(err)
		}
		e.id[tp.Name] = tp.ID
	}
	e.srv = New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	e.srv.Hasher = fastHasher
	e.ts = httptest.NewServer(e.srv.Handler())
	t.Cleanup(e.ts.Close)
	return e
}

type client struct {
	e    *env
	http *http.Client
	hdr  map[string]string // sent with every request
}

type result struct {
	status int
	body   string
	path   string // final URL path and query after redirects
	header http.Header
}

func (e *env) anon() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}

// login returns a client with a session for the seeded user.
func (e *env) login(name string) *client {
	e.t.Helper()
	c := e.anon()
	r := c.postRaw("/login", url.Values{"username": {name}, "password": {pw(name)}}, nil)
	if r.status != http.StatusOK || strings.HasPrefix(r.path, "/login") {
		e.t.Fatalf("login %s: status %d at %s", name, r.status, r.path)
	}
	return c
}

func (c *client) do(req *http.Request) result {
	c.e.t.Helper()
	for k, v := range c.hdr {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return result{resp.StatusCode, string(body), resp.Request.URL.RequestURI(), resp.Header}
}

func (c *client) get(path string) result {
	c.e.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, c.e.ts.URL+path, nil)
	return c.do(req)
}

func (c *client) postRaw(path string, form url.Values, hdr map[string]string) result {
	c.e.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, c.e.ts.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return c.do(req)
}

var csrfRe = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func (c *client) csrf() string {
	c.e.t.Helper()
	m := csrfRe.FindStringSubmatch(c.get("/password").body)
	if m == nil {
		c.e.t.Fatal("no CSRF token on /password")
	}
	return m[1]
}

// post submits a form with a valid CSRF token. kv are key/value pairs;
// repeated keys add values.
func (c *client) post(path string, kv ...string) result {
	c.e.t.Helper()
	form := url.Values{"csrf": {c.csrf()}}
	for i := 0; i+1 < len(kv); i += 2 {
		form.Add(kv[i], kv[i+1])
	}
	return c.postRaw(path, form, nil)
}

func (r result) want(t testing.TB, status int, contains ...string) result {
	t.Helper()
	if r.status != status {
		t.Errorf("status = %d, want %d (at %s)", r.status, status, r.path)
	}
	for _, s := range contains {
		if !strings.Contains(r.body, s) {
			t.Errorf("page %s does not contain %q", r.path, s)
		}
	}
	return r
}

func (r result) wantNot(t testing.TB, absent ...string) result {
	t.Helper()
	for _, s := range absent {
		if strings.Contains(r.body, s) {
			t.Errorf("page %s contains %q", r.path, s)
		}
	}
	return r
}

// send submits a minimal report and returns its ID.
func (c *client) send(subject, content string, extra ...string) string {
	c.e.t.Helper()
	kv := append([]string{"template", c.e.id["Minimal report"], "subject", subject, "content", content}, extra...)
	r := c.post("/reports", kv...)
	if r.status != http.StatusOK || !strings.HasPrefix(r.path, "/reports/") {
		c.e.t.Fatalf("send report: status %d at %s", r.status, r.path)
	}
	return strings.TrimPrefix(r.path, "/reports/")
}

// breakWrites makes every INSERT fail while reads keep working, to simulate
// a full disk. It uses a second connection to the same database file.
func (e *env) breakWrites() {
	e.t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(e.dir, store.DBName))
	if err != nil {
		e.t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"users", "templates", "requests", "reports"} {
		q := "CREATE TRIGGER fail_" + table + " BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT, 'simulated write failure'); END"
		if _, err := db.Exec(q); err != nil {
			e.t.Fatal(err)
		}
	}
}
