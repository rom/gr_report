package web

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/model"
)

const pageSize = 100

// ---- group home ----

type requestRow struct {
	Req          model.Request
	TemplateName string
	Responses    int // leader: all responses; group: own responses
}

type homeView struct {
	Requests  []requestRow
	Templates []model.Template
	Recent    []model.Report
}

func (s *Server) home(c *reqCtx) {
	v := &homeView{Templates: s.Store.Templates(false)}
	for _, row := range s.requestRows(c) {
		if !row.Req.Closed {
			v.Requests = append(v.Requests, row)
		}
	}
	v.Recent = s.Store.Reports(&model.ReportFilter{SenderID: c.user.ID})
	model.SortReports(v.Recent, model.SortTime, true)
	if len(v.Recent) > 10 {
		v.Recent = v.Recent[:10]
	}
	s.render(c, http.StatusOK, "home", c.t("Home"), v, "")
}

// ---- writing a report ----

type reportFormView struct {
	Template  model.Template
	Request   *model.Request
	Receivers []model.User
	Receiver  string
	Subject   string
	Content   string
	Values    map[string]string
}

// reportContext resolves and authorises the template and optional request a
// new report refers to.
func (s *Server) reportContext(c *reqCtx, templateID, requestID string) (*reportFormView, bool) {
	v := &reportFormView{Receiver: model.ReceiverLeader, Values: map[string]string{}}
	if requestID != "" {
		q, ok := s.Store.Request(requestID)
		if !ok || !q.Addressed(c.user.ID) {
			s.fail(c, http.StatusNotFound, c.t("No such request."))
			return nil, false
		}
		if q.Closed {
			s.fail(c, http.StatusUnprocessableEntity, c.t("This request is closed."))
			return nil, false
		}
		v.Request = &q
		templateID = q.TemplateID
		v.Subject = c.t("Re: %s", q.Subject)
	}
	t, ok := s.Store.Template(templateID)
	if !ok || (t.Archived && v.Request == nil) {
		s.fail(c, http.StatusNotFound, c.t("No such template."))
		return nil, false
	}
	v.Template = t
	if v.Request == nil {
		v.Subject = t.DefaultSubject
		for _, g := range s.Store.Groups() {
			if g.ID != c.user.ID {
				v.Receivers = append(v.Receivers, g)
			}
		}
	}
	return v, true
}

func (s *Server) reportForm(c *reqCtx) {
	q := c.r.URL.Query()
	v, ok := s.reportContext(c, q.Get("template"), q.Get("request"))
	if !ok {
		return
	}
	s.render(c, http.StatusOK, "report_new", c.t("New report"), v, "")
}

func (s *Server) reportCreate(c *reqCtx) {
	f := c.r.PostForm
	v, ok := s.reportContext(c, f.Get("template"), f.Get("request"))
	if !ok {
		return
	}
	r := model.Report{
		ID: model.NewID(), Timestamp: s.Now().UTC(), SenderID: c.user.ID, SenderName: c.user.DisplayName,
		ReceiverID: model.ReceiverLeader, ReceiverName: model.ReceiverLeaderName, Status: model.StatusNew,
	}
	v.Subject, v.Content = f.Get("subject"), f.Get("content")
	for _, fd := range v.Template.Fields {
		v.Values[fd.Key] = f.Get("f_" + fd.Key)
	}
	bad := func(msg string) {
		s.render(c, http.StatusUnprocessableEntity, "report_new", c.t("New report"), v, msg)
	}
	if v.Request != nil {
		r.RequestID = v.Request.ID
	} else if rcv := f.Get("receiver"); rcv != "" && rcv != model.ReceiverLeader {
		v.Receiver = rcv
		g, ok := s.Store.User(rcv)
		if !ok || g.Role != model.RoleGroup || g.Disabled || g.ID == c.user.ID {
			bad(c.t("Unknown receiver."))
			return
		}
		r.ReceiverID, r.ReceiverName = g.ID, g.DisplayName
	}
	if err := model.FillReport(&r, &v.Template, v.Subject, v.Content, func(k string) string { return v.Values[k] }); err != nil {
		bad(i18n.TErr(c.lang, err))
		return
	}
	if s.storeErr(c, s.Store.PutReport(r)) {
		return
	}
	s.audit(c, "report_sent", "report", r.ID, "receiver", r.ReceiverID)
	to := r.ReceiverName
	if r.ReceiverID == model.ReceiverLeader {
		to = c.t(model.ReceiverLeaderName)
	}
	s.redirect(c, "/reports/"+r.ID, c.t("Report sent to %s.", to))
}

// ---- viewing and filing ----

type reportView struct {
	Report  model.Report
	Request *model.Request
	Mine    bool // the viewer handles the queue this report is in
	Folders []string
}

