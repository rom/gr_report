package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rom/gr_report/internal/model"
)

func TestLoginLogoutAndHeaders(t *testing.T) {
	e := newEnv(t)
	c := e.anon()
	r := c.get("/").want(t, 200, `action="/login"`)
	if r.path != "/login" {
		t.Errorf("anonymous / ended at %s", r.path)
	}
	for h, want := range map[string]string{
		"Content-Security-Policy": csp, "X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY",
		"Referrer-Policy": "no-referrer", "Cache-Control": "no-store", "Cross-Origin-Opener-Policy": "same-origin",
	} {
		if got := r.header.Get(h); got != want {
			t.Errorf("%s = %q, want %q", h, got, want)
		}
	}
	if r.header.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS sent over plain HTTP")
	}
	if strings.Contains(csp, "unsafe") || strings.Contains(csp, "script-src") {
		t.Error("CSP must not allow scripts")
	}

	c.postRaw("/login", url.Values{"username": {"leader"}, "password": {"wrong"}}, nil).
		want(t, 401, "Wrong username or password", `value="leader"`)
	c.postRaw("/login", url.Values{"username": {"nobody"}, "password": {"wrong"}}, nil).
		want(t, 401, "Wrong username or password")
	c.get("/queue").want(t, 200, `action="/login"`)

	r = c.postRaw("/login", url.Values{"username": {" Leader "}, "password": {pw("leader")}}, nil).want(t, 200, "Report queue", "Exercise leader")
	if r.path != "/queue" {
		t.Errorf("leader landed on %s", r.path)
	}
	if got := e.login("alpha").get("/").path; got != "/home" {
		t.Errorf("group landed on %s", got)
	}
	if got := e.login("admin").get("/").path; got != "/admin/users" {
		t.Errorf("admin landed on %s", got)
	}

	c.post("/logout").want(t, 200, `action="/login"`)
	if got := c.get("/queue").path; got != "/login" {
		t.Errorf("after logout /queue ended at %s", got)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	for _, secure := range []bool{false, true} {
		e := newEnv(t)
		e.srv.Secure = secure
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"username": {"alpha"}, "password": {pw("alpha")}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.srv.Handler().ServeHTTP(rec, req)
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("secure=%v: %d cookies", secure, len(cookies))
		}
		ck := cookies[0]
		if !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode || ck.Path != "/" || ck.Secure != secure || ck.MaxAge != 0 {
			t.Errorf("secure=%v: cookie = %+v", secure, ck)
		}
		if secure != strings.HasPrefix(ck.Name, "__Host-") {
			t.Errorf("secure=%v: cookie name %q", secure, ck.Name)
		}
		if got := rec.Header().Get("Strict-Transport-Security"); secure != (got != "") {
			t.Errorf("secure=%v: HSTS = %q", secure, got)
		}
		if strings.Contains(rec.Body.String(), ck.Value) {
			t.Error("session token leaked into the page")
		}
	}
}

func TestLoginThrottle(t *testing.T) {
	e := newEnv(t)
	c := e.anon()
	bad := url.Values{"username": {"alpha"}, "password": {"wrong"}}
	for i := 0; i < e.srv.UserLimiter.Burst; i++ {
		c.postRaw("/login", bad, nil).want(t, 401)
	}
	r := c.postRaw("/login", url.Values{"username": {"alpha"}, "password": {pw("alpha")}}, nil).want(t, 429, "Too many failed attempts")
	if r.header.Get("Retry-After") == "" {
		t.Error("no Retry-After")
	}
	// Other accounts are still usable from the same address ...
	e.login("bravo")
	// ... until the address itself tries too many names.
	e.srv.IPLimiter.Burst = 3
	for i := 0; i < 3; i++ {
		c.postRaw("/login", url.Values{"username": {fmt.Sprint("user", i)}, "password": {"x"}}, nil)
	}
	c.postRaw("/login", url.Values{"username": {"bravo"}, "password": {pw("bravo")}}, nil).want(t, 429)
	// The lock expires.
	now := time.Now().Add(time.Hour)
	e.srv.UserLimiter.Now = func() time.Time { return now }
	e.srv.IPLimiter.Now = func() time.Time { return now }
	e.login("alpha")
}

