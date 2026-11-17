package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/model"
)

// requestRows lists the requests visible to the user with response counts.
func (s *Server) requestRows(c *reqCtx) []requestRow {
	leader := c.user.Role == model.RoleLeader
	counts := map[string]int{}
	for _, r := range s.Store.Reports(nil) {
		if r.RequestID != "" && (leader || r.SenderID == c.party) {
			counts[r.RequestID]++
		}
	}
	var rows []requestRow
	for _, q := range s.Store.Requests() {
		if !leader && !q.Addressed(c.party) {
			continue
		}
		t, _ := s.Store.Template(q.TemplateID)
		// For a group a deadline only presses while it has not answered.
		overdue := q.Overdue(s.Now()) && (leader || counts[q.ID] == 0)
		rows = append(rows, requestRow{Req: q, TemplateName: t.Name, Responses: counts[q.ID], Overdue: overdue})
	}
	return rows
}

func (s *Server) requests(c *reqCtx) {
	s.render(c, http.StatusOK, "requests", c.t("Requests"), s.requestRows(c), "")
}

type requestFormView struct {
	Req       model.Request
	Templates []model.Template
	Groups    []model.User
	Selected  map[string]bool
	Due       string // the deadline as typed, in the user's time zone
}

// parseDue reads a deadline from a datetime-local field in the user's time
// zone. An empty field means no deadline.
func (c *reqCtx) parseDue(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.ParseInLocation(model.DatetimeLayout, v, c.loc)
	if err != nil {
		return time.Time{}, i18n.Errorf("the deadline must be a date and time")
	}
	return t.UTC(), nil
}

// dueField formats a deadline for a datetime-local field.
func (c *reqCtx) dueField(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(c.loc).Format(model.DatetimeLayout)
}

func (s *Server) requestFormPage(c *reqCtx, status int, q model.Request, due, errMsg string) {
	v := &requestFormView{Req: q, Templates: s.Store.Templates(false), Groups: s.Store.Groups(), Selected: map[string]bool{}, Due: due}
	for _, id := range q.Recipients {
		v.Selected[id] = true
	}
	s.render(c, status, "request_new", c.t("New request"), v, errMsg)
}

func (s *Server) requestForm(c *reqCtx) {
	s.requestFormPage(c, http.StatusOK, model.Request{TemplateID: c.r.URL.Query().Get("template")}, "", "")
}

func (s *Server) requestCreate(c *reqCtx) {
	f := c.r.PostForm
	q := model.Request{ID: model.NewID(), Subject: f.Get("subject"), Body: f.Get("body"),
		TemplateID: f.Get("template"), CreatedBy: c.user.ID, Created: s.Now().UTC()}
	if f.Get("to") != "all" {
		q.Recipients = f["recipient"]
	}
	bad := func(msg string) { s.requestFormPage(c, http.StatusUnprocessableEntity, q, f.Get("due"), msg) }
	if err := q.Validate(); err != nil {
		bad(i18n.TErr(c.lang, err))
		return
	}
	due, err := c.parseDue(f.Get("due"))
	if err != nil {
		bad(i18n.TErr(c.lang, err))
		return
	}
	if !due.IsZero() && !due.After(q.Created) {
		bad(c.t("The deadline must be in the future."))
		return
	}
	q.Due = due
	if f.Get("to") != "all" && len(q.Recipients) == 0 {
		bad(c.t("Select at least one group, or send to all groups."))
		return
	}
	if t, ok := s.Store.Template(q.TemplateID); !ok || t.Archived {
		bad(c.t("Select a report template."))
		return
	}
	for _, id := range q.Recipients {
		if g, ok := s.Store.User(id); !ok || g.Role != model.RoleGroup || g.GroupID != "" {
			bad(c.t("Unknown recipient."))
			return
		}
	}
	if s.storeErr(c, s.Store.PutRequest(q)) {
		return
	}
	s.audit(c, "request_sent", "request", q.ID, "recipients", len(q.Recipients))
	s.redirect(c, "/requests/"+q.ID, c.t("Request sent."))
}

type recipientRow struct {
	Name    string
	Count   int
	Overdue bool // no answer although the deadline has passed
}

type responseRow struct {
	model.Report
	Late bool // sent after the deadline
}

type requestView struct {
	Req        model.Request
	Template   model.Template
	Leader     bool
	Recipients []recipientRow
	Responses  []responseRow
	Overdue    bool
	Due        string // for the form that changes the deadline
}

func (s *Server) requestView(c *reqCtx) {
	q, ok := s.Store.Request(c.r.PathValue("id"))
	leader := c.user.Role == model.RoleLeader
	if !ok || (!leader && !q.Addressed(c.party)) {
		s.fail(c, http.StatusNotFound, c.t("No such request."))
		return
	}
	v := &requestView{Req: q, Leader: leader, Due: c.dueField(q.Due)}
	v.Template, _ = s.Store.Template(q.TemplateID)
	f := model.ReportFilter{RequestID: q.ID}
	if !leader {
		f.SenderID = c.party
	}
	responses := s.Store.Reports(&f)
	model.SortReports(responses, model.SortTime, true)
	for _, r := range responses {
		v.Responses = append(v.Responses, responseRow{Report: r, Late: !q.Due.IsZero() && r.Timestamp.After(q.Due)})
	}
	overdue := q.Overdue(s.Now())
	v.Overdue = overdue && (leader || len(responses) == 0)
	if leader {
		counts := map[string]int{}
		for _, r := range responses {
			counts[r.SenderID]++
		}
		for _, g := range s.Store.Users() {
			if g.Role == model.RoleGroup && g.GroupID == "" && q.Addressed(g.ID) && (!g.Disabled || counts[g.ID] > 0) {
				v.Recipients = append(v.Recipients, recipientRow{Name: g.DisplayName, Count: counts[g.ID], Overdue: overdue && counts[g.ID] == 0})
			}
		}
	}
	s.render(c, http.StatusOK, "request", q.Subject, v, "")
}

// requestDue sets, moves or removes the deadline of a request.
func (s *Server) requestDue(c *reqCtx) {
	q, ok := s.Store.Request(c.r.PathValue("id"))
	if !ok {
		s.fail(c, http.StatusNotFound, c.t("No such request."))
		return
	}
	due, err := c.parseDue(c.r.PostForm.Get("due"))
	if err != nil {
		s.fail(c, http.StatusUnprocessableEntity, i18n.TErr(c.lang, err))
		return
	}
	q.Due = due
	if s.storeErr(c, s.Store.PutRequest(q)) {
		return
	}
	s.audit(c, "request_deadline", "request", q.ID, "due", fmtTime(due))
	msg := c.t("Deadline removed.")
	if !due.IsZero() {
		msg = c.t("Deadline set to %s.", c.ts(due))
	}
	s.redirect(c, "/requests/"+q.ID, msg)
}

func (s *Server) requestClose(c *reqCtx) {
	q, ok := s.Store.Request(c.r.PathValue("id"))
	if !ok {
		s.fail(c, http.StatusNotFound, c.t("No such request."))
		return
	}
	q.Closed = c.r.PostForm.Get("closed") == "1"
	if s.storeErr(c, s.Store.PutRequest(q)) {
		return
	}
	s.audit(c, "request_closed", "request", q.ID, "closed", q.Closed)
	msg := c.t("Request reopened.")
	if q.Closed {
		msg = c.t("Request closed.")
	}
	s.redirect(c, "/requests/"+q.ID, msg)
}
