package web

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rom/gr_report/internal/model"
	"github.com/rom/gr_report/internal/store"
)

// addUser creates a further account with the usual test password.
func (e *env) addUser(name, display string, role model.Role, groupID string) string {
	e.t.Helper()
	hash, err := fastHasher.Hash(pw(name))
	if err != nil {
		e.t.Fatal(err)
	}
	id := model.NewID()
	if err := e.st.PutUser(model.User{ID: id, Username: name, DisplayName: display, Role: role, PassHash: hash, GroupID: groupID}); err != nil {
		e.t.Fatal(err)
	}
	e.id[name] = id
	return id
}

func TestPersonalAccounts(t *testing.T) {
	e := newEnv(t)
	A, B := e.id["alpha"], e.id["bravo"]
	e.addUser("anna", "Anna Andersson", model.RoleGroup, A)
	e.addUser("bo", "Bo Berg", model.RoleGroup, A)
	alpha, anna, bo, bravo, leader := e.login("alpha"), e.login("anna"), e.login("bo"), e.login("bravo"), e.login("leader")

	// A member works as the group: sees its name, its requests, its inbox.
	anna.get("/home").want(t, 200, "<h1>Group Alpha</h1>", "Anna Andersson · Group Alpha")
	anna.get("/reports/new?template="+e.id["Minimal report"]).want(t, 200, `Group Alpha <span class="muted">(Anna Andersson)</span>`, "Group Bravo").
		wantNot(t, `<option value="`+A+`"`, "Bo Berg") // not to the own group, and members are not receivers
	id := anna.send("From Anna", "written by a person")
	rep, _ := e.st.Report(id)
	if rep.SenderID != A || rep.SenderName != "Group Alpha" || rep.AuthorID != e.id["anna"] || rep.AuthorName != "Anna Andersson" {
		t.Errorf("report by a member = %+v", rep)
	}
	// A shared login records no author.
	shared, _ := e.st.Report(alpha.send("From the shared login", "c"))
	if shared.AuthorID != "" || shared.AuthorName != "" || shared.SenderID != A {
		t.Errorf("report by the shared login = %+v", shared)
	}
	// The whole group sees what any of them sent.
	for _, c := range []*client{alpha, anna, bo} {
		c.get("/sent").want(t, 200, "From Anna", "From the shared login")
		c.get("/reports/"+id).want(t, 200, "Written by", "Anna Andersson")
	}
	bravo.get("/reports/"+id).want(t, 404)
	leader.get("/queue").want(t, 200, `Group Alpha <span class="muted">(Anna Andersson)</span>`, "Group Bravo").wantNot(t, ">Anna Andersson<", "Bo Berg</span>")
	leader.get("/reports/"+id).want(t, 200, "Written by", "Anna Andersson")
	leader.get("/queue?q=andersson").want(t, 200, "From Anna", "1–1 of 1")

	// Reports to the group reach every member; read state is personal.
	toAlpha := bravo.send("For Alpha", "c", "receiver", A)
	bravo.post("/reports", "template", e.id["Minimal report"], "subject", "s", "content", "c", "receiver", e.id["anna"]).want(t, 422, "Unknown receiver.")
	anna.get("/queue").want(t, 200, "For Alpha", `class="unread"`)
	anna.get("/reports/"+toAlpha).want(t, 200, "File in folder")
	anna.get("/queue").want(t, 200, "For Alpha").wantNot(t, `class="unread"`)
	bo.get("/queue").want(t, 200, "For Alpha", `class="unread"`, `<span class="badge js-new-count">1</span>`)

	// Requests are addressed to groups, never to members.
	leader.post("/requests", "subject", "s", "template", e.id["SITREP"], "to", "some", "recipient", e.id["anna"]).want(t, 422, "Unknown recipient.")
	r := leader.post("/requests", "subject", "Only Alpha", "template", e.id["Minimal report"], "to", "some", "recipient", A)
	r.want(t, 200, "Group Alpha").wantNot(t, "Anna Andersson", "Bo Berg")
	reqID := strings.TrimPrefix(r.path, "/requests/")
	leader.get("/requests/new").want(t, 200, "Group Alpha", "Group Bravo").wantNot(t, "Anna Andersson")
	bo.get("/home").want(t, 200, "Only Alpha")
	bo.post("/reports", "request", reqID, "subject", "answer", "content", "c").want(t, 200, "Report sent")
	anna.get("/home").want(t, 200, "yes (1)") // answered by the group
	bravo.get("/requests/"+reqID).want(t, 404)

	// Chat: the group is the party, the person is named.
	anna.post("/chat/"+B, "body", "hello from anna").want(t, 200, "Anna Andersson (Group Alpha)")
	bo.get("/chat/"+B).want(t, 200, "hello from anna", `class="chat-line mine"`)
	bravo.get("/chat/"+A).want(t, 200, "Anna Andersson (Group Alpha)")
	bravo.get("/chat/all").wantNot(t, `href="/chat/`+e.id["anna"]+`"`)
	bravo.get("/chat/"+e.id["anna"]).want(t, 404)
	anna.get("/chat/"+A).want(t, 404) // not with the own group

	// Disabling the group blocks its members too.
	g, _ := e.st.User(A)
	g.Disabled = true
	if err := e.st.PutUser(g); err != nil {
		t.Fatal(err)
	}
	if got := anna.get("/home").path; got != "/login" {
		t.Errorf("member of a disabled group reached %s", got)
	}
	e.anon().postRaw("/login", url.Values{"username": {"anna"}, "password": {pw("anna")}}, nil).want(t, 401)
	g.Disabled = false
	_ = e.st.PutUser(g)
	e.login("anna")

	// Creating members in the user interface.
	admin := e.login("admin")
	admin.get("/admin/users").want(t, 200, "Member of group", `<option value="`+A+`">Group Alpha</option>`, "shared account", "<td>Group Alpha</td>").
		wantNot(t, `<option value="`+e.id["anna"]+`">`)
	admin.post("/admin/users", "username", "cia", "display_name", "Cia", "role", "group", "group", B).want(t, 200, "created")
	if u, _ := e.st.UserByUsername("cia"); u.GroupID != B || u.Party() != B {
		t.Errorf("new member = %+v", u)
	}
	for _, bad := range [][]string{
		{"role", "leader", "group", A},             // only group accounts can be members
		{"role", "group", "group", e.id["anna"]},   // not a member of a member
		{"role", "group", "group", e.id["leader"]}, // not of a leader
		{"role", "group", "group", "nope"},         // must exist
	} {
		admin.post("/admin/users", append([]string{"username", "dan", "display_name", "Dan"}, bad...)...).want(t, 422, "Only an account with the group role can be a member")
	}
	if _, ok := e.st.UserByUsername("dan"); ok {
		t.Error("invalid member created")
	}
}