func TestCSRFAndOrigin(t *testing.T) {
	e := newEnv(t)
	c := e.login("alpha")
	form := func(csrf string) url.Values {
		return url.Values{"csrf": {csrf}, "template": {e.id["Minimal report"]}, "subject": {"s"}, "content": {"c"}}
	}
	c.postRaw("/reports", form(""), nil).want(t, 403, "form has expired")
	c.postRaw("/reports", form("forged"), nil).want(t, 403)
	other := e.login("bravo").csrf()
	c.postRaw("/reports", form(other), nil).want(t, 403)
	tok := c.csrf()
	c.postRaw("/reports", form(tok), map[string]string{"Sec-Fetch-Site": "cross-site"}).want(t, 403, "Cross-site request rejected")
	c.postRaw("/reports", form(tok), map[string]string{"Origin": "https://evil.example"}).want(t, 403)
	c.postRaw("/reports", form(tok), map[string]string{"Origin": "://bad"}).want(t, 403)
	c.postRaw("/login", url.Values{"username": {"alpha"}, "password": {pw("alpha")}}, map[string]string{"Sec-Fetch-Site": "cross-site"}).want(t, 403)
	if n := len(e.st.Reports(nil)); n != 0 {
		t.Fatalf("%d reports created by rejected requests", n)
	}
	c.postRaw("/reports", form(tok), map[string]string{"Sec-Fetch-Site": "same-origin"}).want(t, 200, "Report sent")
	c.postRaw("/reports", form(tok), map[string]string{"Origin": e.ts.URL}).want(t, 200, "Report sent")
	if n := len(e.st.Reports(nil)); n != 2 {
		t.Errorf("%d reports, want 2", n)
	}
}

func TestRoleAccess(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		path                 string
		admin, leader, group int
	}{
		{"/home", 403, 403, 200},
		{"/queue", 403, 200, 200},
		{"/sent", 403, 403, 200},
		{"/requests", 403, 200, 200},
		{"/requests/new", 403, 200, 403},
		{"/templates", 200, 200, 403},
		{"/templates/new", 200, 200, 403},
		{"/templates/" + e.id["SITREP"], 200, 200, 403},
		{"/admin/users", 200, 403, 403},
		{"/reports/new?template=" + e.id["SITREP"], 403, 403, 200},
		{"/password", 200, 200, 200},
		{"/healthz", 200, 200, 200},
		{"/nonexistent", 404, 404, 404},
	}
	clients := map[string]*client{"admin": e.login("admin"), "leader": e.login("leader"), "alpha": e.login("alpha")}
	for _, c := range cases {
		for name, want := range map[string]int{"admin": c.admin, "leader": c.leader, "alpha": c.group} {
			if got := clients[name].get(c.path).status; got != want {
				t.Errorf("GET %s as %s = %d, want %d", c.path, name, got, want)
			}
		}
	}
	posts := []struct {
		path   string
		role   string
		status int
	}{
		{"/admin/users", "leader", 403}, {"/admin/users", "alpha", 403},
		{"/templates", "alpha", 403}, {"/requests", "alpha", 403}, {"/requests", "admin", 403},
		{"/reports", "leader", 403}, {"/reports", "admin", 403}, {"/queue/bulk", "admin", 403},
		{"/templates/" + e.id["SITREP"] + "/archive", "alpha", 403},
	}
	for _, p := range posts {
		if got := clients[p.role].post(p.path).status; got != p.status {
			t.Errorf("POST %s as %s = %d, want %d", p.path, p.role, got, p.status)
		}
	}
	if tp, _ := e.st.Template(e.id["SITREP"]); tp.Archived {
		t.Error("group archived a template")
	}
	// Wrong methods are not routed.
	if got := clients["leader"].post("/queue").status; got != http.StatusMethodNotAllowed {
		t.Errorf("POST /queue = %d", got)
	}
}

func TestStaticAndHealth(t *testing.T) {
	e := newEnv(t)
	c := e.anon()
	r := c.get("/static/app.css").want(t, 200, "--accent")
	if ct := r.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("css content type %q", ct)
	}
	if r.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("static files lack nosniff")
	}
	c.get("/static/favicon.svg").want(t, 200, "<svg")
	c.get("/static/").want(t, 404)
	c.get("/static/../web.go").want(t, 404)
	c.get("/healthz").want(t, 200, "ok")
}

func TestPasswordChange(t *testing.T) {
	e := newEnv(t)
	c := e.login("alpha")
	second := e.login("alpha")
	c.get("/password").want(t, 200, "Current password")
	const newPw = "a-much-better-password"
	c.post("/password", "current", "wrong", "new", newPw, "confirm", newPw).want(t, 422, "current password is wrong")
	c.post("/password", "current", pw("alpha"), "new", newPw, "confirm", newPw+"x").want(t, 422, "do not match")
	c.post("/password", "current", pw("alpha"), "new", pw("alpha"), "confirm", pw("alpha")).want(t, 422, "must differ")
	c.post("/password", "current", pw("alpha"), "new", "short", "confirm", "short").want(t, 422, "at least 10")
	c.post("/password", "current", pw("alpha"), "new", newPw, "confirm", newPw).want(t, 200, "Password changed.")
	c.get("/home").want(t, 200).wantNot(t, "Password changed.") // flash is shown once
	if got := second.get("/home").path; got != "/login" {
		t.Errorf("other session survived the password change (at %s)", got)
	}
	e.anon().postRaw("/login", url.Values{"username": {"alpha"}, "password": {pw("alpha")}}, nil).want(t, 401)
	e.anon().postRaw("/login", url.Values{"username": {"alpha"}, "password": {newPw}}, nil).want(t, 200, "Group Alpha")
}

