// Package web is the HTTP layer of gr_report: routing, authentication and
// authorisation middleware, server-rendered HTML pages and the handlers.
// Pages are plain HTML forms that work without JavaScript; one same-origin
// script adds conveniences on top. Texts are translated per request and the
// colour theme is a per-browser preference.
package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rom/gr_report/internal/auth"
	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/model"
	"github.com/rom/gr_report/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// MaxBodyBytes bounds the size of a request body.
const MaxBodyBytes = 256 << 10

// Only same-origin resources and no inline script or style: injected markup
// cannot run code even if an escaping bug slipped through.
const csp = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

// Theme is a colour theme; its name is the value of <html data-theme>.
type Theme struct {
	Name  string
	Label i18n.Label
}

// Themes lists the selectable themes. "auto" follows the operating system
// (light, dark, or high contrast when the user asks for more contrast).
var Themes = []Theme{
	{"auto", "System default"},
	{"light", "Light"},
	{"dark", "Dark"},
	{"contrast", "High contrast"},
	{"gold", "Gold"},
	{"spring", "Spring"},
	{"autumn", "Autumn"},
	{"camouflage", "Camouflage"},
	{"console", "Console green"},
}

// DefaultTheme is used until the user chooses one.
const DefaultTheme = "auto"

func validTheme(name string) bool {
	for _, t := range Themes {
		if t.Name == name {
			return true
		}
	}
	return false
}

// Preference cookies. They are per browser, not per account, because a
// group account is shared by several people.
const (
	langCookie  = "gr_lang"
	themeCookie = "gr_theme"
)

// Server holds the dependencies of the HTTP handlers.
type Server struct {
	Store    *store.Store
	Sessions *auth.Sessions
	// UserLimiter throttles per username and address, IPLimiter per address.
	UserLimiter *auth.Limiter
	IPLimiter   *auth.Limiter
	Hasher      auth.Hasher
	Log         *slog.Logger
	// Secure is set when the server is reached over TLS. It enables Secure
	// cookies with the __Host- prefix and HSTS.
	Secure bool
	Now    func() time.Time
	// DefaultLang is used when neither a cookie nor the browser's
	// Accept-Language header selects a supported language.
	DefaultLang i18n.Lang
	// Chat enables the chat between the participants.
	Chat bool

	chatLimit chatLimiter
	pages     map[string]*template.Template
}

// New returns a Server with default session and throttling settings.
func New(st *store.Store, log *slog.Logger, secure bool) *Server {
	// Many participants can share one address (NAT): the per-address limit
	// is generous and forgets quickly, it only slows password spraying.
	ip := auth.NewLimiter()
	ip.Burst, ip.Forget = 50, 10*time.Minute
	s := &Server{
		Store: st, Sessions: auth.NewSessions(), UserLimiter: auth.NewLimiter(), IPLimiter: ip,
		Log: log, Secure: secure, Now: time.Now, DefaultLang: i18n.EN, Chat: true,
	}
	s.pages = parsePages()
	return s
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04:05") + "Z"
}

func parsePages() map[string]*template.Template {
	funcs := template.FuncMap{"ts": fmtTime, "join": strings.Join, "inc": func(i int) int { return i + 1 }}
	entries, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		panic(err)
	}
	pages := map[string]*template.Template{}
	for _, e := range entries {
		name := strings.TrimSuffix(strings.TrimPrefix(e, "templates/"), ".html")
		if name == "layout" {
			continue
		}
		pages[name] = template.Must(template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", e))
	}
	return pages
}

// reqCtx is the per-request state handed to handlers.
type reqCtx struct {
	w      http.ResponseWriter
	r      *http.Request
	user   model.User
	authed bool // a valid session exists; user and sess are set
	sess   auth.Session
	token  string
	lang   i18n.Lang
	theme  string
}

// t translates a message into the language of the request.
func (c *reqCtx) t(key string, args ...any) string { return i18n.T(c.lang, key, args...) }

type handler func(c *reqCtx)

// page is the data every HTML page receives.
type page struct {
	Title    string
	Lang     i18n.Lang
	Theme    string
	Path     string
	User     *model.User
	CSRF     string
	Flash    string
	Error    string
	NewCount int
	Chat     bool // show the chat in the navigation
	ChatNew  int  // unread chat messages
	Refresh  int
	Data     any
}

