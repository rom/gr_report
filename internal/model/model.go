// Package model holds the domain types of gr_report together with their
// validation rules. It has no I/O and no dependencies outside the standard
// library, which makes it cheap to unit test and fuzz.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rom/gr_report/internal/i18n"
)

// Size limits. They bound memory use and keep the journal small.
const (
	MaxSubjectLen     = 200
	MaxContentLen     = 20000
	MaxFieldValueLen  = 4000
	MaxFields         = 40
	MaxOptions        = 50
	MaxNameLen        = 80
	MaxDescriptionLen = 1000
	MaxFolderLen      = 60
	MaxRecipients     = 500
)

// Role is the authorisation role of an account.
type Role string

// The three roles of the system.
const (
	RoleAdmin  Role = "admin"
	RoleLeader Role = "leader"
	RoleGroup  Role = "group"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleLeader || r == RoleGroup }

// ReceiverLeader is the pseudo receiver ID that addresses the shared queue of
// the exercise leader(s). It is the default receiver of every report.
const ReceiverLeader = "leader"

// ReceiverLeaderName is the display name used for ReceiverLeader.
const ReceiverLeaderName = "Exercise leader"

// User is an account. An account with RoleGroup represents a participating
// group (or a single person) in the exercise.
type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Role        Role      `json:"role"`
	PassHash    string    `json:"pass_hash"`
	Disabled    bool      `json:"disabled,omitempty"`
	MustChange  bool      `json:"must_change,omitempty"`
	Created     time.Time `json:"created"`
}

// FieldType is the input type of a template field.
type FieldType string

// Supported field types.
const (
	FieldText     FieldType = "text"
	FieldTextarea FieldType = "textarea"
	FieldNumber   FieldType = "number"
	FieldSelect   FieldType = "select"
	FieldCheckbox FieldType = "checkbox"
	FieldDatetime FieldType = "datetime"
)

// FieldTypes lists all supported field types in display order.
var FieldTypes = []FieldType{FieldText, FieldTextarea, FieldNumber, FieldSelect, FieldCheckbox, FieldDatetime}

// Valid reports whether t is a supported field type.
func (t FieldType) Valid() bool {
	for _, k := range FieldTypes {
		if t == k {
			return true
		}
	}
	return false
}

// Field is one extra, structured input of a report template.
type Field struct {
	Key      string    `json:"key"`
	Label    string    `json:"label"`
	Type     FieldType `json:"type"`
	Required bool      `json:"required,omitempty"`
	Options  []string  `json:"options,omitempty"`
	Help     string    `json:"help,omitempty"`
}

// Template describes a report layout. Every report always carries the minimal
// set (timestamp, sender, subject, content, receiver); a template adds
// structured fields and guidance on top of that.
type Template struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	DefaultSubject string    `json:"default_subject,omitempty"`
	ContentHelp    string    `json:"content_help,omitempty"`
	Fields         []Field   `json:"fields,omitempty"`
	CreatedBy      string    `json:"created_by,omitempty"`
	Created        time.Time `json:"created"`
	Archived       bool      `json:"archived,omitempty"`
}

// Request is a question / information request sent by the exercise leader to
// some or all groups, with a report template attached for the answer.
type Request struct {
	ID         string    `json:"id"`
	Subject    string    `json:"subject"`
	Body       string    `json:"body"`
	TemplateID string    `json:"template_id"`
	Recipients []string  `json:"recipients,omitempty"` // empty: all groups
	CreatedBy  string    `json:"created_by"`
	Created    time.Time `json:"created"`
	Closed     bool      `json:"closed,omitempty"`
}

// Addressed reports whether the request is addressed to the group userID.
func (q *Request) Addressed(userID string) bool {
	if len(q.Recipients) == 0 {
		return true
	}
	for _, id := range q.Recipients {
		if id == userID {
			return true
		}
	}
	return false
}

// Status is the handling state of a report in the receiver's queue.
type Status string