var otpRe = regexp.MustCompile(`[Oo]ne-time password[^:]*: ([A-Za-z0-9]{16}) `)

func TestAdminUsers(t *testing.T) {
	e := newEnv(t)
	a := e.login("admin")
	a.get("/admin/users").want(t, 200, "Group Alpha", "New account")
	a.post("/admin/users", "username", "alpha", "display_name", "Dup", "role", "group").want(t, 422, "already in use", `value="Dup"`)
	a.post("/admin/users", "username", "x", "display_name", "Bad", "role", "group").want(t, 422, "username must be")
	a.post("/admin/users", "username", "boss", "display_name", "Boss", "role", "emperor").want(t, 422, "unknown role")

	r := a.post("/admin/users", "username", "Charlie", "display_name", "Group Charlie", "role", "group").want(t, 200, "Account &#34;charlie&#34; created", "one-time password")
	m := otpRe.FindStringSubmatch(r.body)
	if m == nil {
		t.Fatalf("no one-time password in flash: %s", r.body)
	}
	a.get("/admin/users").wantNot(t, m[1]) // shown exactly once
	u, _ := e.st.UserByUsername("charlie")
	if !u.MustChange || u.Role != model.RoleGroup || strings.Contains(u.PassHash, m[1]) {
		t.Errorf("new user = %+v", u)
	}

	// The new user must change the one-time password before anything else.
	c := e.anon()
	r = c.postRaw("/login", url.Values{"username": {"charlie"}, "password": {m[1]}}, nil).want(t, 200, "one-time password")
	if r.path != "/password" {
		t.Errorf("first login landed on %s", r.path)
	}
	if got := c.get("/home").path; got != "/password" {
		t.Errorf("/home reachable before password change (at %s)", got)
	}
	if got := c.post("/reports", "template", e.id["Minimal report"], "subject", "s", "content", "c").path; got != "/password" {
		t.Errorf("POST allowed before password change (at %s)", got)
	}
	c.post("/password", "current", m[1], "new", "charlies-own-password", "confirm", "charlies-own-password").want(t, 200, "Password changed", "Group Charlie")
	if got := c.get("/home").path; got != "/home" {
		t.Errorf("after change at %s", got)
	}

	path := "/admin/users/" + u.ID
	a.post(path, "action", "rename", "display_name", "Group C").want(t, 200, "renamed", "Group C")
	a.post(path, "action", "rename", "display_name", "").want(t, 422, "display name is required")
	a.post(path, "action", "explode").want(t, 400, "Unknown action")
	a.post("/admin/users/nope", "action", "reset").want(t, 404)

	a.post(path, "action", "disable").want(t, 200, "disabled")
	if got := c.get("/home").path; got != "/login" {
		t.Errorf("disabled user still has a session (at %s)", got)
	}
	e.anon().postRaw("/login", url.Values{"username": {"charlie"}, "password": {"charlies-own-password"}}, nil).want(t, 401)
	a.post(path, "action", "enable").want(t, 200, "enabled")

	c = e.anon()
	c.postRaw("/login", url.Values{"username": {"charlie"}, "password": {"charlies-own-password"}}, nil).want(t, 200)
	r = a.post(path, "action", "reset").want(t, 200, "New one-time password")
	m2 := otpRe.FindStringSubmatch(r.body)
	if m2 == nil || m2[1] == m[1] {
		t.Fatal("reset did not produce a fresh password")
	}
	if got := c.get("/home").path; got != "/login" {
		t.Errorf("session survived password reset (at %s)", got)
	}
	e.anon().postRaw("/login", url.Values{"username": {"charlie"}, "password": {m2[1]}}, nil).want(t, 200, "one-time password")

	self := "/admin/users/" + e.id["admin"]
	a.post(self, "action", "disable").want(t, 422, "cannot disable your own")
	a.post(self, "action", "reset").want(t, 422, "Change password")
	if u, _ := e.st.User(e.id["admin"]); u.Disabled || u.MustChange {
		t.Error("admin modified own account state")
	}
}