// T translates a message into the language of the page.
func (p *page) T(key string, args ...any) string { return i18n.T(p.Lang, key, args...) }

// Nav reports whether the page belongs to the navigation entry at prefix.
func (p *page) Nav(prefix string) bool {
	return p.Path == prefix || strings.HasPrefix(p.Path, prefix+"/")
}

// Tr translates a value that is stored in English: a role, a report
// status, a field type or a label.
func (p *page) Tr(v any) string {
	switch x := v.(type) {
	case model.Role:
		return p.T(string(x))
	case model.Status:
		return p.T(string(x))
	case model.FieldType:
		return p.T(string(x))
	case i18n.Label:
		return p.T(string(x))
	case string:
		return p.T(x)
	}
	return ""
}

// prefs returns the language and theme for a request: the preference
// cookie wins, then the browser's language, then the server default.
func (s *Server) prefs(r *http.Request) (i18n.Lang, string) {
	lang := i18n.Match(r.Header.Get("Accept-Language"), s.DefaultLang)
	if ck, err := r.Cookie(langCookie); err == nil {
		if l, ok := i18n.Parse(ck.Value); ok {
			lang = l
		}
	}
	theme := DefaultTheme
	if ck, err := r.Cookie(themeCookie); err == nil && validTheme(ck.Value) {
		theme = ck.Value
	}
	return lang, theme
}

func (s *Server) newCtx(w http.ResponseWriter, r *http.Request) *reqCtx {
	c := &reqCtx{w: w, r: r}
	c.lang, c.theme = s.prefs(r)
	return c
}

func (s *Server) cookieName() string {
	if s.Secure {
		return "__Host-gr_session"
	}
	return "gr_session"
}

func (s *Server) setCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(), Value: token, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteStrictMode,
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// sameOrigin rejects cross-site state changing requests. Browsers send
// Sec-Fetch-Site or Origin; other clients are covered by the CSRF token.
func sameOrigin(r *http.Request) bool {
	if v := r.Header.Get("Sec-Fetch-Site"); v != "" {
		return v == "same-origin" || v == "none"
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err == nil && u.Host == r.Host
	}
	return true
}

func (s *Server) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
		h.Set("Cache-Control", "no-store")
		if s.Secure {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
		// A failed database read surfaces as a panic (see package store):
		// answer with an error page instead of dropping the connection.
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			s.Log.Error("panic", "err", v, "path", r.URL.Path)
			c := s.newCtx(w, r)
			s.fail(c, http.StatusInternalServerError,
				c.t("Something went wrong on the server. Try again; if it keeps happening, tell the administrator."))
		}()
		next.ServeHTTP(w, r)
	})
}

// parsePost parses the form of a state changing request and applies the
// origin check. It reports whether the handler may continue.
func (s *Server) parsePost(c *reqCtx) bool {
	if c.r.Method != http.MethodPost {
		return true
	}
	if !sameOrigin(c.r) {
		s.fail(c, http.StatusForbidden, c.t("Cross-site request rejected."))
		return false
	}
	if err := c.r.ParseForm(); err != nil {
		s.fail(c, http.StatusBadRequest, c.t("The request could not be read or is too large."))
		return false
	}
	return true
}

// session loads the session and user of the request, if any. A session of
// a deleted or disabled account is ended.
func (s *Server) session(c *reqCtx) bool {
	if ck, err := c.r.Cookie(s.cookieName()); err == nil {
		c.token = ck.Value
	}
	sess, ok := s.Sessions.Get(c.token)
	if !ok {
		return false
	}
	user, ok := s.Store.User(sess.UserID)
	if !ok || user.Disabled {
		s.Sessions.Delete(c.token)
		return false
	}
	c.sess, c.user, c.authed = sess, user, true
	return true
}

func (s *Server) checkCSRF(c *reqCtx) bool {
	if c.r.Method == http.MethodPost && !auth.EqualToken(c.sess.CSRF, c.r.PostForm.Get("csrf")) {
		s.fail(c, http.StatusForbidden, c.t("The form has expired. Go back, reload the page and try again."))
		return false
	}
	return true
}