// Report states.
const (
	StatusNew   Status = "new"
	StatusRead  Status = "read"
	StatusFiled Status = "filed"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool { return s == StatusNew || s == StatusRead || s == StatusFiled }

// FieldValue is a submitted template field. The label is copied from the
// template so a report stays readable even if the template changes later.
type FieldValue struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// Report is a submitted report. Everything except Status and Folder is
// immutable after submission.
type Report struct {
	ID           string       `json:"id"`
	Timestamp    time.Time    `json:"timestamp"`
	SenderID     string       `json:"sender_id"`
	SenderName   string       `json:"sender_name"`
	Subject      string       `json:"subject"`
	Content      string       `json:"content"`
	ReceiverID   string       `json:"receiver_id"`
	ReceiverName string       `json:"receiver_name"`
	TemplateID   string       `json:"template_id,omitempty"`
	TemplateName string       `json:"template_name,omitempty"`
	Fields       []FieldValue `json:"fields,omitempty"`
	RequestID    string       `json:"request_id,omitempty"`
	Status       Status       `json:"status"`
	Folder       string       `json:"folder,omitempty"`
}

// Clone returns a deep copy of t.
func (t Template) Clone() Template {
	if t.Fields != nil {
		fs := make([]Field, len(t.Fields))
		for i, f := range t.Fields {
			f.Options = append([]string(nil), f.Options...)
			fs[i] = f
		}
		t.Fields = fs
	}
	return t
}

// Clone returns a deep copy of q.
func (q Request) Clone() Request {
	q.Recipients = append([]string(nil), q.Recipients...)
	return q
}

// Clone returns a deep copy of r.
func (r Report) Clone() Report {
	r.Fields = append([]FieldValue(nil), r.Fields...)
	return r
}

// NewID returns a random 96-bit identifier in hex.
func NewID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("model: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

var (
	idRe       = regexp.MustCompile(`^[a-z0-9]{1,40}$`)
	usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,31}$`)
	keyRe      = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)
)

// ValidID reports whether s is syntactically acceptable as an entity ID.
func ValidID(s string) bool { return idRe.MatchString(s) }

// CleanLine normalises a single-line input: it must be valid UTF-8, control
// characters are replaced by spaces and surrounding white space is trimmed.
func CleanLine(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// CleanText normalises multi-line input: valid UTF-8, CRLF becomes LF, control
// characters other than newline and tab are dropped.
func CleanText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func checkLen(what any, s string, min, max int) error {
	n := utf8.RuneCountInString(s)
	if n < min {
		if min == 1 {
			return i18n.Errorf("%s is required", what)
		}
		return i18n.Errorf("%s must be at least %d characters", what, min)
	}
	if n > max {
		return i18n.Errorf("%s must be at most %d characters", what, max)
	}
	return nil
}

// Validate normalises and checks the user-editable parts of u.
func (u *User) Validate() error {
	u.Username = strings.ToLower(strings.TrimSpace(u.Username))
	u.DisplayName = CleanLine(u.DisplayName)
	if !usernameRe.MatchString(u.Username) {
		return i18n.Errorf("username must be 2-32 characters: a-z, 0-9, dot, dash, underscore")
	}
	if err := checkLen(i18n.Label("display name"), u.DisplayName, 1, MaxNameLen); err != nil {
		return err
	}
	if !u.Role.Valid() {
		return i18n.Errorf("unknown role")
	}
	return nil
}

// KeyFromLabel derives a stable field key from a human label.
func KeyFromLabel(label string) string {
	var b strings.Builder
	lastUnderscore := true
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
		if b.Len() >= 40 {
			break
		}
	}
	return strings.Trim(b.String(), "_")
}

// ParseOptions splits a comma separated option list.
func ParseOptions(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = CleanLine(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// Validate normalises and checks t. Missing field keys are derived from the
// labels and made unique.
func (t *Template) Validate() error {
	t.Name = CleanLine(t.Name)
	t.Description = CleanText(t.Description)
	t.DefaultSubject = CleanLine(t.DefaultSubject)
	t.ContentHelp = CleanLine(t.ContentHelp)
	if err := checkLen(i18n.Label("template name"), t.Name, 1, MaxNameLen); err != nil {
		return err
	}
	if err := checkLen(i18n.Label("description"), t.Description, 0, MaxDescriptionLen); err != nil {
		return err
	}
	if err := checkLen(i18n.Label("default subject"), t.DefaultSubject, 0, MaxSubjectLen); err != nil {
		return err
	}
	if err := checkLen(i18n.Label("content help"), t.ContentHelp, 0, MaxSubjectLen); err != nil {
		return err
	}
	if len(t.Fields) > MaxFields {
		return i18n.Errorf("a template can have at most %d fields", MaxFields)
	}
	seen := map[string]bool{}
	for i := range t.Fields {
		f := &t.Fields[i]
		f.Label = CleanLine(f.Label)
		f.Help = CleanLine(f.Help)
		if err := checkLen(i18n.Label("field label"), f.Label, 1, MaxNameLen); err != nil {
			return err
		}
		if err := checkLen(i18n.Label("field help"), f.Help, 0, MaxSubjectLen); err != nil {
			return err
		}
		if !f.Type.Valid() {
			return i18n.Errorf("field %q: unknown type", f.Label)
		}
		if f.Key == "" {
			f.Key = KeyFromLabel(f.Label)
		}
		if f.Key == "" {
			f.Key = "field"
		}
		if !keyRe.MatchString(f.Key) {
			return i18n.Errorf("field %q: invalid key", f.Label)
		}
		base := f.Key
		for n := 2; seen[f.Key]; n++ {
			suffix := fmt.Sprintf("_%d", n)
			if len(base)+len(suffix) > 40 {
				base = base[:40-len(suffix)]
			}
			f.Key = base + suffix
		}
		seen[f.Key] = true
		if f.Type == FieldSelect {
			clean := make([]string, 0, len(f.Options))
			for _, o := range f.Options {
				if o = CleanLine(o); o != "" {
					clean = append(clean, o)
				}
			}
			f.Options = clean
			if len(f.Options) == 0 {
				return i18n.Errorf("field %q: a select field needs options", f.Label)
			}
			if len(f.Options) > MaxOptions {
				return i18n.Errorf("field %q: at most %d options", f.Label, MaxOptions)
			}
			for _, o := range f.Options {
				if err := checkLen(i18n.Label("option"), o, 1, MaxNameLen); err != nil {
					return i18n.Errorf("field %q: %s", f.Label, err)
				}
			}
		} else {
			f.Options = nil
		}
	}
	return nil
}

// Validate normalises and checks q.
func (q *Request) Validate() error {
	q.Subject = CleanLine(q.Subject)
	q.Body = CleanText(q.Body)
	if err := checkLen(i18n.Label("subject"), q.Subject, 1, MaxSubjectLen); err != nil {
		return err
	}
	if err := checkLen(i18n.Label("message"), q.Body, 0, MaxContentLen); err != nil {
		return err
	}
	if !ValidID(q.TemplateID) {
		return i18n.Errorf("a report template must be attached")
	}
	if len(q.Recipients) > MaxRecipients {
		return i18n.Errorf("too many recipients")
	}
	for _, id := range q.Recipients {
		if !ValidID(id) {
			return i18n.Errorf("invalid recipient")
		}
	}
	return nil
}

// CleanFolder normalises a folder name used when filing reports.
func CleanFolder(s string) (string, error) {
	s = CleanLine(s)
	if err := checkLen(i18n.Label("folder"), s, 0, MaxFolderLen); err != nil {
		return "", err
	}
	return s, nil
}

// DatetimeLayout is the layout of HTML datetime-local inputs.
const DatetimeLayout = "2006-01-02T15:04"

// FillReport validates the user input for a new report against the template
// and stores the normalised result in r. get returns the submitted value of a
// template field by key.
func FillReport(r *Report, t *Template, subject, content string, get func(key string) string) error {
	r.Subject = CleanLine(subject)
	r.Content = CleanText(content)
	if err := checkLen(i18n.Label("subject"), r.Subject, 1, MaxSubjectLen); err != nil {
		return err
	}
	if err := checkLen(i18n.Label("content"), r.Content, 1, MaxContentLen); err != nil {
		return err
	}
	r.TemplateID, r.TemplateName = t.ID, t.Name
	r.Fields = nil
	for _, f := range t.Fields {
		var v string
		if f.Type == FieldTextarea {
			v = CleanText(get(f.Key))
		} else {
			v = CleanLine(get(f.Key))
		}
		if err := checkLen(f.Label, v, 0, MaxFieldValueLen); err != nil {
			return err
		}
		switch f.Type {
		case FieldCheckbox:
			if v != "" {
				v = "yes"
			} else {
				v = "no"
			}
			if f.Required && v != "yes" {
				return i18n.Errorf("%s must be checked", f.Label)
			}
		case FieldNumber:
			if v != "" && !isNumber(v) {
				return i18n.Errorf("%s must be a number", f.Label)
			}
		case FieldDatetime:
			if v != "" {
				if _, err := time.Parse(DatetimeLayout, v); err != nil {
					return i18n.Errorf("%s must be a date and time", f.Label)
				}
			}
		case FieldSelect:
			if v != "" && !contains(f.Options, v) {
				return i18n.Errorf("%s: not a valid choice", f.Label)
			}
		}
		if f.Required && v == "" {
			return i18n.Errorf("%s is required", f.Label)
		}
		r.Fields = append(r.Fields, FieldValue{Key: f.Key, Label: f.Label, Value: v})
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var numberRe = regexp.MustCompile(`^[+-]?[0-9]{1,18}([.,][0-9]{1,9})?$`)

func isNumber(s string) bool { return numberRe.MatchString(s) }