func TestReportFlow(t *testing.T) {
	e := newEnv(t)
	alpha, bravo, leader := e.login("alpha"), e.login("bravo"), e.login("leader")
	fixed := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	e.srv.Now = func() time.Time { return fixed }

	alpha.get("/home").want(t, 200, "Write a report", "Minimal report", "SITREP", "Nothing sent yet", "No open requests")
	alpha.get("/reports/new?template="+e.id["SITREP"]).want(t, 200, `value="SITREP"`, "Location", "Exercise leader", "Group Bravo", `name="f_help_needed"`).
		wantNot(t, `<option value="`+e.id["alpha"]+`"`)
	alpha.get("/reports/new?template=nope").want(t, 404)
	alpha.get("/reports/new").want(t, 404)

	// Validation errors keep what was typed.
	alpha.post("/reports", "template", e.id["SITREP"], "subject", "SITREP 1", "content", "kept text", "f_status", "Red", "f_notes", "note").
		want(t, 422, "Location is required", "kept text", `value="SITREP 1"`, `<option selected>Red</option>`)
	alpha.post("/reports", "template", e.id["SITREP"], "subject", "s", "content", "c", "f_location", "x", "f_status", "Blue").want(t, 422, "not a valid choice")
	alpha.post("/reports", "template", e.id["Minimal report"], "subject", "s", "content", "c", "receiver", e.id["alpha"]).want(t, 422, "Unknown receiver")
	alpha.post("/reports", "template", e.id["Minimal report"], "subject", "s", "content", "c", "receiver", e.id["leader"]).want(t, 422, "Unknown receiver")
	alpha.post("/reports", "template", "nope", "subject", "s", "content", "c").want(t, 404)

	r := alpha.post("/reports", "template", e.id["SITREP"], "subject", "SITREP 1", "content", "Bridge <b>blocked</b>\nMoving north",
		"f_location", "Grid 7", "f_status", "Red", "f_help_needed", "yes", "f_notes", "Need fuel", "f_when", "2026-05-01T11:30", "f_count", "12")
	r.want(t, 200, "Report sent to Exercise leader", "2026-05-01 12:00:00Z", "Grid 7", "Need fuel", "Bridge &lt;b&gt;blocked&lt;/b&gt;", "Group Alpha").
		wantNot(t, "<b>blocked", "File in folder")
	sitrepID := strings.TrimPrefix(r.path, "/reports/")
	rep, _ := e.st.Report(sitrepID)
	if rep.SenderID != e.id["alpha"] || rep.ReceiverID != model.ReceiverLeader || rep.Status != model.StatusNew ||
		!rep.Timestamp.Equal(fixed) || rep.TemplateName != "SITREP" || len(rep.Fields) != 6 {
		t.Errorf("stored report = %+v", rep)
	}

	fixed = fixed.Add(time.Hour)
	bravoID := bravo.send("Arrived at assembly point", "All present")
	fixed = fixed.Add(time.Hour)
	toAlpha := bravo.send("Need a radio", "Can you lend us one?", "receiver", e.id["alpha"])

	// Object level access control.
	alpha.get("/reports/"+bravoID).want(t, 404)
	alpha.post("/reports/"+bravoID+"/status", "action", "file", "folder", "x").want(t, 404)
	bravo.get("/reports/"+sitrepID).want(t, 404)
	alpha.get("/reports/nope").want(t, 404)
	if rep, _ := e.st.Report(bravoID); rep.Status != model.StatusNew {
		t.Error("foreign group changed a report")
	}
	// A sender reading its own report does not mark it read.
	alpha.get("/reports/"+sitrepID).want(t, 200)
	if rep, _ := e.st.Report(sitrepID); rep.Status != model.StatusNew {
		t.Error("sender view changed the status")
	}

	alpha.get("/sent").want(t, 200, "Sent reports", "SITREP 1").wantNot(t, "Arrived at assembly", "With selected")
	alpha.get("/home").want(t, 200, "Recently sent", "SITREP 1")

	// Group inbox: bravo's report to alpha.
	alpha.get("/queue").want(t, 200, "Inbox", "Need a radio", "Group Bravo", `<span class="badge">1</span>`).wantNot(t, "SITREP 1", "Arrived at assembly")
	alpha.get("/reports/"+toAlpha).want(t, 200, "Can you lend us one?", "File in folder")
	if rep, _ := e.st.Report(toAlpha); rep.Status != model.StatusRead {
		t.Error("receiver view did not mark the report read")
	}
	// The leader may read group-to-group traffic but it is not in the queue.
	leader.get("/reports/"+toAlpha).want(t, 200, "Need a radio").wantNot(t, "File in folder")
	leader.post("/reports/"+toAlpha+"/status", "action", "file").want(t, 404)

	// Leader queue: one queue per group, newest first.
	q := leader.get("/queue").want(t, 200, "Report queue", "All queues", "Group Alpha", "Group Bravo", "SITREP 1", "Arrived at assembly point", "1–2 of 2").
		wantNot(t, "Need a radio")
	if strings.Index(q.body, "Arrived at assembly point") > strings.Index(q.body, "SITREP 1") {
		t.Error("default order is not newest first")
	}
	leader.get("/queue?group="+e.id["alpha"]).want(t, 200, "SITREP 1").wantNot(t, "Arrived at assembly point")
	leader.get("/queue?sort=subject&dir=asc").want(t, 200, "▲")
	q = leader.get("/queue?sort=sender")
	if strings.Index(q.body, "Arrived at assembly point") > strings.Index(q.body, "SITREP 1") {
		t.Error("sort by sender descending is wrong")
	}
	leader.get("/queue?q=fuel+grid").want(t, 200, "SITREP 1", "1–1 of 1").wantNot(t, "Arrived at assembly point")
	leader.get("/queue?q=nomatch").want(t, 200, "No reports match the filter", "0 reports")
	leader.get("/queue?template="+e.id["Minimal report"]).want(t, 200, "Arrived at assembly point").wantNot(t, "SITREP 1")
	leader.get("/queue?from=2026-05-02").want(t, 200, "No reports match")
	leader.get("/queue?to=2026-05-01&from=2026-05-01").want(t, 200, "1–2 of 2")
	leader.get("/queue?status=bogus&sort=bogus&page=-3&from=x&to=y&group=../&folder="+strings.Repeat("f", 300)).want(t, 200, "1–2 of 2")
	leader.get("/queue?auto=1").want(t, 200, `http-equiv="refresh"`, "Auto-refresh: on")
	leader.get("/queue").wantNot(t, `http-equiv="refresh"`)

	// Opening marks read; filing moves it to a folder.
	leader.get("/reports/"+sitrepID).want(t, 200, "File in folder", `<span class="tag read">read</span>`)
	leader.get("/queue?status=new").want(t, 200, "Arrived at assembly point").wantNot(t, "SITREP 1")
	leader.post("/reports/"+sitrepID+"/status", "action", "file", "folder", " Logistics ").want(t, 200, "Report updated", "in folder <strong>Logistics</strong>", "Back to queue")
	leader.post("/reports/"+sitrepID+"/status", "action", "file", "folder", strings.Repeat("f", 61)).want(t, 422)
	leader.post("/reports/"+sitrepID+"/status", "action", "shred").want(t, 422, "choose an action")
	leader.get("/queue?folder=Logistics").want(t, 200, "SITREP 1", `<option selected>Logistics</option>`).wantNot(t, "Arrived at assembly point")
	leader.get("/queue?status=filed").want(t, 200, "SITREP 1")
	leader.get("/queue?sort=folder").want(t, 200)
	leader.get("/queue?sort=status&dir=asc").want(t, 200)

	// Bulk actions keep the current view.
	r = leader.post("/queue/bulk", "id", sitrepID, "id", bravoID, "id", toAlpha, "id", "nope", "action", "file", "folder", "Archive", "return", "group="+e.id["alpha"]+"&sort=subject&evil=1")
	r.want(t, 200, "2 report(s) updated")
	if r.path != "/queue?group="+e.id["alpha"]+"&sort=subject" {
		t.Errorf("bulk returned to %s", r.path)
	}
	if rep, _ := e.st.Report(toAlpha); rep.Folder != "" {
		t.Error("bulk action touched a report outside the queue")
	}
	leader.post("/queue/bulk", "id", bravoID, "action", "").want(t, 200, "Nothing changed: choose an action")
	leader.post("/queue/bulk", "id", bravoID, "action", "read").want(t, 200, "1 report(s) updated")
	if rep, _ := e.st.Report(bravoID); rep.Status != model.StatusRead || rep.Folder != "Archive" {
		t.Errorf("after bulk read: %+v", rep)
	}
	many := make([]string, 0, 1200)
	for i := 0; i < 501; i++ {
		many = append(many, "id", "x")
	}
	leader.post("/queue/bulk", append(many, "action", "read")...).want(t, 200, "too many reports selected")
	r = leader.post("/reports/"+bravoID+"/status", "action", "new").want(t, 200, "Report marked as new")
	if r.path != "/queue" {
		t.Errorf("mark as new ended at %s", r.path)
	}
	if rep, _ := e.st.Report(bravoID); rep.Status != model.StatusNew {
		t.Error("not marked new")
	}
}

