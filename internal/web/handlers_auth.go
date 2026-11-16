package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/rom/gr_report/internal/auth"
	"github.com/rom/gr_report/internal/model"
)

func (s *Server) loginForm(w http.ResponseWriter, _ *http.Request) {
	s.write(w, http.StatusOK, "login", &page{Title: "Log in"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.parsePost(w, r) {
		return
	}
	username := strings.ToLower(strings.TrimSpace(r.PostForm.Get("username")))
	password := r.PostForm.Get("password")
	ip := clientIP(r)
	userKey, ipKey := "u:"+username+"|"+ip, "ip:"+ip

	deny := func(status int, msg string) {
		s.write(w, status, "login", &page{Title: "Log in", Error: msg, Data: username})
	}
	for _, chk := range []struct {
		l   *auth.Limiter
		key string
	}{{s.UserLimiter, userKey}, {s.IPLimiter, ipKey}} {
		if ok, wait := chk.l.Allowed(chk.key); !ok {
			s.Log.Warn("audit", "event", "login_throttled", "username", username, "ip", ip)
			w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
			deny(http.StatusTooManyRequests, "Too many failed attempts. Try again later.")
			return
		}
	}
	user, found := s.Store.UserByUsername(username)
	// Verify always runs a key derivation, also for unknown users, so the
	// response time does not reveal which usernames exist.
	if !auth.Verify(user.PassHash, password) || !found || user.Disabled {
		s.UserLimiter.Fail(userKey)
		s.IPLimiter.Fail(ipKey)
		s.Log.Warn("audit", "event", "login_failed", "username", username, "ip", ip)
		deny(http.StatusUnauthorized, "Wrong username or password.")
		return
	}
	s.UserLimiter.Reset(userKey)
	s.setCookie(w, s.Sessions.Create(user.ID), 0)
	s.Log.Info("audit", "event", "login", "user", user.Username, "role", string(user.Role), "ip", ip)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(c *reqCtx) {
	s.Sessions.Delete(c.token)
	s.setCookie(c.w, "", -1)
	s.audit(c, "logout")
	http.Redirect(c.w, c.r, "/login", http.StatusSeeOther)
}

func (s *Server) passwordForm(c *reqCtx) {
	s.render(c, http.StatusOK, "password", "Change password", nil, "")
}

func (s *Server) passwordChange(c *reqCtx) {
	f := c.r.PostForm
	bad := func(msg string) {
		s.render(c, http.StatusUnprocessableEntity, "password", "Change password", nil, msg)
	}
	newPw := f.Get("new")
	switch {
	case !auth.Verify(c.user.PassHash, f.Get("current")):
		bad("The current password is wrong.")
		return
	case newPw != f.Get("confirm"):
		bad("The new passwords do not match.")
		return
	case newPw == f.Get("current"):
		bad("The new password must differ from the current one.")
		return
	}
	if err := auth.CheckPolicy(newPw); err != nil {
		bad(err.Error())
		return
	}
	hash, err := s.Hasher.Hash(newPw)
	if s.storeErr(c, err) {
		return
	}
	u := c.user
	u.PassHash, u.MustChange = hash, false
	if s.storeErr(c, s.Store.PutUser(u)) {
		return
	}
	// End every session of the user, then start a fresh one.
	s.Sessions.DeleteUser(u.ID)
	c.token = s.Sessions.Create(u.ID)
	s.setCookie(c.w, c.token, 0)
	s.audit(c, "password_changed")
	s.redirect(c, "/", "Password changed.")
}

// ---- administration of accounts ----

type usersView struct {
	Users []model.User
	Self  string
	Roles []model.Role
	Form  model.User
}

func (s *Server) usersPage(c *reqCtx, status int, form model.User, errMsg string) {
	v := &usersView{Users: s.Store.Users(), Self: c.user.ID, Form: form,
		Roles: []model.Role{model.RoleGroup, model.RoleLeader, model.RoleAdmin}}
	s.render(c, status, "users", "Accounts", v, errMsg)
}

func (s *Server) users(c *reqCtx) {
	s.usersPage(c, http.StatusOK, model.User{Role: model.RoleGroup}, "")
}

func (s *Server) userCreate(c *reqCtx) {
	f := c.r.PostForm
	u := model.User{ID: model.NewID(), Username: f.Get("username"), DisplayName: f.Get("display_name"),
		Role: model.Role(f.Get("role")), MustChange: true, Created: s.Now().UTC()}
	if err := u.Validate(); err != nil {
		s.usersPage(c, http.StatusUnprocessableEntity, u, err.Error())
		return
	}
	if _, exists := s.Store.UserByUsername(u.Username); exists {
		s.usersPage(c, http.StatusUnprocessableEntity, u, "That username is already in use.")
		return
	}
	pw := auth.GeneratePassword()
	hash, err := s.Hasher.Hash(pw)
	if s.storeErr(c, err) {
		return
	}
	u.PassHash = hash
	if s.storeErr(c, s.Store.PutUser(u)) {
		return
	}
	s.audit(c, "user_created", "target", u.Username, "target_role", string(u.Role))
	s.redirect(c, "/admin/users", fmt.Sprintf("Account %q created. One-time password: %s — hand it over now, it is not shown again.", u.Username, pw))
}

func (s *Server) userUpdate(c *reqCtx) {
	u, ok := s.Store.User(c.r.PathValue("id"))
	if !ok {
		s.fail(c, http.StatusNotFound, "No such account.")
		return
	}
	flash := ""
	action := c.r.PostForm.Get("action")
	switch action {
	case "reset":
		if u.ID == c.user.ID {
			s.fail(c, http.StatusUnprocessableEntity, "Use Change password for your own account.")
			return
		}
		pw := auth.GeneratePassword()
		hash, err := s.Hasher.Hash(pw)
		if s.storeErr(c, err) {
			return
		}
		u.PassHash, u.MustChange = hash, true
		flash = fmt.Sprintf("New one-time password for %q: %s — hand it over now, it is not shown again.", u.Username, pw)
	case "disable", "enable":
		if u.ID == c.user.ID {
			s.fail(c, http.StatusUnprocessableEntity, "You cannot disable your own account.")
			return
		}
		u.Disabled = action == "disable"
		flash = fmt.Sprintf("Account %q %sd.", u.Username, action)
	case "rename":
		u.DisplayName = c.r.PostForm.Get("display_name")
		if err := u.Validate(); err != nil {
			s.fail(c, http.StatusUnprocessableEntity, err.Error())
			return
		}
		flash = fmt.Sprintf("Account %q renamed.", u.Username)
	default:
		s.fail(c, http.StatusBadRequest, "Unknown action.")
		return
	}
	if s.storeErr(c, s.Store.PutUser(u)) {
		return
	}
	if action == "reset" || action == "disable" {
		s.Sessions.DeleteUser(u.ID)
	}
	s.audit(c, "user_"+action, "target", u.Username)
	s.redirect(c, "/admin/users", flash)
}
