package web

import (
	"encoding/csv"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/model"
	"github.com/rom/gr_report/internal/store"
)

const pageSize = 100

// ---- group home ----

type requestRow struct {
	Req          model.Request
	TemplateName string
	Responses    int  // leader: all responses; group: the group's responses
	Overdue      bool // the deadline has passed (for a group: without an answer)
}

type draftRow struct {
	Draft        model.Draft
	TemplateName string
	RequestName  string
}

type homeView struct {
	Requests  []requestRow
	Templates []model.Template
	Recent    []model.Report
	Drafts    []draftRow
}

func (s *Server) home(c *reqCtx) {
	v := &homeView{Templates: s.Store.Templates(false)}
	for _, row := range s.requestRows(c) {
		if !row.Req.Closed {
			v.Requests = append(v.Requests, row)
		}
	}
	v.Recent = s.Store.Reports(&model.ReportFilter{SenderID: c.party})
	model.SortReports(v.Recent, model.SortTime, true)
	if len(v.Recent) > 10 {
		v.Recent = v.Recent[:10]
	}
	for _, d := range s.Store.Drafts(c.user.ID) {
		row := draftRow{Draft: d}
		if t, ok := s.Store.Template(d.TemplateID); ok {
			row.TemplateName = t.Name
		}
		if q, ok := s.Store.Request(d.RequestID); ok && d.RequestID != "" {
			row.RequestName = q.Subject
		}
		v.Drafts = append(v.Drafts, row)
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
	// DraftSaved is when the draft this form was filled from was saved.
	DraftSaved time.Time
}

// reportContext resolves and authorises the template and optional request a
// new report refers to.
func (s *Server) reportContext(c *reqCtx, templateID, requestID string) (*reportFormView, bool) {
	v := &reportFormView{Receiver: model.ReceiverLeader, Values: map[string]string{}}
	if requestID != "" {
		q, ok := s.Store.Request(requestID)
		if !ok || !q.Addressed(c.party) {
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
			if g.ID != c.party {
				v.Receivers = append(v.Receivers, g)
			}
		}
	}
	return v, true
}

func (v *reportFormView) requestID() string {
	if v.Request != nil {
		return v.Request.ID
	}
	return ""
}

func (s *Server) reportForm(c *reqCtx) {
	q := c.r.URL.Query()
	v, ok := s.reportContext(c, q.Get("template"), q.Get("request"))
	if !ok {
		return
	}
	// Continue where the user left off.
	if d, ok := s.Store.Draft(c.user.ID, v.Template.ID, v.requestID()); ok {
		v.Subject, v.Content, v.DraftSaved = d.Subject, d.Content, d.Updated
		if d.Receiver != "" && v.Request == nil {
			v.Receiver = d.Receiver
		}
		for k, val := range d.Values {
			v.Values[k] = val
		}
	}
	s.render(c, http.StatusOK, "report_new", c.t("New report"), v, "")
}

// saveDraft stores what is in the form without validating it as a report:
// a draft may be incomplete. Only the size limits apply.
func (s *Server) saveDraft(c *reqCtx, v *reportFormView) error {
	d := model.Draft{ID: model.NewID(), UserID: c.user.ID, TemplateID: v.Template.ID, RequestID: v.requestID(),
		Receiver: v.Receiver, Subject: model.CleanLine(v.Subject), Content: model.CleanText(v.Content),
		Values: map[string]string{}, Updated: s.Now().UTC()}
	if len([]rune(d.Subject)) > model.MaxSubjectLen || len([]rune(d.Content)) > model.MaxContentLen {
		return i18n.Errorf("the draft is too long to save")
	}
	for _, fd := range v.Template.Fields {
		val := model.CleanText(v.Values[fd.Key])
		if len([]rune(val)) > model.MaxFieldValueLen {
			return i18n.Errorf("the draft is too long to save")
		}
		if val != "" {
			d.Values[fd.Key] = val
		}
	}
	_, err := s.Store.PutDraft(d)
	if err == store.ErrTooMany {
		return i18n.Errorf("you have too many drafts; send or delete one first")
	}
	return err
}

func (s *Server) reportCreate(c *reqCtx) {
	f := c.r.PostForm
	v, ok := s.reportContext(c, f.Get("template"), f.Get("request"))
	if !ok {
		return
	}
	r := model.Report{
		ID: model.NewID(), Timestamp: s.Now().UTC(), SenderID: c.party, SenderName: c.group.DisplayName,
		ReceiverID: model.ReceiverLeader, ReceiverName: model.ReceiverLeaderName, Status: model.StatusNew,
	}
	if c.user.GroupID != "" {
		r.AuthorID, r.AuthorName = c.user.ID, c.user.DisplayName
	}
	v.Subject, v.Content = f.Get("subject"), f.Get("content")
	for _, fd := range v.Template.Fields {
		v.Values[fd.Key] = f.Get("f_" + fd.Key)
	}
	if rcv := f.Get("receiver"); rcv != "" && v.Request == nil {
		v.Receiver = rcv
	}
	bad := func(msg string) {
		s.render(c, http.StatusUnprocessableEntity, "report_new", c.t("New report"), v, msg)
	}

	if f.Get("action") == "draft" {
		auto := c.r.Header.Get("X-Autosave") != ""
		if err := s.saveDraft(c, v); err != nil {
			if _, ok := err.(*i18n.Msg); !ok {
				s.Log.Error("store", "err", err, "path", c.r.URL.Path)
				err = i18n.Errorf("The change could not be saved.")
			}
			if auto {
				http.Error(c.w, i18n.TErr(c.lang, err), http.StatusUnprocessableEntity)
				return
			}
			bad(i18n.TErr(c.lang, err))
			return
		}
		if auto { // the page script saves in the background
			c.w.WriteHeader(http.StatusNoContent)
			return
		}
		back := "/reports/new?template=" + url.QueryEscape(v.Template.ID)
		if v.Request != nil {
			back = "/reports/new?request=" + url.QueryEscape(v.Request.ID)
		}
		s.redirect(c, back, c.t("Draft saved. It has not been sent."))
		return
	}

	if v.Request != nil {
		r.RequestID = v.Request.ID
	} else if v.Receiver != model.ReceiverLeader {
		g, ok := s.Store.User(v.Receiver)
		if !ok || g.Role != model.RoleGroup || g.GroupID != "" || g.Disabled || g.ID == c.party {
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
	// The sender has, of course, seen the report; and the draft is done.
	_ = s.Store.MarkSeen(c.user.ID, r.ID, true)
	if d, ok := s.Store.Draft(c.user.ID, v.Template.ID, v.requestID()); ok {
		_ = s.Store.DeleteDraft(c.user.ID, d.ID)
	}
	s.audit(c, "report_sent", "report", r.ID, "receiver", r.ReceiverID)
	to := r.ReceiverName
	if r.ReceiverID == model.ReceiverLeader {
		to = c.t(model.ReceiverLeaderName)
	}
	s.redirect(c, "/reports/"+r.ID, c.t("Report sent to %s.", to))
}

func (s *Server) draftDelete(c *reqCtx) {
	if s.storeErr(c, s.Store.DeleteDraft(c.user.ID, c.r.PathValue("id"))) {
		return
	}
	s.redirect(c, "/home", c.t("Draft deleted."))
}

// ---- viewing and filing ----

type reportView struct {
	Report  model.Report
	Request *model.Request
	Mine    bool // the viewer handles the queue this report is in
	Folders []string
	Late    bool         // sent after the deadline of the request it answers
	Leaders []model.User // for assigning (leader queue only)
}

// leaders returns the enabled exercise leader accounts.
func (s *Server) leaders() []model.User {
	var out []model.User
	for _, u := range s.Store.Users() {
		if u.Role == model.RoleLeader && !u.Disabled {
			out = append(out, u)
		}
	}
	return out
}

func (s *Server) reportView(c *reqCtx) {
	r, ok := s.Store.Report(c.r.PathValue("id"))
	mine := ok && r.ReceiverID == c.party
	// Leaders may read everything; groups only what they sent or received.
	visible := mine || r.SenderID == c.party || c.user.Role == model.RoleLeader
	if !ok || !visible {
		s.fail(c, http.StatusNotFound, c.t("No such report."))
		return
	}
	if mine {
		// Read state is per person; the shared status only records that
		// somebody has opened the report.
		if s.storeErr(c, s.Store.MarkSeen(c.user.ID, r.ID, true)) {
			return
		}
		if r.Status == model.StatusNew {
			upd, err := s.Store.UpdateReport(r.ID, func(r *model.Report) bool {
				r.Status = model.StatusRead
				return true
			})
			if s.storeErr(c, err) {
				return
			}
			r = upd
		}
	}
	v := &reportView{Report: r, Mine: mine}
	if mine {
		v.Folders = s.Store.Stats(r.ReceiverID, c.user.ID).Folders
		if c.user.Role == model.RoleLeader {
			v.Leaders = s.leaders()
		}
	}
	if q, ok := s.Store.Request(r.RequestID); ok && r.RequestID != "" {
		v.Request = &q
		v.Late = !q.Due.IsZero() && r.Timestamp.After(q.Due)
	}
	s.render(c, http.StatusOK, "report", r.Subject, v, "")
}

// applyStatus performs a queue action on the given reports and returns how
// many were changed. Reports outside the user's own queue are skipped.
func (s *Server) applyStatus(c *reqCtx, ids []string, action, folder string) (int, error) {
	leader := c.user.Role == model.RoleLeader
	switch action {
	case "new", "read", "file":
	case "assign_me", "unassign":
		if !leader {
			return 0, i18n.Errorf("choose an action")
		}
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
	n := 0
	for _, id := range ids {
		changed := false
		_, err := s.Store.UpdateReport(id, func(r *model.Report) bool {
			if r.ReceiverID != c.party {
				return false
			}
			changed = true
			switch action {
			case "new":
				r.Status = model.StatusNew
			case "read":
				r.Status = model.StatusRead
			case "file":
				r.Status, r.Folder = model.StatusFiled, folder
			case "assign_me":
				r.AssigneeID, r.AssigneeName = c.user.ID, c.user.DisplayName
			case "unassign":
				r.AssigneeID, r.AssigneeName = "", ""
			}
			return true
		})
		if !changed || err != nil {
			continue
		}
		n++
		switch action {
		case "new":
			_ = s.Store.MarkSeen(c.user.ID, id, false)
		case "read":
			_ = s.Store.MarkSeen(c.user.ID, id, true)
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

// reportAssign gives a report in the leader queue to a leader, or to
// nobody.
func (s *Server) reportAssign(c *reqCtx) {
	id, to := c.r.PathValue("id"), c.r.PostForm.Get("assignee")
	var assignee model.User
	if to != "" {
		var ok bool
		if assignee, ok = s.Store.User(to); !ok || assignee.Role != model.RoleLeader || assignee.Disabled {
			s.fail(c, http.StatusUnprocessableEntity, c.t("Unknown exercise leader."))
			return
		}
	}
	found := false
	_, err := s.Store.UpdateReport(id, func(r *model.Report) bool {
		if r.ReceiverID != model.ReceiverLeader {
			return false
		}
		found = true
		r.AssigneeID, r.AssigneeName = assignee.ID, assignee.DisplayName
		return true
	})
	if !found {
		s.fail(c, http.StatusNotFound, c.t("No such report."))
		return
	}
	if s.storeErr(c, err) {
		return
	}
	s.audit(c, "report_assigned", "report", id, "assignee", assignee.Username)
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

// Values of the assignee parameter besides a leader's ID.
const (
	assigneeMe   = "me"
	assigneeNone = "none"
)

type queueParams struct {
	Group, Status, Template, Folder, Request, Q, From, To string
	Assignee                                              string // "", "me", "none" or a leader ID
	Sort                                                  string
	Asc                                                   bool
	Unread                                                bool // only what the viewer has not opened
	Page                                                  int
	Auto                                                  bool
}

func parseQueueParams(v url.Values) queueParams {
	p := queueParams{
		Group: v.Get("group"), Status: v.Get("status"), Template: v.Get("template"),
		Folder: model.CleanLine(v.Get("folder")), Request: v.Get("request"), Q: model.CleanLine(v.Get("q")),
		From: v.Get("from"), To: v.Get("to"), Sort: v.Get("sort"), Assignee: v.Get("assignee"),
		Asc: v.Get("dir") == "asc", Auto: v.Get("auto") == "1", Unread: v.Get("unread") == "1",
	}
	for _, id := range []*string{&p.Group, &p.Template, &p.Request} {
		if !model.ValidID(*id) {
			*id = ""
		}
	}
	if p.Assignee != assigneeMe && p.Assignee != assigneeNone && !model.ValidID(p.Assignee) {
		p.Assignee = ""
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
	set("assignee", p.Assignee)
	set("q", p.Q)
	set("from", p.From)
	set("to", p.To)
	if p.Sort != model.SortTime {
		v.Set("sort", p.Sort)
	}
	if p.Asc {
		v.Set("dir", "asc")
	}
	if p.Unread {
		v.Set("unread", "1")
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

// filtered reports whether anything narrows the view beyond the queue.
func (p queueParams) filtered() bool {
	return p.Status != "" || p.Template != "" || p.Request != "" || p.Folder != "" || p.Q != "" ||
		p.From != "" || p.To != "" || p.Assignee != "" || p.Unread
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

type savedLink struct {
	ID, Name, URL string
}

type queueView struct {
	Base      string
	Sent      bool
	Leader    bool // the leader queue: assignment is available
	P         queueParams
	Queues    []queueLink
	Rows      []model.Report
	Total     int
	First     int
	Last      int
	Templates []model.Template
	Requests  []model.Request
	Folders   []string
	Leaders   []model.User
	Saved     []savedLink
	Sorts     []sortLink
	PrevURL   string
	NextURL   string
	AutoURL   string
	ExportURL string
	Return    string
	Filtered  bool
}

func (s *Server) queue(c *reqCtx) { s.list(c, false) }
func (s *Server) sent(c *reqCtx)  { s.list(c, true) }

// dayIn interprets a YYYY-MM-DD date as midnight in a time zone.
func dayIn(s string, loc *time.Location) time.Time {
	d := model.ParseDay(s)
	if d.IsZero() {
		return d
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}

// queueRows returns the reports of the queue (or of the sent list) that
// match the request's parameters, sorted. It is shared by the page and the
// export so both always show the same selection.
func (s *Server) queueRows(c *reqCtx, sent bool) (queueParams, []model.Report) {
	p := parseQueueParams(c.r.URL.Query())
	f := model.ReportFilter{
		Status: model.Status(p.Status), TemplateID: p.Template, RequestID: p.Request,
		Folder: p.Folder, Query: p.Q, From: dayIn(p.From, c.loc), ViewerID: c.user.ID, UnseenOnly: p.Unread,
	}
	if to := dayIn(p.To, c.loc); !to.IsZero() {
		f.To = to.AddDate(0, 0, 1)
	}
	if sent {
		f.SenderID = c.party
		p.Group, p.Assignee, p.Unread, f.UnseenOnly = "", "", false, false
	} else {
		f.ReceiverID, f.SenderID = c.party, p.Group
		switch {
		case c.user.Role != model.RoleLeader:
			p.Assignee = ""
		case p.Assignee == assigneeMe:
			f.AssigneeID = c.user.ID
		case p.Assignee == assigneeNone:
			f.AssigneeID = model.AssigneeNone
		default:
			f.AssigneeID = p.Assignee
		}
	}
	rows := s.Store.Reports(&f)
	model.SortReports(rows, p.Sort, !p.Asc)
	return p, rows
}

func (s *Server) list(c *reqCtx, sent bool) {
	p, rows := s.queueRows(c, sent)
	v := &queueView{Base: "/queue", Sent: sent, P: p, Templates: s.Store.Templates(true)}
	title := c.t("Inbox")
	if sent {
		v.Base, title = "/sent", c.t("Sent reports")
	} else {
		stats := s.Store.Stats(c.party, c.user.ID)
		v.Folders = stats.Folders
		all := queueLink{Name: c.t("All queues"), Active: p.Group == ""}
		one := p
		one.Page = 0
		for _, g := range s.Store.Users() {
			// Leaders get one queue per group; a group sees one queue
			// per group that has written to it. Members are not groups.
			if g.Role != model.RoleGroup || g.GroupID != "" || g.ID == c.party {
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
			v.Leader = true
			v.Requests = s.Store.Requests()
			v.Leaders = s.leaders()
		}
		for _, sf := range s.Store.SavedFilters(c.user.ID) {
			v.Saved = append(v.Saved, savedLink{ID: sf.ID, Name: sf.Name, URL: "/queue?" + sf.Query})
		}
	}
	v.Total = len(rows)
	start := p.Page * pageSize
	if start > len(rows) {
		start = len(rows)
	}
	end := min(start+pageSize, len(rows))
	v.Rows = rows[start:end]
	v.First, v.Last = start+1, end
	v.Filtered = p.filtered()

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
	ep := p
	ep.Page, ep.Auto = 0, false
	v.ExportURL = ep.link(v.Base + "/export.csv")
	v.Return = p.values().Encode()
	s.render(c, http.StatusOK, "queue", title, v, "")
}

// ---- export ----

// csvCell guards against formula injection: spreadsheet programs run a
// cell that starts with =, +, - or @ as a formula.
func csvCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (s *Server) queueExport(c *reqCtx) { s.export(c, false) }
func (s *Server) sentExport(c *reqCtx)  { s.export(c, true) }

// export writes the current selection of the queue or the sent list as
// CSV: one row per report, the template fields as extra columns.
func (s *Server) export(c *reqCtx, sent bool) {
	_, rows := s.queueRows(c, sent)
	// One column per field label, in order of first appearance.
	var labels []string
	seen := map[string]bool{}
	for _, r := range rows {
		for _, f := range r.Fields {
			if !seen[f.Label] {
				seen[f.Label] = true
				labels = append(labels, f.Label)
			}
		}
	}
	name := "queue"
	if sent {
		name = "sent"
	}
	h := c.w.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="gr_report-`+name+"-"+s.Now().UTC().Format("20060102-150405")+`.csv"`)
	// A byte order mark makes spreadsheet programs read the file as UTF-8.
	_, _ = c.w.Write([]byte("\xEF\xBB\xBF"))
	w := csv.NewWriter(c.w)
	header := []string{c.t("Time") + " (" + c.loc.String() + ")", c.t("Time") + " (UTC)", c.t("From"), c.t("Written by"), c.t("To"),
		c.t("Subject"), c.t("Status"), c.t("Folder"), c.t("Assigned to"), c.t("Template"), c.t("Request"), c.t("Content")}
	for _, l := range labels {
		header = append(header, csvCell(l))
	}
	_ = w.Write(header)
	requests := map[string]string{}
	for _, r := range rows {
		if _, ok := requests[r.RequestID]; !ok && r.RequestID != "" {
			q, _ := s.Store.Request(r.RequestID)
			requests[r.RequestID] = q.Subject
		}
		to := r.ReceiverName
		if r.ReceiverID == model.ReceiverLeader {
			to = c.t(model.ReceiverLeaderName)
		}
		rec := []string{c.ts(r.Timestamp), r.Timestamp.UTC().Format(time.RFC3339), r.SenderName, r.AuthorName, to,
			r.Subject, c.t(string(r.Status)), r.Folder, r.AssigneeName, r.TemplateName, requests[r.RequestID], r.Content}
		values := map[string]string{}
		for _, f := range r.Fields {
			values[f.Label] = f.Value
		}
		for _, l := range labels {
			rec = append(rec, values[l])
		}
		for i := range rec {
			rec[i] = csvCell(rec[i])
		}
		_ = w.Write(rec)
	}
	w.Flush()
	s.audit(c, "export", "list", name, "reports", len(rows))
}

// ---- saved filters ----

func (s *Server) filterSave(c *reqCtx) {
	f := c.r.PostForm
	name := model.CleanLine(f.Get("name"))
	q, _ := url.ParseQuery(f.Get("return"))
	p := parseQueueParams(q)
	p.Page, p.Auto = 0, false
	back := p.link("/queue")
	if name == "" || len([]rune(name)) > model.MaxFolderLen {
		s.redirect(c, back, c.t("Give the filter a name of at most %d characters.", model.MaxFolderLen))
		return
	}
	err := s.Store.PutSavedFilter(model.SavedFilter{ID: model.NewID(), UserID: c.user.ID, Name: name, Query: p.values().Encode()})
	if err == store.ErrTooMany {
		s.redirect(c, back, c.t("You have too many saved filters; delete one first."))
		return
	}
	if s.storeErr(c, err) {
		return
	}
	s.redirect(c, back, c.t("Filter saved."))
}

func (s *Server) filterDelete(c *reqCtx) {
	if s.storeErr(c, s.Store.DeleteSavedFilter(c.user.ID, c.r.PathValue("id"))) {
		return
	}
	s.redirect(c, "/queue", c.t("Filter deleted."))
}