func TestQueuePagination(t *testing.T) {
	e := newEnv(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < pageSize+20; i++ {
		rep := model.Report{ID: model.NewID(), Timestamp: base.Add(time.Duration(i) * time.Minute), SenderID: e.id["alpha"], SenderName: "Group Alpha",
			Subject: fmt.Sprintf("report-%03d-end", i), Content: "c", ReceiverID: model.ReceiverLeader, Status: model.StatusNew}
		if err := e.st.PutReport(rep); err != nil {
			t.Fatal(err)
		}
	}
	leader := e.login("leader")
	leader.get("/queue").want(t, 200, "1–100 of 120", "report-119-end", "report-020-end", "Next page", `<span class="badge">120</span>`).wantNot(t, "report-019-end", "Newer page")
	leader.get("/queue?page=1").want(t, 200, "101–120 of 120", "report-019-end", "report-000-end", "Newer page").wantNot(t, "report-020-end", "Next page")
	leader.get("/queue?page=99").want(t, 200, "No reports")
}

func TestRequestFlow(t *testing.T) {
	e := newEnv(t)
	alpha, bravo, leader := e.login("alpha"), e.login("bravo"), e.login("leader")

	leader.get("/requests").want(t, 200, "No requests", "New request")
	leader.get("/requests/new?template="+e.id["SITREP"]).want(t, 200, "All groups", "Group Alpha", `value="`+e.id["SITREP"]+`" selected`)
	leader.post("/requests", "subject", "", "template", e.id["SITREP"], "to", "all").want(t, 422, "subject is required")
	leader.post("/requests", "subject", "Kept subject", "template", "nope", "to", "all").want(t, 422, "Select a report template", "Kept subject")
	leader.post("/requests", "subject", "s", "template", e.id["SITREP"], "to", "some").want(t, 422, "Select at least one group")
	leader.post("/requests", "subject", "s", "template", e.id["SITREP"], "to", "some", "recipient", e.id["leader"]).want(t, 422, "Unknown recipient")
	leader.post("/requests", "subject", "s", "template", e.id["SITREP"], "to", "some", "recipient", "../").want(t, 422, "invalid recipient")

	r := leader.post("/requests", "subject", "Status at 14:00", "body", "Report status\nand needs", "template", e.id["SITREP"], "to", "some", "recipient", e.id["alpha"])
	r.want(t, 200, "Request sent", "Status at 14:00", "Report status\nand needs", "Group Alpha", "No responses yet", "Close request").wantNot(t, "Group Bravo")
	reqPath := r.path
	reqID := strings.TrimPrefix(reqPath, "/requests/")
	r = leader.post("/requests", "subject", "To everyone", "template", e.id["Minimal report"], "to", "all", "recipient", e.id["alpha"])
	r.want(t, 200, "Group Alpha", "Group Bravo")
	allID := strings.TrimPrefix(r.path, "/requests/")

	// Only addressed groups see a request.
	bravo.get(reqPath).want(t, 404)
	bravo.get("/requests").want(t, 200, "To everyone").wantNot(t, "Status at 14:00")
	bravo.get("/reports/new?request="+reqID).want(t, 404)
	bravo.post("/reports", "request", reqID, "subject", "s", "content", "c", "f_location", "x").want(t, 404)
	leader.get("/requests/nope").want(t, 404)

	alpha.get("/home").want(t, 200, "Status at 14:00", "To everyone", `<span class="tag new">no</span>`)
	alpha.get("/requests").want(t, 200, "Status at 14:00", "Answer")
	alpha.get(reqPath).want(t, 200, "Report status", "Answer with a report", "Your responses").wantNot(t, "Close request", "Recipients")
	alpha.get("/reports/new?request="+reqID).want(t, 200, "Answer to request", `value="Re: Status at 14:00"`, "Location", "disabled")

	// The request dictates template and receiver.
	r = alpha.post("/reports", "request", reqID, "template", e.id["Minimal report"], "receiver", e.id["bravo"],
		"subject", "Re: Status at 14:00", "content", "All fine", "f_location", "Grid 1")
	r.want(t, 200, "Report sent to Exercise leader", "Answers request", "Grid 1")
	rep, _ := e.st.Report(strings.TrimPrefix(r.path, "/reports/"))
	if rep.RequestID != reqID || rep.TemplateID != e.id["SITREP"] || rep.ReceiverID != model.ReceiverLeader {
		t.Errorf("response = %+v", rep)
	}
	alpha.get("/home").want(t, 200, "yes (1)")
	alpha.get(reqPath).want(t, 200, "Re: Status at 14:00")
	leader.get(reqPath).want(t, 200, "Responses", "Re: Status at 14:00", "Group Alpha")
	leader.get("/requests").want(t, 200, "Status at 14:00", "SITREP")
	leader.get("/queue?request="+reqID).want(t, 200, "Re: Status at 14:00", "1–1 of 1")
	leader.get("/queue?request="+allID).want(t, 200, "No reports match")

	// Closing stops further answers; archived templates stay usable for
	// open requests.
	tp, _ := e.st.Template(e.id["Minimal report"])
	tp.Archived = true
	if err := e.st.PutTemplate(tp); err != nil {
		t.Fatal(err)
	}
	bravo.get("/reports/new?request="+allID).want(t, 200, "To everyone")
	bravo.get("/reports/new?template="+tp.ID).want(t, 404)

	leader.post(reqPath+"/close", "closed", "1").want(t, 200, "Request closed", "Reopen request")
	alpha.get("/home").wantNot(t, ">Status at 14:00<")
	alpha.get(reqPath).want(t, 200, "closed").wantNot(t, "Answer with a report")
	alpha.get("/reports/new?request="+reqID).want(t, 422, "This request is closed")
	alpha.post("/reports", "request", reqID, "subject", "late", "content", "c", "f_location", "x").want(t, 422, "closed")
	leader.post(reqPath+"/close", "closed", "0").want(t, 200, "Request reopened", "Close request")
	leader.post("/requests/nope/close", "closed", "1").want(t, 404)
	alpha.post(reqPath+"/close", "closed", "1").want(t, 403)
}

func TestTemplates(t *testing.T) {
	e := newEnv(t)
	leader, admin, alpha := e.login("leader"), e.login("admin"), e.login("alpha")

	leader.get("/templates").want(t, 200, "Minimal report", "SITREP", "Send request")
	admin.get("/templates").want(t, 200, "SITREP").wantNot(t, "Send request")
	leader.get("/templates/new").want(t, 200, "New template", `name="rows" value="3"`)
	leader.get("/templates/"+e.id["SITREP"]).want(t, 200, "Edit template", `value="Location"`, `value="Green, Red"`, `name="rows" value="9"`)
	leader.get("/templates/nope").want(t, 404)

	leader.post("/templates", "name", "", "rows", "0").want(t, 422, "template name is required")
	leader.post("/templates", "name", "Kept name", "rows", "1", "f0_label", "Colour", "f0_type", "select").
		want(t, 422, "a select field needs options", `value="Kept name"`, `value="Colour"`)

	r := admin.post("/templates", "name", "Contact report", "description", "Report a contact", "default_subject", "CONTACT", "content_help", "What happened",
		"rows", "4", "f0_label", "Direction", "f0_type", "select", "f0_opts", "North, South", "f0_req", "1", "f0_help", "bearing",
		"f1_label", "", "f1_type", "text", "f2_label", "Number seen", "f2_type", "number", "f3_label", "Direction", "f3_type", "text", "more", "1")
	r.want(t, 200, "You can add more fields", `value="Direction"`, `value="North, South"`, `name="rows" value="6"`)
	id := strings.TrimPrefix(r.path, "/templates/")
	tp, ok := e.st.Template(id)
	if !ok || len(tp.Fields) != 3 || tp.Fields[0].Key != "direction" || !tp.Fields[0].Required || tp.Fields[1].Key != "number_seen" ||
		tp.Fields[2].Key != "direction_2" || tp.CreatedBy != e.id["admin"] || tp.DefaultSubject != "CONTACT" {
		t.Fatalf("stored template = %+v", tp)
	}

	// Editing keeps keys of existing fields, even when relabelled, and
	// removes fields whose label was cleared.
	leader.post("/templates/"+id, "name", "Contact report v2", "rows", "3",
		"f0_key", "direction", "f0_label", "Bearing", "f0_type", "select", "f0_opts", "N,S,E,W",
		"f1_key", "number_seen", "f1_label", "", "f1_type", "number",
		"f2_key", "direction_2", "f2_label", "Direction", "f2_type", "text").want(t, 200, "Template saved.", "Contact report v2")
	tp, _ = e.st.Template(id)
	if len(tp.Fields) != 2 || tp.Fields[0].Key != "direction" || tp.Fields[0].Label != "Bearing" || len(tp.Fields[0].Options) != 4 || tp.CreatedBy != e.id["admin"] {
		t.Errorf("edited template = %+v", tp)
	}
	leader.post("/templates/nope", "name", "x").want(t, 404)

	alpha.get("/reports/new?template="+id).want(t, 200, "Contact report v2", "Bearing", "<option>N</option>")
	leader.post("/templates/"+id+"/archive", "archived", "1").want(t, 200, "Template archived", "Restore")
	alpha.get("/home").wantNot(t, "Contact report v2")
	alpha.get("/reports/new?template="+id).want(t, 404)
	alpha.post("/reports", "template", id, "subject", "s", "content", "c", "f_direction", "N").want(t, 404)
	leader.post("/requests", "subject", "s", "template", id, "to", "all").want(t, 422, "Select a report template")
	leader.post("/templates/"+id+"/archive", "archived", "0").want(t, 200, "Template restored")
	alpha.get("/reports/new?template="+id).want(t, 200)
	leader.post("/templates/nope/archive", "archived", "1").want(t, 404)
}

func TestOutputEscaping(t *testing.T) {
	e := newEnv(t)
	const evil = `"><script>alert(1)</script><img src=x onerror=alert(2)>`
	alpha, leader := e.login("alpha"), e.login("leader")
	id := alpha.send(evil, evil)
	leader.post("/reports/"+id+"/status", "action", "file", "folder", `"><script>f</script>`)
	leader.post("/templates", "name", evil, "description", evil, "rows", "1", "f0_label", evil, "f0_type", "select", "f0_opts", evil)
	leader.post("/requests", "subject", evil, "body", evil, "template", e.id["Minimal report"], "to", "all")
	for _, c := range []*client{alpha, leader} {
		for _, p := range []string{"/queue", "/requests", "/reports/" + id, "/queue?q=" + url.QueryEscape(evil) + "&folder=" + url.QueryEscape(evil)} {
			c.get(p).want(t, 200).wantNot(t, "<script>", "<img src=x")
		}
	}
	leader.get("/templates").wantNot(t, "<script>")
	alpha.get("/home").wantNot(t, "<script>")
	e.anon().postRaw("/login", url.Values{"username": {evil}, "password": {"x"}}, nil).want(t, 401).wantNot(t, "<script>")
}

func TestBodyLimit(t *testing.T) {
	e := newEnv(t)
	c := e.login("alpha")
	big := strings.Repeat("x", MaxBodyBytes+1)
	c.post("/reports", "template", e.id["Minimal report"], "subject", "s", "content", big).want(t, 400, "too large")
	e.anon().postRaw("/login", url.Values{"username": {"alpha"}, "password": {big}}, nil).want(t, 400)
	if n := len(e.st.Reports(nil)); n != 0 {
		t.Errorf("%d reports stored", n)
	}
}

func TestStaleSessions(t *testing.T) {
	e := newEnv(t)
	c := e.login("alpha")
	u, _ := e.st.User(e.id["alpha"])
	u.Disabled = true
	if err := e.st.PutUser(u); err != nil {
		t.Fatal(err)
	}
	if got := c.get("/home").path; got != "/login" {
		t.Errorf("disabled user reached %s", got)
	}
	if e.srv.Sessions.Len() != 0 {
		t.Error("session of disabled user kept")
	}
	l := e.login("leader")
	now := time.Now().Add(e.srv.Sessions.Idle + time.Minute)
	e.srv.Sessions.Now = func() time.Time { return now }
	if got := l.get("/queue").path; got != "/login" {
		t.Errorf("expired session reached %s", got)
	}
}

func TestStoreFailure(t *testing.T) {
	e := newEnv(t)
	alpha, leader, admin := e.login("alpha"), e.login("leader"), e.login("admin")
	id := alpha.send("s", "c")
	toks := map[*client]string{alpha: alpha.csrf(), leader: leader.csrf(), admin: admin.csrf()}
	e.st.Close()
	post := func(c *client, path string, kv ...string) {
		t.Helper()
		form := url.Values{"csrf": {toks[c]}}
		for i := 0; i+1 < len(kv); i += 2 {
			form.Add(kv[i], kv[i+1])
		}
		c.postRaw(path, form, nil).want(t, 500, "could not be saved")
	}
	post(alpha, "/reports", "template", e.id["Minimal report"], "subject", "s", "content", "c")
	post(alpha, "/password", "current", pw("alpha"), "new", "another-password", "confirm", "another-password")
	post(leader, "/templates", "name", "x")
	post(leader, "/templates/"+e.id["SITREP"]+"/archive", "archived", "1")
	post(leader, "/requests", "subject", "s", "template", e.id["SITREP"], "to", "all")
	post(admin, "/admin/users", "username", "delta", "display_name", "Delta", "role", "group")
	post(admin, "/admin/users/"+e.id["alpha"], "action", "disable")
	leader.get("/reports/"+id).want(t, 500)
	leader.get("/queue").want(t, 200) // reading still works
}

func TestClientIPAndOrigin(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.7:1234"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	if got := clientIP(r); got != "192.0.2.7" {
		t.Errorf("clientIP = %q (forwarding headers must be ignored)", got)
	}
	r.RemoteAddr = "weird"
	if got := clientIP(r); got != "weird" {
		t.Errorf("clientIP = %q", got)
	}
	if got := fmtTime(time.Time{}); got != "" {
		t.Errorf("fmtTime(zero) = %q", got)
	}
}