func (s *Server) reportView(c *reqCtx) {
	r, ok := s.Store.Report(c.r.PathValue("id"))
	mine := ok && r.ReceiverID == queueID(&c.user)
	// Leaders may read everything; groups only what they sent or received.
	visible := mine || r.SenderID == c.user.ID || c.user.Role == model.RoleLeader
	if !ok || !visible {
		s.fail(c, http.StatusNotFound, c.t("No such report."))
		return
	}
	if mine && r.Status == model.StatusNew {
		upd, err := s.Store.UpdateReport(r.ID, func(r *model.Report) bool {
			r.Status = model.StatusRead
			return true
		})
		if s.storeErr(c, err) {
			return
		}
		r = upd
	}
	v := &reportView{Report: r, Mine: mine}
	if mine {
		v.Folders = s.Store.Stats(r.ReceiverID).Folders
	}
	if q, ok := s.Store.Request(r.RequestID); ok && r.RequestID != "" {
		v.Request = &q
	}
	s.render(c, http.StatusOK, "report", r.Subject, v, "")
}

// applyStatus performs a queue action on the given reports and returns how
// many were changed. Reports outside the user's own queue are skipped.
func (s *Server) applyStatus(c *reqCtx, ids []string, action, folder string) (int, error) {
	var status model.Status
	switch action {
	case "new":
		status = model.StatusNew
	case "read":
		status = model.StatusRead
	case "file":
		status = model.StatusFiled
	default:
		return 0, i18n.Errorf("choose an action")
	}
	folder, err := model.CleanFolder(folder)
	if err != nil {
		return 0, err
	}
	if len(ids) > 5*pageSize {
		return 0, i18n.Errorf("too many reports selected")
	}
	mine := queueID(&c.user)
	n := 0
	for _, id := range ids {
		changed := false
		_, err := s.Store.UpdateReport(id, func(r *model.Report) bool {
			if r.ReceiverID != mine {
				return false
			}
			r.Status = status
			if status == model.StatusFiled {
				r.Folder = folder
			}
			changed = true
			return true
		})
		if changed && err == nil {
			n++
		}
	}
	if n > 0 {
		s.audit(c, "reports_"+action, "count", n, "folder", folder)
	}
	return n, nil
}

func (s *Server) reportStatus(c *reqCtx) {
	id := c.r.PathValue("id")
	n, err := s.applyStatus(c, []string{id}, c.r.PostForm.Get("action"), c.r.PostForm.Get("folder"))
	if err != nil {
		s.fail(c, http.StatusUnprocessableEntity, i18n.TErr(c.lang, err))
		return
	}
	if n == 0 {
		s.fail(c, http.StatusNotFound, c.t("No such report."))
		return
	}
	if c.r.PostForm.Get("action") == "new" {
		// Opening the report again would mark it read: go to the queue.
		s.redirect(c, "/queue", c.t("Report marked as new."))
		return
	}
	s.redirect(c, "/reports/"+id, c.t("Report updated."))
}

func (s *Server) queueBulk(c *reqCtx) {
	f := c.r.PostForm
	back := "/queue"
	if q, err := url.ParseQuery(f.Get("return")); err == nil && len(q) > 0 {
		back += "?" + parseQueueParams(q).values().Encode()
	}
	n, err := s.applyStatus(c, f["id"], f.Get("action"), f.Get("folder"))
	if err != nil {
		s.redirect(c, back, c.t("Nothing changed: %s.", i18n.TErr(c.lang, err)))
		return
	}
	s.redirect(c, back, c.t("%d report(s) updated.", n))
}

// ---- queue ----

type queueParams struct {
	Group, Status, Template, Folder, Request, Q, From, To string
	Sort                                                  string
	Asc                                                   bool
	Page                                                  int
	Auto                                                  bool
}

func parseQueueParams(v url.Values) queueParams {
	p := queueParams{
		Group: v.Get("group"), Status: v.Get("status"), Template: v.Get("template"),
		Folder: model.CleanLine(v.Get("folder")), Request: v.Get("request"), Q: model.CleanLine(v.Get("q")),
		From: v.Get("from"), To: v.Get("to"), Sort: v.Get("sort"),
		Asc: v.Get("dir") == "asc", Auto: v.Get("auto") == "1",
	}
	for _, id := range []*string{&p.Group, &p.Template, &p.Request} {
		if !model.ValidID(*id) {
			*id = ""
		}
	}
	if !model.Status(p.Status).Valid() {
		p.Status = ""
	}
	// Cut on a character boundary and clean again: the cut may expose
	// trailing white space.
	if r := []rune(p.Q); len(r) > model.MaxSubjectLen {
		p.Q = model.CleanLine(string(r[:model.MaxSubjectLen]))
	}
	if len(p.Folder) > model.MaxSubjectLen {
		p.Folder = ""
	}
	if model.ParseDay(p.From).IsZero() {
		p.From = ""
	}
	if model.ParseDay(p.To).IsZero() {
		p.To = ""
	}
	valid := false
	for _, k := range model.SortKeys {
		valid = valid || k == p.Sort
	}
	if !valid {
		p.Sort = model.SortTime
	}
	if n, err := strconv.Atoi(v.Get("page")); err == nil && n > 0 && n < 1_000_000 {
		p.Page = n
	}
	return p
}

