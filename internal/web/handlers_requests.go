package web

import (
	"net/http"

	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/model"
)

// requestRows lists the requests visible to the user with response counts.
func (s *Server) requestRows(c *reqCtx) []requestRow {
	leader := c.user.Role == model.RoleLeader
	counts := map[string]int{}
	for _, r := range s.Store.Reports(nil) {
		if r.RequestID != "" && (leader || r.SenderID == c.user.ID) {
			counts[r.RequestID]++
		}
	}
	var rows []requestRow
	for _, q := range s.Store.Requests() {
		if !leader && !q.Addressed(c.user.ID) {
			continue
		}
		t, _ := s.Store.Template(q.TemplateID)
		rows = append(rows, requestRow{Req: q, TemplateName: t.Name, Responses: counts[q.ID]})
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
}

func (s *Server) requestFormPage(c *reqCtx, status int, q model.Request, errMsg string) {
	v := &requestFormView{Req: q, Templates: s.Store.Templates(false), Groups: s.Store.Groups(), Selected: map[string]bool{}}
	for _, id := range q.Recipients {
		v.Selected[id] = true
	}
	s.render(c, status, "request_new", c.t("New request"), v, errMsg)
}

func (s *Server) requestForm(c *reqCtx) {
	s.requestFormPage(c, http.StatusOK, model.Request{TemplateID: c.r.URL.Query().Get("template")}, "")
}

func (s *Server) requestCreate(c *reqCtx) {
	f := c.r.PostForm
	q := model.Request{ID: model.NewID(), Subject: f.Get("subject"), Body: f.Get("body"),
		TemplateID: f.Get("template"), CreatedBy: c.user.ID, Created: s.Now().UTC()}
	if f.Get("to") != "all" {
		q.Recipients = f["recipient"]
	}
	bad := func(msg string) { s.requestFormPage(c, http.StatusUnprocessableEntity, q, msg) }
	if err := q.Validate(); err != nil {
		bad(i18n.TErr(c.lang, err))
		return
	}
	if f.Get("to") != "all" && len(q.Recipients) == 0 {
		bad(c.t("Select at least one group, or send to all groups."))
		return
	}
	if t, ok := s.Store.Template(q.TemplateID); !ok || t.Archived {
		bad(c.t("Select a report template."))
		return
	}
	for _, id := range q.Recipients {
		if g, ok := s.Store.User(id); !ok || g.Role != model.RoleGroup {
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
	Name  string
	Count int
}

type requestView struct {
	Req        model.Request
	Template   model.Template
	Leader     bool
	Recipients []recipientRow
	Responses  []model.Report
}

func (s *Server) requestView(c *reqCtx) {
	q, ok := s.Store.Request(c.r.PathValue("id"))
	leader := c.user.Role == model.RoleLeader
	if !ok || (!leader && !q.Addressed(c.user.ID)) {
		s.fail(c, http.StatusNotFound, c.t("No such request."))
		return
	}
	v := &requestView{Req: q, Leader: leader}
	v.Template, _ = s.Store.Template(q.TemplateID)
	f := model.ReportFilter{RequestID: q.ID}
	if !leader {
		f.SenderID = c.user.ID
	}
	v.Responses = s.Store.Reports(&f)
	model.SortReports(v.Responses, model.SortTime, true)
	if leader {
		counts := map[string]int{}
		for _, r := range v.Responses {
			counts[r.SenderID]++
		}
		for _, g := range s.Store.Users() {
			if g.Role == model.RoleGroup && q.Addressed(g.ID) && (!g.Disabled || counts[g.ID] > 0) {
				v.Recipients = append(v.Recipients, recipientRow{Name: g.DisplayName, Count: counts[g.ID]})
			}
		}
	}
	s.render(c, http.StatusOK, "request", q.Subject, v, "")
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
