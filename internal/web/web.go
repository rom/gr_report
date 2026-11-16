// Package web is the HTTP layer of gr_report: routing, authentication and
// authorisation middleware, server-rendered HTML pages and the handlers.
// Pages are plain HTML forms without JavaScript, which permits a strict
// Content-Security-Policy.
package web

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rom/gr_report/internal/auth"
	"github.com/rom/gr_report/internal/model"
	"github.com/rom/gr_report/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// MaxBodyBytes bounds the size of a request body.
const MaxBodyBytes = 256 << 10

const csp = "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

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

	pages map[string]*template.Template
}

// New returns a Server with default session and throttling settings.
func New(st *store.Store, log *slog.Logger, secure bool) *Server {
	// Many participants can share one address (NAT): the per-address limit
	// is generous and forgets quickly, it only slows password spraying.
	ip := auth.NewLimiter()
	ip.Burst, ip.Forget = 50, 10*time.Minute
	s := &Server{
		Store: st, Sessions: auth.NewSessions(), UserLimiter: auth.NewLimiter(), IPLimiter: ip,
		Log: log, Secure: secure, Now: time.Now,
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
	funcs := template.FuncMap{"ts": fmtTime, "join": strings.Join}
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

// reqCtx is the per-request state handed to authenticated handlers.
type reqCtx struct {
	w     http.ResponseWriter
	r     *http.Request
	user  model.User
	sess  auth.Session
	token string
}

type handler func(c *reqCtx)

// page is the data every HTML page receives.
type page struct {
	Title    string
	User     *model.User
	CSRF     string
	Flash    string
	Error    string
	NewCount int
	Refresh  int
	Data     any
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
		next.ServeHTTP(w, r)
	})
}

// parsePost parses the form of a state changing request and applies the
// origin check. It reports whether the handler may continue.
func (s *Server) parsePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		return true
	}
	if !sameOrigin(r) {
		s.plainError(w, r, http.StatusForbidden, "Cross-site request rejected.")
		return false
	}
	if err := r.ParseForm(); err != nil {
		s.plainError(w, r, http.StatusBadRequest, "The request could not be read or is too large.")
		return false
	}
	return true
}

// authed wraps a handler with session, CSRF and role checks.
func (s *Server) authed(h handler, roles ...model.Role) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &reqCtx{w: w, r: r}
		if ck, err := r.Cookie(s.cookieName()); err == nil {
			c.token = ck.Value
		}
		sess, ok := s.Sessions.Get(c.token)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		user, ok := s.Store.User(sess.UserID)
		if !ok || user.Disabled {
			s.Sessions.Delete(c.token)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		c.sess, c.user = sess, user
		if !s.parsePost(w, r) {
			return
		}
		if r.Method == http.MethodPost && !auth.EqualToken(sess.CSRF, r.PostForm.Get("csrf")) {
			s.fail(c, http.StatusForbidden, "The form has expired. Go back, reload the page and try again.")
			return
		}
		if user.MustChange && r.URL.Path != "/password" && r.URL.Path != "/logout" {
			http.Redirect(w, r, "/password", http.StatusSeeOther)
			return
		}
		if len(roles) > 0 {
			allowed := false
			for _, role := range roles {
				allowed = allowed || role == user.Role
			}
			if !allowed {
				s.fail(c, http.StatusForbidden, "You do not have access to this page.")
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
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// render writes a page for a logged-in user.
func (s *Server) render(c *reqCtx, status int, name, title string, data any, errMsg string) {
	p := &page{Title: title, User: &c.user, CSRF: c.sess.CSRF, Error: errMsg, Data: data}
	if status == http.StatusOK {
		p.Flash = s.Sessions.TakeFlash(c.token)
	}
	if q := queueID(&c.user); q != "" {
		for _, n := range s.Store.Stats(q).New {
			p.NewCount += n
		}
	}
	if v, ok := data.(*queueView); ok && v.P.Auto {
		p.Refresh = 60
	}
	s.write(c.w, status, name, p)
}

func (s *Server) fail(c *reqCtx, status int, msg string) {
	s.render(c, status, "error", http.StatusText(status), nil, msg)
}

// plainError renders an error page without a session.
func (s *Server) plainError(w http.ResponseWriter, _ *http.Request, status int, msg string) {
	s.write(w, status, "error", &page{Title: http.StatusText(status), Error: msg})
}

// storeErr handles a failed write: the user gets a 500 and the cause is
// logged. It reports whether err was non-nil.
func (s *Server) storeErr(c *reqCtx, err error) bool {
	if err == nil {
		return false
	}
	s.Log.Error("store", "err", err, "path", c.r.URL.Path)
	s.fail(c, http.StatusInternalServerError, "The change could not be saved.")
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

	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.login)
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
