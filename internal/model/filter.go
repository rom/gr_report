package model

import (
	"sort"
	"strings"
	"time"
)

// Sort keys understood by SortReports.
const (
	SortTime    = "time"
	SortSubject = "subject"
	SortSender  = "sender"
	SortStatus  = "status"
	SortFolder  = "folder"
)

// SortKeys lists the valid sort keys.
var SortKeys = []string{SortTime, SortSubject, SortSender, SortStatus, SortFolder}

// ReportFilter selects reports. Zero-valued members do not restrict.
type ReportFilter struct {
	ReceiverID string
	SenderID   string
	Status     Status
	TemplateID string
	RequestID  string
	Folder     string
	Query      string // white-space separated terms, all must match
	From, To   time.Time

	terms []string
}

// MaxQueryTerms bounds the work a single search can cause.
const MaxQueryTerms = 10

// Prepare parses Query. It must be called before Match.
func (f *ReportFilter) Prepare() {
	// CleanLine removes control characters: a NUL byte would end the
	// pattern early inside SQLite and turn the term into "match all".
	f.terms = strings.Fields(strings.ToLower(CleanLine(f.Query)))
	if len(f.terms) > MaxQueryTerms {
		f.terms = f.terms[:MaxQueryTerms]
	}
}

// Match reports whether r passes the filter.
func (f *ReportFilter) Match(r *Report) bool {
	switch {
	case f.ReceiverID != "" && r.ReceiverID != f.ReceiverID,
		f.SenderID != "" && r.SenderID != f.SenderID,
		f.Status != "" && r.Status != f.Status,
		f.TemplateID != "" && r.TemplateID != f.TemplateID,
		f.RequestID != "" && r.RequestID != f.RequestID,
		f.Folder != "" && r.Folder != f.Folder,
		!f.From.IsZero() && r.Timestamp.Before(f.From),
		!f.To.IsZero() && !r.Timestamp.Before(f.To):
		return false
	}
	if len(f.terms) == 0 {
		return true
	}
	hay := make([]string, 0, 5+len(r.Fields))
	hay = append(hay, strings.ToLower(r.Subject), strings.ToLower(r.Content),
		strings.ToLower(r.SenderName), strings.ToLower(r.TemplateName), strings.ToLower(r.Folder))
	for _, fv := range r.Fields {
		hay = append(hay, strings.ToLower(fv.Value))
	}
	for _, t := range f.terms {
		found := false
		for _, h := range hay {
			if strings.Contains(h, t) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

var statusOrder = map[Status]int{StatusNew: 0, StatusRead: 1, StatusFiled: 2}

// SortReports sorts rs in place. Unknown keys sort by time. Ties are broken
// by timestamp and ID, so the order is always deterministic.
func SortReports(rs []Report, key string, desc bool) {
	cmp := func(a, b *Report) int {
		switch key {
		case SortSubject:
			return strings.Compare(strings.ToLower(a.Subject), strings.ToLower(b.Subject))
		case SortSender:
			return strings.Compare(strings.ToLower(a.SenderName), strings.ToLower(b.SenderName))
		case SortStatus:
			return statusOrder[a.Status] - statusOrder[b.Status]
		case SortFolder:
			return strings.Compare(strings.ToLower(a.Folder), strings.ToLower(b.Folder))
		}
		return 0
	}
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := &rs[i], &rs[j]
		c := cmp(a, b)
		if c == 0 {
			c = a.Timestamp.Compare(b.Timestamp)
		}
		if c == 0 {
			c = strings.Compare(a.ID, b.ID)
		}
		if desc {
			return c > 0
		}
		return c < 0
	})
}

// ParseDay parses a YYYY-MM-DD date (UTC). An empty or invalid string gives
// the zero time.
func ParseDay(s string) time.Time {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

// Terms returns the normalised, lower-cased search terms of Query.
func (f *ReportFilter) Terms() []string {
	f.Prepare()
	return f.terms
}
