package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rom/gr_report/internal/auth"
	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/model"
)

func (s *Server) loginForm(c *reqCtx) {
	c.authed = false // the login page never shows the navigation
	s.render(c, http.StatusOK, "login", c.t("Log in"), "", "")
}

func (s *Server) login(c *reqCtx) {
	w, r := c.w, c.r
	c.authed = false
	username := strings.ToLower(strings.TrimSpace(r.PostForm.Get("username")))
	password := r.PostForm.Get("password")
	ip := clientIP(r)
	userKey, ipKey := "u:"+username+"|"+ip, "ip:"+ip

	deny := func(status int, msg string) {
		s.render(c, status, "login", c.t("Log in"), username, c.t(msg))
	}
	for _, chk := range []struct {
		l   *auth.Limiter
		key string
	}{{s.UserLimiter, userKey}, {s.IPLimiter, ipKey}} {
		if ok, wait := chk.l.Allowed(chk.key); !ok {
			s.auditEvent(r, slog.LevelWarn, "login_throttled", username, "")
			w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
			deny(http.StatusTooManyRequests, "Too many failed attempts. Try again later.")
			return
		}
	}
	user, found := s.Store.UserByUsername(username)
	// Verify always runs a key derivation, also for unknown users, so the
	// response time does not reveal which usernames exist.
	valid := auth.Verify(user.PassHash, password) && found && !user.Disabled
	if valid {
		// A member cannot log in while the group is disabled.
		_, valid = s.groupOf(&user)
	}
	if !valid {
		s.UserLimiter.Fail(userKey)
		s.IPLimiter.Fail(ipKey)
		s.auditEvent(r, slog.LevelWarn, "login_failed", username, "")
		deny(http.StatusUnauthorized, "Wrong username or password.")
		return
	}
	s.UserLimiter.Reset(userKey)
	// Upgrade hashes made with the previous scheme or weaker parameters
	// while the password is at hand. Failing to store the new hash must
	// not stop the login.
	if s.Hasher.NeedsRehash(user.PassHash) {
		if hash, err := s.Hasher.Hash(password); err != nil {
			s.Log.Error("rehash", "user", user.Username, "err", err)
		} else {
			user.PassHash = hash
			if err := s.Store.PutUser(user); err != nil {
				s.Log.Error("rehash", "user", user.Username, "err", err)
			} else {
				s.auditEvent(r, slog.LevelInfo, "password_rehashed", user.Username, string(user.Role))
			}
		}
	}
	s.setCookie(w, s.Sessions.Create(user.ID), 0)
	s.auditEvent(r, slog.LevelInfo, "login", user.Username, string(user.Role))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(c *reqCtx) {
	s.Sessions.Delete(c.token)
	s.setCookie(c.w, "", -1)
	s.audit(c, "logout")
	http.Redirect(c.w, c.r, "/login", http.StatusSeeOther)
}

func (s *Server) passwordForm(c *reqCtx) {
	s.render(c, http.StatusOK, "password", c.t("Change password"), nil, "")
}

func (s *Server) passwordChange(c *reqCtx) {
	f := c.r.PostForm
	bad := func(msg string) {
		s.render(c, http.StatusUnprocessableEntity, "password", c.t("Change password"), nil, msg)
	}
	newPw := f.Get("new")
	switch {
	case !auth.Verify(c.user.PassHash, f.Get("current")):
		bad(c.t("The current password is wrong."))
		return
	case newPw != f.Get("confirm"):
		bad(c.t("The new passwords do not match."))
		return
	case newPw == f.Get("current"):
		bad(c.t("The new password must differ from the current one."))
		return
	}
	if err := auth.CheckPolicy(newPw); err != nil {
		bad(i18n.TErr(c.lang, err))
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
	s.redirect(c, "/", c.t("Password changed."))
}

// ---- administration of accounts ----

type usersView struct {
	Users      []model.User
	Self       string
	Roles      []model.Role
	Form       model.User
	Groups     []model.User      // groups a personal account can belong to
	GroupNames map[string]string // group account ID -> display name
}

func (s *Server) usersPage(c *reqCtx, status int, form model.User, errMsg string) {
	v := &usersView{Users: s.Store.Users(), Self: c.user.ID, Form: form, GroupNames: map[string]string{},
		Roles: []model.Role{model.RoleGroup, model.RoleLeader, model.RoleAdmin}}
	for _, u := range v.Users {
		if u.Role == model.RoleGroup && u.GroupID == "" {
			v.Groups = append(v.Groups, u)
			v.GroupNames[u.ID] = u.DisplayName
		}
	}
	s.render(c, status, "users", c.t("Accounts"), v, errMsg)
}

func (s *Server) users(c *reqCtx) {
	s.usersPage(c, http.StatusOK, model.User{Role: model.RoleGroup}, "")
}

func (s *Server) userCreate(c *reqCtx) {
	f := c.r.PostForm
	u := model.User{ID: model.NewID(), Username: f.Get("username"), DisplayName: f.Get("display_name"),
		Role: model.Role(f.Get("role")), MustChange: true, Created: s.Now().UTC(), GroupID: f.Get("group")}
	if err := u.Validate(); err != nil {
		s.usersPage(c, http.StatusUnprocessableEntity, u, i18n.TErr(c.lang, err))
		return
	}
	// A personal account in a group: the group must be a group account.
	if u.GroupID != "" {
		g, ok := s.Store.User(u.GroupID)
		if u.Role != model.RoleGroup || !ok || g.Role != model.RoleGroup || g.GroupID != "" {
			s.usersPage(c, http.StatusUnprocessableEntity, u, c.t("Only an account with the group role can be a member, and only of an existing group."))
			return
		}
	}
	if _, exists := s.Store.UserByUsername(u.Username); exists {
		s.usersPage(c, http.StatusUnprocessableEntity, u, c.t("That username is already in use."))
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
	s.audit(c, "user_created", "target", u.Username, "target_role", string(u.Role), "group", u.GroupID)
	s.redirect(c, "/admin/users", c.t("Account %q created. One-time password: %s — hand it over now, it is not shown again.", u.Username, pw))
}

func (s *Server) userUpdate(c *reqCtx) {
	u, ok := s.Store.User(c.r.PathValue("id"))
	if !ok {
		s.fail(c, http.StatusNotFound, c.t("No such account."))
		return
	}
	flash := ""
	action := c.r.PostForm.Get("action")
	switch action {
	case "reset":
		if u.ID == c.user.ID {
			s.fail(c, http.StatusUnprocessableEntity, c.t("Use Change password for your own account."))
			return
		}
		pw := auth.GeneratePassword()
		hash, err := s.Hasher.Hash(pw)
		if s.storeErr(c, err) {
			return
		}
		u.PassHash, u.MustChange = hash, true
		flash = c.t("New one-time password for %q: %s — hand it over now, it is not shown again.", u.Username, pw)
	case "disable", "enable":
		if u.ID == c.user.ID {
			s.fail(c, http.StatusUnprocessableEntity, c.t("You cannot disable your own account."))
			return
		}
		u.Disabled = action == "disable"
		flash = c.t("Account %q enabled.", u.Username)
		if u.Disabled {
			flash = c.t("Account %q disabled.", u.Username)
		}
	case "rename":
		u.DisplayName = c.r.PostForm.Get("display_name")
		if err := u.Validate(); err != nil {
			s.fail(c, http.StatusUnprocessableEntity, i18n.TErr(c.lang, err))
			return
		}
		flash = c.t("Account %q renamed.", u.Username)
	default:
		s.fail(c, http.StatusBadRequest, c.t("Unknown action."))
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