func (p queueParams) values() url.Values {
	v := url.Values{}
	set := func(k, val string) {
		if val != "" {
			v.Set(k, val)
		}
	}
	set("group", p.Group)
	set("status", p.Status)
	set("template", p.Template)
	set("folder", p.Folder)
	set("request", p.Request)
	set("q", p.Q)
	set("from", p.From)
	set("to", p.To)
	if p.Sort != model.SortTime {
		v.Set("sort", p.Sort)
	}
	if p.Asc {
		v.Set("dir", "asc")
	}
	if p.Page > 0 {
		v.Set("page", strconv.Itoa(p.Page))
	}
	if p.Auto {
		v.Set("auto", "1")
	}
	return v
}

func (p queueParams) link(base string) string {
	if q := p.values().Encode(); q != "" {
		return base + "?" + q
	}
	return base
}

type queueLink struct {
	Name       string
	URL        string
	New, Total int
	Active     bool
}

type sortLink struct {
	Label, URL, Arrow string
	Aria              string // value of aria-sort
}

type queueView struct {
	Base      string
	Sent      bool
	P         queueParams
	Queues    []queueLink
	Rows      []model.Report
	Total     int
	First     int
	Last      int
	Templates []model.Template
	Requests  []model.Request
	Folders   []string
	Sorts     []sortLink
	PrevURL   string
	NextURL   string
	AutoURL   string
	Return    string
	Filtered  bool
}

func (s *Server) queue(c *reqCtx) { s.list(c, false) }
func (s *Server) sent(c *reqCtx)  { s.list(c, true) }

func (s *Server) list(c *reqCtx, sent bool) {
	p := parseQueueParams(c.r.URL.Query())
	v := &queueView{Base: "/queue", Sent: sent, P: p, Templates: s.Store.Templates(true)}
	f := model.ReportFilter{
		Status: model.Status(p.Status), TemplateID: p.Template, RequestID: p.Request,
		Folder: p.Folder, Query: p.Q, From: model.ParseDay(p.From),
	}
	if to := model.ParseDay(p.To); !to.IsZero() {
		f.To = to.Add(24 * time.Hour)
	}
	title := c.t("Inbox")
	if sent {
		v.Base, title = "/sent", c.t("Sent reports")
		f.SenderID = c.user.ID
		p.Group, v.P.Group = "", ""
	} else {
		f.ReceiverID, f.SenderID = queueID(&c.user), p.Group
		stats := s.Store.Stats(f.ReceiverID)
		v.Folders = stats.Folders
		all := queueLink{Name: c.t("All queues"), Active: p.Group == ""}
		one := p
		one.Page = 0
		for _, g := range s.Store.Users() {
			// Leaders get one queue per group; a group sees one queue
			// per group that has written to it.
			if g.Role != model.RoleGroup || g.ID == c.user.ID {
				continue
			}
			if c.user.Role != model.RoleLeader && stats.Total[g.ID] == 0 {
				continue
			}
			if g.Disabled && stats.Total[g.ID] == 0 {
				continue
			}
			one.Group = g.ID
			v.Queues = append(v.Queues, queueLink{Name: g.DisplayName, URL: one.link(v.Base),
				New: stats.New[g.ID], Total: stats.Total[g.ID], Active: p.Group == g.ID})
			all.New += stats.New[g.ID]
			all.Total += stats.Total[g.ID]
		}
		one.Group = ""
		all.URL = one.link(v.Base)
		v.Queues = append([]queueLink{all}, v.Queues...)
		if c.user.Role == model.RoleLeader {
			title = c.t("Report queue")
			v.Requests = s.Store.Requests()
		}
	}
	rows := s.Store.Reports(&f)
	model.SortReports(rows, p.Sort, !p.Asc)
	v.Total = len(rows)
	start := p.Page * pageSize
	if start > len(rows) {
		start = len(rows)
	}
	end := min(start+pageSize, len(rows))
	v.Rows = rows[start:end]
	v.First, v.Last = start+1, end
	v.Filtered = p.Status != "" || p.Template != "" || p.Request != "" || p.Folder != "" || p.Q != "" || p.From != "" || p.To != ""

	labels := map[string]string{model.SortTime: "Time", model.SortSubject: "Subject",
		model.SortSender: "From", model.SortStatus: "Status", model.SortFolder: "Folder"}
	for _, k := range model.SortKeys {
		sp := p
		sp.Page, sp.Sort, sp.Asc = 0, k, false
		l := sortLink{Label: c.t(labels[k]), Aria: "none"}
		if k == p.Sort {
			sp.Asc = !p.Asc
			l.Arrow, l.Aria = "▼", "descending"
			if p.Asc {
				l.Arrow, l.Aria = "▲", "ascending"
			}
		}
		l.URL = sp.link(v.Base)
		v.Sorts = append(v.Sorts, l)
	}
	if p.Page > 0 {
		pp := p
		pp.Page--
		v.PrevURL = pp.link(v.Base)
	}
	if end < len(rows) {
		np := p
		np.Page++
		v.NextURL = np.link(v.Base)
	}
	ap := p
	ap.Auto = !p.Auto
	v.AutoURL = ap.link(v.Base)
	v.Return = p.values().Encode()
	s.render(c, http.StatusOK, "queue", title, v, "")
}