// open wraps a handler that works with or without a session.
func (s *Server) open(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := s.newCtx(w, r)
		s.session(c)
		if !s.parsePost(c) || (c.authed && !s.checkCSRF(c)) {
			return
		}
		h(c)
	}
}

// authed wraps a handler with session, CSRF and role checks.
func (s *Server) authed(h handler, roles ...model.Role) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := s.newCtx(w, r)
		if !s.session(c) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if !s.parsePost(c) || !s.checkCSRF(c) {
			return
		}
		if c.user.MustChange && r.URL.Path != "/password" && r.URL.Path != "/logout" {
			http.Redirect(w, r, "/password", http.StatusSeeOther)
			return
		}
		if len(roles) > 0 {
			allowed := false
			for _, role := range roles {
				allowed = allowed || role == c.user.Role
			}
			if !allowed {
				s.fail(c, http.StatusForbidden, c.t("You do not have access to this page."))
				return
			}
		}
		h(c)
	}
}

// queueID returns the receiver ID of the queue the user works on: leaders
// share one queue, each group has its own inbox.
func queueID(u *model.User) string {
	switch u.Role {
	case model.RoleLeader:
		return model.ReceiverLeader
	case model.RoleGroup:
		return u.ID
	}
	return ""
}

func (s *Server) write(w http.ResponseWriter, status int, name string, p *page) {
	var buf bytes.Buffer
	if err := s.pages[name].Execute(&buf, p); err != nil {
		s.Log.Error("template", "page", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Language", string(p.Lang))
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// render writes a page. It works for anonymous requests too.
func (s *Server) render(c *reqCtx, status int, name, title string, data any, errMsg string) {
	p := &page{Title: title, Lang: c.lang, Theme: c.theme, Path: c.r.URL.Path, Error: errMsg, Data: data}
	if c.authed {
		p.User, p.CSRF = &c.user, c.sess.CSRF
		if status == http.StatusOK {
			p.Flash = s.Sessions.TakeFlash(c.token)
		}
		// Not on a server error page: the database may be what failed.
		if q := queueID(&c.user); q != "" && status < http.StatusInternalServerError {
			p.NewCount = s.Store.NewCount(q)
			p.Chat, p.ChatNew = s.Chat, s.chatUnreadTotal(&c.user)
		}
	}
	if v, ok := data.(*queueView); ok && v.P.Auto {
		p.Refresh = 60
	}
	s.write(c.w, status, name, p)
}

func (s *Server) fail(c *reqCtx, status int, msg string) {
	s.render(c, status, "error", c.t(http.StatusText(status)), nil, msg)
}

// storeErr handles a failed write: the user gets a 500 and the cause is
// logged. It reports whether err was non-nil.
func (s *Server) storeErr(c *reqCtx, err error) bool {
	if err == nil {
		return false
	}
	s.Log.Error("store", "err", err, "path", c.r.URL.Path)
	s.fail(c, http.StatusInternalServerError, c.t("The change could not be saved."))
	return true
}

func (s *Server) audit(c *reqCtx, event string, attrs ...any) {
	base := []any{"event", event, "user", c.user.Username, "role", string(c.user.Role), "ip", clientIP(c.r)}
	s.Log.Info("audit", append(base, attrs...)...)
}

func (s *Server) redirect(c *reqCtx, to, flash string) {
	if flash != "" {
		s.Sessions.SetFlash(c.token, flash)
	}
	http.Redirect(c.w, c.r, to, http.StatusSeeOther)
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.StripPrefix("/static/", http.FileServerFS(static))
	mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fileServer.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /login", s.open(s.loginForm))
	mux.HandleFunc("POST /login", s.open(s.login))
	mux.HandleFunc("GET /settings", s.open(s.settingsForm))
	mux.HandleFunc("POST /settings", s.open(s.settingsSave))
	mux.HandleFunc("POST /logout", s.authed(s.logout))
	mux.HandleFunc("GET /password", s.authed(s.passwordForm))
	mux.HandleFunc("POST /password", s.authed(s.passwordChange))
	mux.HandleFunc("GET /{$}", s.authed(s.root))

	group, leader, admin := model.RoleGroup, model.RoleLeader, model.RoleAdmin
	mux.HandleFunc("GET /home", s.authed(s.home, group))
	mux.HandleFunc("GET /reports/new", s.authed(s.reportForm, group))
	mux.HandleFunc("POST /reports", s.authed(s.reportCreate, group))
	mux.HandleFunc("GET /reports/{id}", s.authed(s.reportView, group, leader))
	mux.HandleFunc("POST /reports/{id}/status", s.authed(s.reportStatus, group, leader))
	mux.HandleFunc("GET /queue", s.authed(s.queue, group, leader))
	mux.HandleFunc("POST /queue/bulk", s.authed(s.queueBulk, group, leader))
	mux.HandleFunc("GET /sent", s.authed(s.sent, group))
	mux.HandleFunc("GET /api/new-count", s.authed(s.newCount, group, leader))

	mux.HandleFunc("GET /chat", s.authed(s.chatEnabled(s.chatRoot), group, leader))
	mux.HandleFunc("GET /chat/{peer}", s.authed(s.chatEnabled(s.chat), group, leader))
	mux.HandleFunc("POST /chat/{peer}", s.authed(s.chatEnabled(s.chatSend), group, leader))
	mux.HandleFunc("GET /api/chat/{peer}", s.authed(s.chatEnabled(s.chatPoll), group, leader))

	mux.HandleFunc("GET /requests", s.authed(s.requests, group, leader))
	mux.HandleFunc("GET /requests/new", s.authed(s.requestForm, leader))
	mux.HandleFunc("POST /requests", s.authed(s.requestCreate, leader))
	mux.HandleFunc("GET /requests/{id}", s.authed(s.requestView, group, leader))
	mux.HandleFunc("POST /requests/{id}/close", s.authed(s.requestClose, leader))

	mux.HandleFunc("GET /templates", s.authed(s.templates, leader, admin))
	mux.HandleFunc("GET /templates/new", s.authed(s.templateForm, leader, admin))
	mux.HandleFunc("GET /templates/{id}", s.authed(s.templateForm, leader, admin))
	mux.HandleFunc("POST /templates", s.authed(s.templateSave, leader, admin))
	mux.HandleFunc("POST /templates/{id}", s.authed(s.templateSave, leader, admin))
	mux.HandleFunc("POST /templates/{id}/archive", s.authed(s.templateArchive, leader, admin))

	mux.HandleFunc("GET /admin/users", s.authed(s.users, admin))
	mux.HandleFunc("POST /admin/users", s.authed(s.userCreate, admin))
	mux.HandleFunc("POST /admin/users/{id}", s.authed(s.userUpdate, admin))

	return s.headers(mux)
}

func (s *Server) root(c *reqCtx) {
	switch c.user.Role {
	case model.RoleLeader:
		http.Redirect(c.w, c.r, "/queue", http.StatusSeeOther)
	case model.RoleAdmin:
		http.Redirect(c.w, c.r, "/admin/users", http.StatusSeeOther)
	default:
		http.Redirect(c.w, c.r, "/home", http.StatusSeeOther)
	}
}

// newCount serves the number of new reports in the user's queue and of
// unread chat messages to the page script, which updates the counters and
// shows a notice when reports arrive.
func (s *Server) newCount(c *reqCtx) {
	c.w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(c.w).Encode(map[string]int{
		"new": s.Store.NewCount(queueID(&c.user)), "chat": s.chatUnreadTotal(&c.user)})
}

// ---- language and theme ----

type settingsView struct {
	Languages []i18n.Lang
	Themes    []Theme
}

func (s *Server) settingsForm(c *reqCtx) {
	s.render(c, http.StatusOK, "settings", c.t("Settings"), &settingsView{Languages: i18n.Languages, Themes: Themes}, "")
}

func (s *Server) settingsSave(c *reqCtx) {
	f := c.r.PostForm
	set := func(name, value string) {
		http.SetCookie(c.w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 365 * 24 * 3600,
			HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode})
	}
	if l, ok := i18n.Parse(f.Get("lang")); ok {
		set(langCookie, string(l))
		c.lang = l
	}
	if th := f.Get("theme"); validTheme(th) {
		set(themeCookie, th)
	}
	if c.authed {
		s.Sessions.SetFlash(c.token, c.t("Settings saved."))
	}
	http.Redirect(c.w, c.r, "/settings", http.StatusSeeOther)
}