func TestDrafts(t *testing.T) {
	e := newEnv(t)
	alpha, bravo := e.login("alpha"), e.login("bravo")
	fixed := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	e.srv.Now = func() time.Time { return fixed }
	sitrep := e.id["SITREP"]

	// An incomplete report can be saved; nothing is sent.
	r := alpha.post("/reports", "template", sitrep, "action", "draft", "subject", "Half written", "content", "", "f_notes", "remember the bridge", "receiver", e.id["bravo"])
	r.want(t, 200, "Draft saved. It has not been sent.", "Continuing your draft, saved 2026-05-01 12:00:00Z.", `value="Half written"`, "remember the bridge",
		`<option value="`+e.id["bravo"]+`" selected>`, "Save as draft", `data-autosave`)
	if r.path != "/reports/new?template="+sitrep {
		t.Errorf("after saving at %s", r.path)
	}
	if n := len(e.st.Reports(nil)); n != 0 {
		t.Fatalf("%d reports sent by saving a draft", n)
	}
	alpha.get("/home").want(t, 200, "Your drafts", "Half written", "SITREP", "saved 2026-05-01 12:00:00Z")
	bravo.get("/home").wantNot(t, "Your drafts", "Half written")
	bravo.get("/reports/new?template="+sitrep).wantNot(t, "Half written", "Continuing your draft")

	// Saving again replaces it; the page script saves in the background.
	fixed = fixed.Add(time.Minute)
	req, _ := http.NewRequest(http.MethodPost, e.ts.URL+"/reports", strings.NewReader(url.Values{
		"csrf": {alpha.csrf()}, "template": {sitrep}, "action": {"draft"}, "subject": {"Half written v2"}, "f_location": {"Åre"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Autosave", "1")
	if res := alpha.do(req); res.status != http.StatusNoContent || res.body != "" {
		t.Errorf("autosave: %d %q", res.status, res.body)
	}
	drafts := e.st.Drafts(e.id["alpha"])
	if len(drafts) != 1 || drafts[0].Subject != "Half written v2" || drafts[0].Values["location"] != "Åre" || drafts[0].Values["notes"] != "" {
		t.Fatalf("drafts = %+v", drafts)
	}
	alpha.get("/reports/new?template="+sitrep).want(t, 200, `value="Half written v2"`, `value="Åre"`)
	// Too long: refused, in both ways of saving.
	long := strings.Repeat("x", model.MaxSubjectLen+1)
	alpha.post("/reports", "template", sitrep, "action", "draft", "subject", long).want(t, 422, "the draft is too long to save")
	req, _ = http.NewRequest(http.MethodPost, e.ts.URL+"/reports", strings.NewReader(url.Values{
		"csrf": {alpha.csrf()}, "template": {sitrep}, "action": {"draft"}, "f_notes": {strings.Repeat("y", model.MaxFieldValueLen+1)}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Autosave", "1")
	if res := alpha.do(req); res.status != http.StatusUnprocessableEntity {
		t.Errorf("oversized autosave: %d", res.status)
	}

	// A draft per template and per request.
	leader := e.login("leader")
	rq := leader.post("/requests", "subject", "Status?", "template", sitrep, "to", "all")
	reqID := strings.TrimPrefix(rq.path, "/requests/")
	alpha.post("/reports", "request", reqID, "action", "draft", "subject", "Answer in progress").want(t, 200, "Draft saved", `value="Answer in progress"`, "Answer to request")
	alpha.post("/reports", "template", e.id["Minimal report"], "action", "draft", "subject", "").want(t, 200, "Draft saved")
	alpha.get("/home").want(t, 200, "Half written v2", "Answer in progress", "Status?", "(no subject)")
	if n := len(e.st.Drafts(e.id["alpha"])); n != 3 {
		t.Errorf("%d drafts", n)
	}

	// Sending removes the draft; a validation error keeps it.
	alpha.post("/reports", "template", sitrep, "subject", "Half written v2", "content", "", "f_location", "Åre").want(t, 422, "content is required")
	alpha.post("/reports", "template", sitrep, "subject", "Done", "content", "now complete", "f_location", "Åre").want(t, 200, "Report sent")
	alpha.get("/home").want(t, 200, "Answer in progress").wantNot(t, "Half written v2")
	alpha.get("/reports/new?template="+sitrep).wantNot(t, "Continuing your draft")

	// Deleting: only one's own.
	d := e.st.Drafts(e.id["alpha"])[0]
	bravo.post("/drafts/"+d.ID+"/delete").want(t, 200)
	if n := len(e.st.Drafts(e.id["alpha"])); n != 2 {
		t.Errorf("another user deleted a draft: %d left", n)
	}
	alpha.post("/drafts/"+d.ID+"/delete").want(t, 200, "Draft deleted.")
	if n := len(e.st.Drafts(e.id["alpha"])); n != 1 {
		t.Errorf("%d drafts after delete", n)
	}
	leader.post("/drafts/x/delete").want(t, 403)

	// The limit per user.
	for i := 0; i < model.MaxDrafts+1; i++ {
		tp := model.Template{ID: model.NewID(), Name: fmt.Sprint("T", i)}
		_ = e.st.PutTemplate(tp)
		r = bravo.post("/reports", "template", tp.ID, "action", "draft", "subject", "s")
	}
	r.want(t, 422, "you have too many drafts; send or delete one first")
	// A failed write is reported, not swallowed.
	e.breakWrites()
	alpha.post("/reports", "template", sitrep, "action", "draft", "subject", "s").want(t, 422, "could not be saved")
}

func TestRequestDeadlines(t *testing.T) {
	e := newEnv(t)
	alpha, bravo, leader := e.login("alpha"), e.login("bravo"), e.login("leader")
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	e.srv.Now = func() time.Time { return now }
	tpl := e.id["Minimal report"]

	leader.get("/requests/new").want(t, 200, `name="due"`, "Deadline", "Time zone: UTC.")
	leader.post("/requests", "subject", "s", "template", tpl, "to", "all", "due", "tomorrow").want(t, 422, "the deadline must be a date and time", `value="tomorrow"`)
	leader.post("/requests", "subject", "s", "template", tpl, "to", "all", "due", "2026-05-01T09:00").want(t, 422, "The deadline must be in the future.")
	r := leader.post("/requests", "subject", "Status by noon", "template", tpl, "to", "all", "due", "2026-05-01T12:00")
	r.want(t, 200, "Request sent.", "Deadline", "2026-05-01 12:00:00Z", `value="2026-05-01T12:00"`).wantNot(t, "overdue")
	path := r.path
	id := strings.TrimPrefix(path, "/requests/")
	q, _ := e.st.Request(id)
	if !q.Due.Equal(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("due = %v", q.Due)
	}
	leader.post("/requests", "subject", "No deadline", "template", tpl, "to", "all").want(t, 200, "<dd>none</dd>")

	// Before the deadline: shown, nothing is overdue.
	alpha.get("/home").want(t, 200, "Status by noon", "2026-05-01 12:00:00Z").wantNot(t, "overdue")
	alpha.post("/reports", "request", id, "subject", "in time", "content", "c").want(t, 200, "Report sent").wantNot(t, "after the deadline")

	// After it: overdue for those who have not answered, and a late answer
	// is accepted but marked.
	now = now.Add(3 * time.Hour)
	alpha.get("/home").want(t, 200, "Status by noon").wantNot(t, `<span class="tag new">overdue</span>`)
	bravo.get("/home").want(t, 200, "Status by noon", `<span class="tag new">overdue</span>`)
	bravo.get("/requests").want(t, 200, `<span class="tag new">overdue</span>`)
	bravo.get(path).want(t, 200, `<span class="tag new">overdue</span>`, "Answer with a report")
	leader.get("/requests").want(t, 200, `<span class="tag new">overdue</span>`)
	lr := leader.get(path).want(t, 200, "Group Bravo", `<span class="tag new">none</span> <span class="tag new">overdue</span>`)
	if strings.Count(lr.body, `<span class="tag new">none</span>`) != 1 {
		t.Error("only the group without an answer should be listed as missing")
	}
	late := bravo.post("/reports", "request", id, "subject", "late answer", "content", "c").want(t, 200, "Report sent", "after the deadline")
	bravo.get("/home").wantNot(t, `<span class="tag new">overdue</span>`)
	leader.get(path).want(t, 200, "late answer", "after the deadline", "in time")
	leader.get(late.path).want(t, 200, "after the deadline")

	// The leader can move or remove the deadline; a closed request is
	// never overdue.
	leader.post(path+"/due", "due", "2026-05-02T08:30").want(t, 200, "Deadline set to 2026-05-02 08:30:00Z.").wantNot(t, "overdue")
	leader.post(path+"/due", "due", "garbage").want(t, 422, "the deadline must be a date and time")
	leader.post(path+"/due", "due", "").want(t, 200, "Deadline removed.", "<dd>none</dd>")
	leader.post(path+"/due", "due", "2026-05-01T11:00").want(t, 200, "overdue")
	leader.post(path+"/close", "closed", "1").want(t, 200).wantNot(t, "overdue")
	alpha.post(path+"/due", "due", "").want(t, 403)
	leader.post("/requests/nope/due", "due", "").want(t, 404)
}

func TestTimeZones(t *testing.T) {
	e := newEnv(t)
	alpha, leader := e.login("alpha"), e.login("leader")
	// 23:30 UTC on 1 May is 01:30 on 2 May in Stockholm (CEST).
	sent := time.Date(2026, 5, 1, 23, 30, 0, 0, time.UTC)
	e.srv.Now = func() time.Time { return sent }
	id := alpha.send("Night report", "c")

	leader.get("/reports/"+id).want(t, 200, "2026-05-01 23:30:00Z")
	leader.get("/settings").want(t, 200, "Time zone", `name="tz" value="UTC"`, `<option value="Europe/Stockholm">`)
	leader.post("/settings", "tz", "Europe/Stockholm").want(t, 200, `name="tz" value="Europe/Stockholm"`, "Settings saved.")
	leader.get("/reports/"+id).want(t, 200, "2026-05-02 01:30:00 CEST").wantNot(t, "23:30:00Z")
	leader.get("/queue").want(t, 200, "2026-05-02 01:30:00 CEST", "Times in Europe/Stockholm")
	// Date filters follow the user's calendar day.
	leader.get("/queue?from=2026-05-02&to=2026-05-02").want(t, 200, "Night report")
	leader.get("/queue?to=2026-05-01").want(t, 200, "No reports match the filter.")
	// Another user is unaffected.
	alpha.get("/reports/"+id).want(t, 200, "2026-05-01 23:30:00Z")
	alpha.get("/queue?from=2026-05-01&to=2026-05-01").want(t, 200)

	// Deadlines are typed and shown in the user's zone.
	r := leader.post("/requests", "subject", "s", "template", e.id["Minimal report"], "to", "all", "due", "2026-05-03T09:00")
	r.want(t, 200, "2026-05-03 09:00:00 CEST", `value="2026-05-03T09:00"`)
	q, _ := e.st.Request(strings.TrimPrefix(r.path, "/requests/"))
	if !q.Due.Equal(time.Date(2026, 5, 3, 7, 0, 0, 0, time.UTC)) {
		t.Errorf("due stored as %v", q.Due)
	}
	alpha.get("/home").want(t, 200, "2026-05-03 07:00:00Z")

	// Winter time is used when it applies.
	jan := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if got := fmtTimeIn(jan, mustZone(t, "Europe/Stockholm")); got != "2026-01-15 13:00:00 CET" {
		t.Errorf("winter time = %q", got)
	}
	// Unknown or unsafe names are ignored; the server default applies to
	// those who have not chosen.
	leader.post("/settings", "tz", "Mars/Olympus").want(t, 200, `name="tz" value="Europe/Stockholm"`)
	leader.post("/settings", "tz", "Local").want(t, 200, `name="tz" value="Europe/Stockholm"`)
	leader.post("/settings", "tz", "../../etc/passwd").want(t, 200, `name="tz" value="Europe/Stockholm"`)
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/login", nil)
	req.AddCookie(&http.Cookie{Name: zoneCookie, Value: `"><script>`})
	e.anon().do(req).want(t, 200).wantNot(t, "<script>")
	e.srv.DefaultZone = mustZone(t, "Asia/Tokyo")
	alpha.get("/reports/"+id).want(t, 200, "2026-05-02 08:30:00 JST")
	leader.get("/reports/"+id).want(t, 200, "CEST")
	for _, bad := range []string{"", "Local", strings.Repeat("a", 65), "No/Such"} {
		if _, ok := loadZone(bad); ok {
			t.Errorf("loadZone(%q) accepted", bad)
		}
	}
	if loc, ok := loadZone("UTC"); !ok || loc != time.UTC {
		t.Error("UTC")
	}
}

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, ok := loadZone(name)
	if !ok {
		t.Fatalf("time zone %s not available", name)
	}
	return loc
}

func TestReadStateAssignmentAndSavedFilters(t *testing.T) {
	e := newEnv(t)
	L2 := e.addUser("leader2", "Second Leader", model.RoleLeader, "")
	alpha, bravo, l1, l2 := e.login("alpha"), e.login("bravo"), e.login("leader"), e.login("leader2")
	a1 := alpha.send("Alpha one", "c")
	a2 := alpha.send("Alpha two", "c")
	b1 := bravo.send("Bravo one", "c")
	const dot = `class="unread"`

	// Read state is per leader.
	if n := strings.Count(l1.get("/queue").body, dot); n != 3 {
		t.Errorf("fresh queue shows %d unread", n)
	}
	l1.get("/reports/" + a1)
	q1 := l1.get("/queue").want(t, 200, `<span class="badge js-new-count">2</span>`)
	if n := strings.Count(q1.body, dot); n != 2 {
		t.Errorf("leader 1 sees %d unread after opening one", n)
	}
	q2 := l2.get("/queue").want(t, 200, `<span class="badge js-new-count">3</span>`)
	if n := strings.Count(q2.body, dot); n != 3 {
		t.Errorf("leader 2 sees %d unread", n)
	}
	// The shared status still tells that somebody has opened it.
	l2.get("/queue?status=read").want(t, 200, "Alpha one", "1–1 of 1")
	l1.get("/queue?unread=1").want(t, 200, "Alpha two", "Bravo one", "1–2 of 2", `name="unread" value="1" checked`).wantNot(t, "Alpha one")
	l2.get("/queue?unread=1").want(t, 200, "1–3 of 3")
	// "Mark as new" makes it unread for the one who does it.
	l1.post("/reports/"+a1+"/status", "action", "new")
	if n := strings.Count(l1.get("/queue").body, dot); n != 3 {
		t.Errorf("after mark as new: %d unread", n)
	}
	l1.post("/queue/bulk", "id", a1, "id", a2, "action", "read")
	if n := strings.Count(l1.get("/queue").body, dot); n != 1 {
		t.Errorf("after bulk read: %d unread", n)
	}
	if n := strings.Count(l2.get("/queue").body, dot); n != 3 {
		t.Errorf("bulk read by one leader changed another's view: %d", n)
	}

	// Assignment.
	l1.get("/queue").want(t, 200, "Assigned to", `<option value="me">me</option>`, `<option value="`+L2+`">Second Leader</option>`, "assign to me")
	l1.post("/queue/bulk", "id", a1, "id", b1, "action", "assign_me").want(t, 200, "2 report(s) updated")
	l1.get("/reports/"+a1).want(t, 200, "Assigned to", "<dd>Exercise leader</dd>", `<option value="`+e.id["leader"]+`" selected>`)
	l2.post("/reports/"+a2+"/assign", "assignee", L2).want(t, 200, "Report updated.", "<dd>Second Leader</dd>")
	l1.get("/queue?assignee=me").want(t, 200, "Alpha one", "Bravo one", "1–2 of 2").wantNot(t, "Alpha two")
	l2.get("/queue?assignee=me").want(t, 200, "Alpha two", "1–1 of 1")
	l1.get("/queue?assignee="+L2).want(t, 200, "Alpha two", "1–1 of 1")
	l1.get("/queue?assignee=none").want(t, 200, "0 reports")
	l1.post("/queue/bulk", "id", b1, "action", "unassign")
	l1.get("/queue?assignee=none").want(t, 200, "Bravo one", "1–1 of 1")
	l2.post("/reports/"+a2+"/assign", "assignee", "").want(t, 200).wantNot(t, "<dd>Second Leader</dd>")
	// Only leaders, only to enabled leaders, only reports in their queue.
	l1.post("/reports/"+a1+"/assign", "assignee", e.id["alpha"]).want(t, 422, "Unknown exercise leader.")
	l1.post("/reports/"+a1+"/assign", "assignee", "nope").want(t, 422)
	l1.post("/reports/nope/assign", "assignee", L2).want(t, 404)
	alpha.post("/reports/"+a1+"/assign", "assignee", L2).want(t, 403)
	toAlpha := bravo.send("group to group", "c", "receiver", e.id["alpha"])
	l1.post("/reports/"+toAlpha+"/assign", "assignee", L2).want(t, 404)
	alpha.post("/queue/bulk", "id", toAlpha, "action", "assign_me").want(t, 200, "Nothing changed: choose an action.")
	alpha.get("/queue").want(t, 200).wantNot(t, "Assigned to", "assign to me")
	alpha.get("/queue?assignee=me").want(t, 200, "group to group") // ignored for groups

	// Saved filters are personal.
	l1.get("/queue").want(t, 200, "Saved filters", "Save the current view as")
	r := l1.post("/queue/filters", "name", " Mine, unread ", "return", "assignee=me&unread=1&page=3&evil=1")
	r.want(t, 200, "Filter saved.", "Mine, unread")
	if r.path != "/queue?assignee=me&unread=1" {
		t.Errorf("after saving at %s", r.path)
	}
	l1.post("/queue/filters", "name", "From Alpha", "return", "group="+e.id["alpha"]+"&sort=subject")
	page := l1.get("/queue").want(t, 200, `href="/queue?assignee=me&amp;unread=1"`, "From Alpha")
	if strings.Index(page.body, ">From Alpha<") > strings.Index(page.body, ">Mine, unread<") {
		t.Error("saved filters are not sorted by name")
	}
	l2.get("/queue").wantNot(t, "Mine, unread", "From Alpha")
	l1.post("/queue/filters", "name", "", "return", "q=x").want(t, 200, "Give the filter a name of at most 60 characters.")
	l1.post("/queue/filters", "name", strings.Repeat("n", 61), "return", "q=x").want(t, 200, "Give the filter a name")
	saved := e.st.SavedFilters(e.id["leader"])
	if len(saved) != 2 {
		t.Fatalf("%d saved filters", len(saved))
	}
	l2.post("/queue/filters/" + saved[0].ID + "/delete")
	if len(e.st.SavedFilters(e.id["leader"])) != 2 {
		t.Error("another user deleted a saved filter")
	}
	l1.post("/queue/filters/"+saved[0].ID+"/delete").want(t, 200, "Filter deleted.")
	alpha.post("/queue/filters", "name", "Inbox new", "return", "status=new").want(t, 200, "Filter saved.", "Inbox new")
	for i := 0; i < model.MaxSavedFilters+1; i++ {
		r = l2.post("/queue/filters", "name", fmt.Sprint("f", i), "return", "q=x")
	}
	r.want(t, 200, "You have too many saved filters; delete one first.")
	e.login("admin").post("/queue/filters", "name", "x").want(t, 403)
}

func TestExportCSV(t *testing.T) {
	e := newEnv(t)
	A := e.id["alpha"]
	e.addUser("anna", "Anna Andersson", model.RoleGroup, A)
	alpha, anna, bravo, leader := e.login("alpha"), e.login("anna"), e.login("bravo"), e.login("leader")
	now := time.Date(2026, 5, 1, 22, 30, 0, 0, time.UTC)
	e.srv.Now = func() time.Time { return now }
	anna.post("/reports", "template", e.id["SITREP"], "subject", "SITREP 1", "content", "line one\nline \"two\"; with, separators",
		"f_location", "Åre", "f_status", "Red", "f_notes", "=HYPERLINK(\"http://evil\")")
	bravo.send("=1+1", "+danger\n@cmd")
	bravo.send("Plain", "-5 degrees")
	leader.post("/settings", "tz", "Europe/Stockholm")

	r := leader.get("/queue/export.csv").want(t, 200)
	if ct := r.header.Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("content type %q", ct)
	}
	if cd := r.header.Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="gr_report-queue-20260501-223000.csv"`) {
		t.Errorf("content disposition %q", cd)
	}
	if !strings.HasPrefix(r.body, "\xEF\xBB\xBF") {
		t.Error("no byte order mark")
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(r.body, "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatalf("not valid CSV: %v\n%s", err, r.body)
	}
	if len(rows) != 4 {
		t.Fatalf("%d rows, want header + 3", len(rows))
	}
	head := strings.Join(rows[0], "|")
	if !strings.HasPrefix(head, "Time (Europe/Stockholm)|Time (UTC)|From|Written by|To|Subject|Status|Folder|Assigned to|Template|Request|Content|Location|Status|Help needed|Notes") {
		t.Errorf("header = %s", head)
	}
	col := func(name string, row []string) string {
		for i, h := range rows[0] {
			if h == name {
				return row[i]
			}
		}
		t.Fatalf("no column %q", name)
		return ""
	}
	var sitrep, formula []string
	for _, row := range rows[1:] {
		switch col("Subject", row) {
		case "SITREP 1":
			sitrep = row
		case "'=1+1":
			formula = row
		}
	}
	if sitrep == nil || formula == nil {
		t.Fatalf("rows not found: %v", rows)
	}
	if col("Time (Europe/Stockholm)", sitrep) != "2026-05-02 00:30:00 CEST" || col("Time (UTC)", sitrep) != "2026-05-01T22:30:00Z" ||
		col("From", sitrep) != "Group Alpha" || col("Written by", sitrep) != "Anna Andersson" || col("To", sitrep) != "Exercise leader" ||
		col("Content", sitrep) != "line one\nline \"two\"; with, separators" || col("Location", sitrep) != "Åre" {
		t.Errorf("SITREP row = %q", sitrep)
	}
	// Cells that a spreadsheet would run as formulas are neutralised.
	if col("Notes", sitrep) != `'=HYPERLINK("http://evil")` || col("Content", formula) != "'+danger\n@cmd" || col("Location", formula) != "" {
		t.Errorf("formula guard: %q / %q", sitrep, formula)
	}
	for _, row := range rows[1:] {
		for _, cell := range row {
			if cell != "" && strings.ContainsRune("=+-@", rune(cell[0])) {
				t.Errorf("cell %q could be run as a formula", cell)
			}
		}
	}

	// The export follows the filter of the page it was started from.
	page := leader.get("/queue?q=sitrep&group="+A).want(t, 200, `href="/queue/export.csv?group=`+A+`&amp;q=sitrep"`)
	_ = page
	r = leader.get("/queue/export.csv?q=sitrep&group=" + A)
	if n := strings.Count(r.body, "SITREP 1"); n != 1 || strings.Contains(r.body, "Plain") {
		t.Errorf("filtered export: %s", r.body)
	}
	leader.hdr = map[string]string{"Accept-Language": "sv"}
	leader.get("/queue/export.csv").want(t, 200, "Ämne", "Skriven av", "Övningsledare")
	leader.hdr = nil

	// Sent list for groups; each role only its own data.
	r = alpha.get("/sent/export.csv").want(t, 200, "SITREP 1")
	if strings.Contains(r.body, "Plain") || !strings.Contains(r.header.Get("Content-Disposition"), "gr_report-sent-") {
		t.Errorf("sent export leaked or misnamed: %s", r.body)
	}
	bravo.get("/queue/export.csv").want(t, 200).wantNot(t, "SITREP 1", "Plain") // bravo's inbox is empty
	leader.get("/sent/export.csv").want(t, 403)
	e.login("admin").get("/queue/export.csv").want(t, 403)
	if got := e.anon().get("/queue/export.csv").path; got != "/login" {
		t.Errorf("anonymous export ended at %s", got)
	}
	if got := csvCell("\tx"); got != "'\tx" {
		t.Errorf("csvCell tab = %q", got)
	}
}

func TestBulkAccountImport(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin")
	admin.get("/admin/users/import").want(t, 200, "Create several accounts", "Accounts, one per line", "At most 200 accounts at once.")
	before := len(e.st.Users())

	// Any mistake stops the whole import.
	bad := "username; display name; role; group\n" +
		"charlie; Group Charlie\n" +
		"alpha; Dup of existing\n" +
		"charlie; Dup in text\n" +
		"x; Too short\n" +
		"dora; Dora; emperor\n" +
		"eve; Eve; leader; charlie\n" +
		"finn; Finn; group; nosuchgroup\n" +
		"gus; Gus; group; leader\n" +
		"onlyone\n" +
		"a; b; c; d; e\n"
	r := admin.post("/admin/users/import", "accounts", bad).want(t, 422, "No account was created.", "Line 3: the username &#34;alpha&#34; is already in use",
		"Line 4: the username &#34;charlie&#34; occurs twice", "Line 5: username must be", "Line 6: unknown role",
		"Line 7: only an account with the group role can belong to a group", "Line 8: there is no group &#34;nosuchgroup&#34;",
		"Line 9: there is no group &#34;leader&#34;", "Line 10: expected: username; display name; role; group", "Line 11: expected")
	r.want(t, 422, "charlie; Group Charlie") // the text is kept for correction
	if len(e.st.Users()) != before {
		t.Fatalf("accounts created by a failed import")
	}
	admin.post("/admin/users/import", "accounts", "  \n# only a comment\n").want(t, 422, "Nothing to create")
	admin.post("/admin/users/import", "accounts", strings.Repeat("u; U\n", 0)+func() string {
		var b strings.Builder
		for i := 0; i < maxImport+1; i++ {
			fmt.Fprintf(&b, "user%03d; User %d\n", i, i)
		}
		return b.String()
	}()).want(t, 422, "At most 200 accounts can be created at once.")
	if len(e.st.Users()) != before {
		t.Fatalf("accounts created by an oversized import")
	}

	// A good batch: groups, members (also of an existing group), a leader;
	// semicolons, commas and tabs.
	good := "Username; Display name; Role; Group\n" +
		"charlie; Group Charlie\n" +
		"# members\n" +
		"cia, Cia Carlsson, group, charlie\n" +
		"carl\tCarl Ceder\t\tCHARLIE\n" +
		"anna; Anna Andersson; group; alpha\n" +
		"staff2; Maria Lind; Leader\n"
	r = admin.post("/admin/users/import", "accounts", good).want(t, 200, "5 accounts created.", "shown only now", "Group Charlie", "Cia Carlsson", "Maria Lind",
		"<td>Group Alpha</td>", "<code>staff2</code>")
	if len(e.st.Users()) != before+5 {
		t.Fatalf("%d accounts created", len(e.st.Users())-before)
	}
	charlie, _ := e.st.UserByUsername("charlie")
	cia, _ := e.st.UserByUsername("cia")
	carl, _ := e.st.UserByUsername("carl")
	anna, _ := e.st.UserByUsername("anna")
	staff, _ := e.st.UserByUsername("staff2")
	if charlie.GroupID != "" || cia.GroupID != charlie.ID || carl.GroupID != charlie.ID || anna.GroupID != e.id["alpha"] ||
		staff.Role != model.RoleLeader || !cia.MustChange || carl.Role != model.RoleGroup {
		t.Errorf("imported accounts: %+v %+v %+v %+v %+v", charlie, cia, carl, anna, staff)
	}
	// The one-time passwords on the page work, and only once.
	i := strings.Index(r.body, "<code>cia</code></td><td><code>")
	if i < 0 {
		t.Fatalf("no password for cia on the page")
	}
	otp := r.body[i+len("<code>cia</code></td><td><code>"):][:16]
	c := e.anon()
	res := c.postRaw("/login", url.Values{"username": {"cia"}, "password": {otp}}, nil).want(t, 200, "one-time password")
	if res.path != "/password" {
		t.Errorf("first login of an imported account at %s", res.path)
	}
	admin.get("/admin/users").want(t, 200, "Cia Carlsson", "<td>Group Charlie</td>", "Create several accounts at once").wantNot(t, otp)
	admin.get("/admin/audit").want(t, 200, "users_imported", "count=5").wantNot(t, otp)
	e.login("leader").get("/admin/users/import").want(t, 403)
	e.login("leader").post("/admin/users/import", "accounts", "zed; Zed").want(t, 403)
}

func TestAuditLogViewer(t *testing.T) {
	e := newEnv(t)
	alpha, leader, admin := e.login("alpha"), e.login("leader"), e.login("admin")
	e.anon().postRaw("/login", url.Values{"username": {"alpha"}, "password": {"wrong-password-value"}}, nil)
	e.anon().postRaw("/login", url.Values{"username": {"ghost"}, "password": {"x"}}, nil)
	id := alpha.send("Subject that must not be logged", "content that must not be logged")
	leader.post("/reports/"+id+"/status", "action", "file", "folder", "Logistics")
	alpha.post("/chat/all", "body", "chat text that must not be logged")
	admin.post("/admin/users", "username", "charlie", "display_name", "Group Charlie", "role", "group")
	leader.get("/queue/export.csv")

	r := admin.get("/admin/audit").want(t, 200, "Audit log", "<code>login</code>", "<code>login_failed</code>", "<code>report_sent</code>", "<code>reports_file</code>",
		"<code>chat_sent</code>", "<code>user_created</code>", "<code>export</code>", "folder=Logistics", "target=charlie", "ghost", "127.0.0.1", "Times in UTC")
	// Never content or secrets.
	r.wantNot(t, "must not be logged", "wrong-password-value", pw("alpha"), "pbkdf2", "argon2")
	for _, entry := range e.st.Audit("", 0, 1000) {
		if strings.Contains(entry.Detail, "must not be logged") || strings.Contains(entry.Detail, "wrong-password") {
			t.Errorf("content in the audit log: %+v", entry)
		}
	}
	// Newest first.
	if strings.Index(r.body, "<code>export</code>") > strings.Index(r.body, "<code>report_sent</code>") {
		t.Error("audit log is not newest first")
	}
	admin.get("/admin/audit?q=login_failed").want(t, 200, "ghost", `value="login_failed"`, "Clear filter").wantNot(t, "<code>report_sent</code>")
	admin.get("/admin/audit?q=CHARLIE").want(t, 200, "<code>user_created</code>").wantNot(t, "<code>chat_sent</code>")
	admin.get("/admin/audit?q=nomatch").want(t, 200, "No entries.")
	admin.get("/admin/audit?q="+url.QueryEscape(`<script>alert(1)</script>`)).want(t, 200).wantNot(t, "<script>alert")

	// Paging.
	for i := 0; i < auditPageSize+20; i++ {
		_ = e.st.AddAudit(model.AuditEntry{Time: time.Now(), User: "bulk", Event: fmt.Sprintf("bulk-%03d", i)})
	}
	p1 := admin.get("/admin/audit?q=bulk-").want(t, 200, "bulk-119", "bulk-020", "Older entries").wantNot(t, "bulk-019")
	i := strings.Index(p1.body, `href="/admin/audit?before=`)
	if i < 0 {
		t.Fatal("no link to older entries")
	}
	next := strings.ReplaceAll(p1.body[i+6:i+6+strings.Index(p1.body[i+6:], `"`)], "&amp;", "&")
	admin.get(next).want(t, 200, "bulk-019", "bulk-000").wantNot(t, "bulk-020", "Older entries")
	admin.get("/admin/audit?before=-5").want(t, 200)

	// Administrators only.
	leader.get("/admin/audit").want(t, 403)
	alpha.get("/admin/audit").want(t, 403)
	// Swedish, and time zone.
	admin.hdr = map[string]string{"Accept-Language": "sv"}
	admin.get("/admin/audit?q=export").want(t, 200, "Granskningslogg", "Händelse", "övningsledare")
	// The log survives when the database refuses the entry.
	e.st.Close()
	e.srv.auditEvent(mustRequest(t), 0, "after_close", "x", "")
}

func mustRequest(t *testing.T) *http.Request {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, "http://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.RemoteAddr = "192.0.2.1:1"
	return r
}

func TestNewExercise(t *testing.T) {
	e := newEnv(t)
	alpha, bravo, leader, admin := e.login("alpha"), e.login("bravo"), e.login("leader"), e.login("admin")
	now := time.Date(2026, 5, 1, 16, 0, 0, 0, time.UTC)
	e.srv.Now = func() time.Time { return now }
	alpha.send("Report one", "c")
	bravo.send("Report two", "c")
	leader.post("/requests", "subject", "Q", "template", e.id["SITREP"], "to", "all")
	alpha.post("/chat/all", "body", "hello")
	alpha.post("/reports", "template", e.id["SITREP"], "action", "draft", "subject", "draft")
	leader.post("/queue/filters", "name", "kept filter", "return", "status=new")

	leader.get("/exercise").want(t, 200, "Current exercise", "<dt>Reports</dt><dd>2</dd>", "<dt>Requests</dt><dd>1</dd>", "<dt>Chat messages</dt><dd>1</dd>",
		"<dt>Drafts</dt><dd>1</dd>", "Type RESET to confirm", "No archived exercises.", "Export all reports")
	admin.get("/exercise").want(t, 200, "Start a new exercise").wantNot(t, "Export all reports") // no access to content
	alpha.get("/exercise").want(t, 403)
	alpha.post("/exercise/reset", "confirm", "RESET").want(t, 403)

	// Without the confirmation word nothing happens.
	for _, word := range []string{"", "reset", "RESET ", "yes"} {
		leader.post("/exercise/reset", "label", "Spring drill", "confirm", word).want(t, 422, "Type RESET in the confirmation field", "Nothing was changed.", `value="Spring drill"`)
	}
	if c := e.st.Counts(); c.Reports != 2 || c.ChatMessages != 1 {
		t.Fatalf("data removed without confirmation: %+v", c)
	}
	if copies, _ := e.st.Copies(store.ArchiveDir); len(copies) != 0 {
		t.Fatalf("archive written without confirmation")
	}

	r := leader.post("/exercise/reset", "label", " Spring Drill 2026 / Åre! ", "confirm", "RESET")
	const name = "20260501-160000-spring-drill-2026-re.db"
	r.want(t, 200, "A new exercise has started.", name, "<dt>Reports</dt><dd>0</dd>", "<dt>Chat messages</dt><dd>0</dd>", "<code>"+name+"</code>")
	if c := e.st.Counts(); c != (store.ExerciseCounts{}) {
		t.Errorf("after reset: %+v", c)
	}
	// Accounts, templates and saved filters are kept; everyone can go on.
	alpha.get("/home").want(t, 200, "Nothing sent yet", "SITREP").wantNot(t, "Report one", "Your drafts")
	alpha.get("/chat/all").want(t, 200, "No messages yet.")
	leader.get("/queue").want(t, 200, "0 reports", "kept filter", "Group Alpha")
	alpha.send("First report of the new exercise", "c")
	// The archive is a complete database of the old exercise.
	path := filepath.Join(e.dir, store.ArchiveDir, name)
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("archive file: %v %v", fi, err)
	}
	dir := t.TempDir()
	raw, _ := os.ReadFile(path)
	_ = os.WriteFile(filepath.Join(dir, store.DBName), raw, 0o600)
	old, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if c := old.Counts(); c.Reports != 2 || c.Requests != 1 || c.ChatMessages != 1 {
		t.Errorf("archive content: %+v", c)
	}
	admin.get("/admin/audit?q=exercise_reset").want(t, 200, "archive="+name, "reports=2", "chat_messages=1")

	// An administrator can do it too; without a label the name is the time.
	now = now.Add(time.Hour)
	admin.post("/exercise/reset", "confirm", "RESET").want(t, 200, "20260501-170000.db")
	if c := e.st.Counts(); c.Reports != 0 {
		t.Errorf("after second reset: %+v", c)
	}
	// If the archive cannot be written, nothing is removed.
	alpha.send("Must survive", "c")
	leader.post("/exercise/reset", "confirm", "RESET").want(t, 500) // same second: the file exists
	if c := e.st.Counts(); c.Reports != 1 {
		t.Errorf("data removed although archiving failed: %+v", c)
	}
	leader.hdr = map[string]string{"Accept-Language": "sv"}
	leader.get("/exercise").want(t, 200, "Starta en ny övning", "Skriv RESET för att bekräfta", "Arkiverade övningar")
}

func TestBackupPage(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin")
	now := time.Date(2026, 5, 1, 16, 0, 0, 0, time.UTC)
	e.srv.Now = func() time.Time { return now }
	e.login("alpha").send("In the backup", "c")

	admin.get("/admin/backup").want(t, 200, "Backup", "Make a backup now", "No backups yet.", "cannot be downloaded here")
	r := admin.post("/admin/backup").want(t, 200, "Backup manual-20260501-160000.db created.", "<code>manual-20260501-160000.db</code>").wantNot(t, "No backups yet.")
	_ = r
	path := filepath.Join(e.dir, store.BackupDir, "manual-20260501-160000.db")
	fi, err := os.Stat(path)
	if err != nil || fi.Size() == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup file: %v %v", fi, err)
	}
	// It is a usable database with the data in it.
	dir := t.TempDir()
	raw, _ := os.ReadFile(path)
	_ = os.WriteFile(filepath.Join(dir, store.DBName), raw, 0o600)
	copyDB, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	if rs := copyDB.Reports(nil); len(rs) != 1 || rs[0].Subject != "In the backup" {
		t.Errorf("backup content: %+v", rs)
	}
	admin.post("/admin/backup").want(t, 500) // same second: never overwrite a backup
	now = now.Add(time.Minute)
	admin.post("/admin/backup").want(t, 200, "manual-20260501-160100.db", "manual-20260501-160000.db")
	admin.get("/admin/audit?q=backup_created").want(t, 200, "file=manual-20260501-160000.db")
	// Nothing is served from the backup folder.
	admin.get("/admin/backup/manual-20260501-160000.db").want(t, 404)
	admin.get("/static/../backups/manual-20260501-160000.db").want(t, 404)
	// Administrators only.
	e.login("leader").get("/admin/backup").want(t, 403)
	e.login("leader").post("/admin/backup").want(t, 403)
	admin.hdr = map[string]string{"Accept-Language": "sv"}
	admin.get("/admin/backup").want(t, 200, "Säkerhetskopia", "Gör en säkerhetskopia nu", "Filnamn")
}
