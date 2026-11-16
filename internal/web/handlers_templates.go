package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rom/gr_report/internal/model"
)

const blankFieldRows = 3

func (s *Server) templates(c *reqCtx) {
	s.render(c, http.StatusOK, "templates", "Report templates", s.Store.Templates(true), "")
}

type fieldRow struct {
	I       int
	F       model.Field
	Options string
}

type templateFormView struct {
	T     model.Template
	New   bool
	Rows  []fieldRow
	Types []model.FieldType
}

func (s *Server) templateFormPage(c *reqCtx, status int, t model.Template, errMsg string) {
	v := &templateFormView{T: t, New: t.ID == "", Types: model.FieldTypes}
	for i, f := range t.Fields {
		v.Rows = append(v.Rows, fieldRow{I: i, F: f, Options: strings.Join(f.Options, ", ")})
	}
	for i := 0; i < blankFieldRows && len(v.Rows) < model.MaxFields; i++ {
		v.Rows = append(v.Rows, fieldRow{I: len(v.Rows), F: model.Field{Type: model.FieldText}})
	}
	title := "Edit template"
	if v.New {
		title = "New template"
	}
	s.render(c, status, "template_edit", title, v, errMsg)
}

func (s *Server) templateForm(c *reqCtx) {
	var t model.Template
	if id := c.r.PathValue("id"); id != "" {
		var ok bool
		if t, ok = s.Store.Template(id); !ok {
			s.fail(c, http.StatusNotFound, "No such template.")
			return
		}
	}
	s.templateFormPage(c, http.StatusOK, t, "")
}

func (s *Server) templateSave(c *reqCtx) {
	f := c.r.PostForm
	t := model.Template{CreatedBy: c.user.ID, Created: s.Now().UTC()}
	if id := c.r.PathValue("id"); id != "" {
		var ok bool
		if t, ok = s.Store.Template(id); !ok {
			s.fail(c, http.StatusNotFound, "No such template.")
			return
		}
	}
	t.Name, t.Description = f.Get("name"), f.Get("description")
	t.DefaultSubject, t.ContentHelp = f.Get("default_subject"), f.Get("content_help")
	t.Fields = nil
	rows, _ := strconv.Atoi(f.Get("rows"))
	for i := 0; i < rows && i < model.MaxFields+blankFieldRows; i++ {
		n := "f" + strconv.Itoa(i) + "_"
		label := model.CleanLine(f.Get(n + "label"))
		if label == "" {
			continue // an emptied row removes the field
		}
		t.Fields = append(t.Fields, model.Field{
			Key: f.Get(n + "key"), Label: label, Type: model.FieldType(f.Get(n + "type")),
			Required: f.Get(n+"req") != "", Options: model.ParseOptions(f.Get(n + "opts")), Help: f.Get(n + "help"),
		})
	}
	if err := t.Validate(); err != nil {
		s.templateFormPage(c, http.StatusUnprocessableEntity, t, err.Error())
		return
	}
	if t.ID == "" {
		t.ID = model.NewID()
	}
	if s.storeErr(c, s.Store.PutTemplate(t)) {
		return
	}
	s.audit(c, "template_saved", "template", t.ID, "name", t.Name)
	if f.Get("more") != "" {
		s.redirect(c, "/templates/"+t.ID, "Template saved. You can add more fields.")
		return
	}
	s.redirect(c, "/templates", "Template saved.")
}

func (s *Server) templateArchive(c *reqCtx) {
	t, ok := s.Store.Template(c.r.PathValue("id"))
	if !ok {
		s.fail(c, http.StatusNotFound, "No such template.")
		return
	}
	t.Archived = c.r.PostForm.Get("archived") == "1"
	if s.storeErr(c, s.Store.PutTemplate(t)) {
		return
	}
	s.audit(c, "template_archived", "template", t.ID, "archived", t.Archived)
	msg := "Template restored."
	if t.Archived {
		msg = "Template archived. It can no longer be selected for new reports."
	}
	s.redirect(c, "/templates", msg)
}
