package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/rom/gr_report/internal/auth"
	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/model"
	"github.com/rom/gr_report/internal/store"
)

// ---- creating many accounts at once ----

// maxImport bounds one import; each account costs a password derivation.
const maxImport = 200

type importRow struct {
	Line        int
	Username    string
	DisplayName string
	Role        model.Role
	Group       string // username of the group, for a personal account
	GroupName   string
	Password    string // one-time password, shown once
}

type importView struct {
	Text    string
	Created []importRow
	Errors  []string
	Max     int
}

func (s *Server) importForm(c *reqCtx) {
	s.render(c, http.StatusOK, "users_import", c.t("Create several accounts"), &importView{Max: maxImport}, "")
}

// splitImportLine splits a line on the first separator that occurs in it:
// semicolon, tab or comma.
func splitImportLine(line string) []string {
	sep := ","
	for _, cand := range []string{";", "\t"} {
		if strings.Contains(line, cand) {
			sep = cand
			break
		}
	}
	parts := strings.Split(line, sep)
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// parseImport reads lines of "username; display name; role; group" and
// checks them against each other and against the existing accounts. The
// role defaults to group; a non-empty group makes a personal account that
// belongs to that group, which may be created earlier in the same text.
func (s *Server) parseImport(c *reqCtx, text string) ([]importRow, map[string]model.User, []string) {
	var rows []importRow
	var errs []string
	fail := func(line int, msg string) {
		errs = append(errs, c.t("Line %d: %s", line, msg))
	}
	existing := map[string]model.User{}
	for _, u := range s.Store.Users() {
		existing[u.Username] = u
	}
	inBatch := map[string]importRow{}
	n := 0
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := splitImportLine(line)
		if i == 0 && strings.EqualFold(parts[0], "username") {
			continue // a header row
		}
		n++
		if n > maxImport {
			errs = append(errs, c.t("At most %d accounts can be created at once.", maxImport))
			break
		}
		if len(parts) < 2 || len(parts) > 4 {
			fail(i+1, c.t("expected: username; display name; role; group"))
			continue
		}
		for len(parts) < 4 {
			parts = append(parts, "")
		}
		row := importRow{Line: i + 1, Username: parts[0], DisplayName: parts[1], Role: model.Role(strings.ToLower(parts[2])), Group: strings.ToLower(parts[3])}
		if row.Role == "" {
			row.Role = model.RoleGroup
		}
		u := model.User{Username: row.Username, DisplayName: row.DisplayName, Role: row.Role}
		if err := u.Validate(); err != nil {
			fail(i+1, i18n.TErr(c.lang, err))
			continue
		}
		row.Username, row.DisplayName = u.Username, u.DisplayName
		if _, dup := existing[row.Username]; dup {
			fail(i+1, c.t("the username %q is already in use", row.Username))
			continue
		}
		if _, dup := inBatch[row.Username]; dup {
			fail(i+1, c.t("the username %q occurs twice", row.Username))
			continue
		}
		if row.Group != "" {
			g, inDB := existing[row.Group]
			b, inText := inBatch[row.Group]
			switch {
			case row.Role != model.RoleGroup:
				fail(i+1, c.t("only an account with the group role can belong to a group"))
				continue
			case inDB && g.Role == model.RoleGroup && g.GroupID == "":
				row.GroupName = g.DisplayName
			case inText && b.Role == model.RoleGroup && b.Group == "":
				row.GroupName = b.DisplayName
			default:
				fail(i+1, c.t("there is no group %q; create it on an earlier line", row.Group))
				continue
			}
		}
		inBatch[row.Username] = row
		rows = append(rows, row)
	}
	if len(rows) == 0 && len(errs) == 0 {
		errs = append(errs, c.t("Nothing to create: write one account per line."))
	}
	return rows, existing, errs
}

// importCreate creates all accounts of the text, or none if any line is
// wrong, and shows the generated one-time passwords once.
func (s *Server) importCreate(c *reqCtx) {
	text := c.r.PostForm.Get("accounts")
	rows, existing, errs := s.parseImport(c, text)
	if len(errs) > 0 {
		s.render(c, http.StatusUnprocessableEntity, "users_import", c.t("Create several accounts"),
			&importView{Text: text, Errors: errs, Max: maxImport}, c.t("No account was created. Correct the lines listed below."))
		return
	}
	ids := map[string]string{} // username -> ID, for groups in this text
	now := s.Now().UTC()
	for i := range rows {
		row := &rows[i]
		row.Password = auth.GeneratePassword()
		hash, err := s.Hasher.Hash(row.Password)
		if s.storeErr(c, err) {
			return
		}
		u := model.User{ID: model.NewID(), Username: row.Username, DisplayName: row.DisplayName, Role: row.Role,
			PassHash: hash, MustChange: true, Created: now}
		if row.Group != "" {
			if g, ok := existing[row.Group]; ok {
				u.GroupID = g.ID
			} else {
				u.GroupID = ids[row.Group]
			}
		}
		if s.storeErr(c, s.Store.PutUser(u)) {
			return
		}
		ids[u.Username] = u.ID
	}
	s.audit(c, "users_imported", "count", len(rows))
	// The passwords are in this response only; it is never cached.
	s.render(c, http.StatusOK, "users_import", c.t("Create several accounts"), &importView{Created: rows, Max: maxImport}, "")
}

// ---- audit log ----

const auditPageSize = 100

type auditView struct {
	Entries  []model.AuditEntry
	Q        string
	OlderURL string
}

func (s *Server) auditLog(c *reqCtx) {
	q := c.r.URL.Query()
	v := &auditView{Q: model.CleanLine(q.Get("q"))}
	if len(v.Q) > model.MaxSubjectLen {
		v.Q = ""
	}
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	if before < 0 {
		before = 0
	}
	entries := s.Store.Audit(v.Q, before, auditPageSize+1)
	if len(entries) > auditPageSize {
		entries = entries[:auditPageSize]
		next := url.Values{"before": {strconv.FormatInt(entries[auditPageSize-1].Seq, 10)}}
		if v.Q != "" {
			next.Set("q", v.Q)
		}
		v.OlderURL = "/admin/audit?" + next.Encode()
	}
	v.Entries = entries
	s.render(c, http.StatusOK, "audit", c.t("Audit log"), v, "")
}

// ---- backup ----

type copiesView struct {
	Copies []store.CopyInfo
	Counts store.ExerciseCounts
	Word   string // what has to be typed to confirm a reset
	Label  string
}

const stampLayout = "20060102-150405"

func (s *Server) backupPage(c *reqCtx) {
	copies, err := s.Store.Copies(store.BackupDir)
	if s.storeErr(c, err) {
		return
	}
	s.render(c, http.StatusOK, "backup", c.t("Backup"), &copiesView{Copies: copies}, "")
}

func (s *Server) backupCreate(c *reqCtx) {
	info, err := s.Store.CopyTo(store.BackupDir, "manual-"+s.Now().UTC().Format(stampLayout)+".db")
	if s.storeErr(c, err) {
		return
	}
	s.audit(c, "backup_created", "file", info.Name, "bytes", info.Size)
	s.redirect(c, "/admin/backup", c.t("Backup %s created.", info.Name))
}

// ---- starting a new exercise ----

// resetWord must be typed to confirm; it is the same in every language so
// the instructions and the check cannot drift apart.
const resetWord = "RESET"

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func (s *Server) exercisePage(c *reqCtx, status int, label, errMsg string) {
	copies, err := s.Store.Copies(store.ArchiveDir)
	if s.storeErr(c, err) {
		return
	}
	s.render(c, status, "exercise", c.t("Exercise"), &copiesView{Copies: copies, Counts: s.Store.Counts(), Word: resetWord, Label: label}, errMsg)
}

func (s *Server) exercise(c *reqCtx) { s.exercisePage(c, http.StatusOK, "", "") }

// exerciseReset archives the whole database on the server and then removes
// the reports, requests, chat and drafts. If the archive cannot be written
// nothing is removed.
func (s *Server) exerciseReset(c *reqCtx) {
	f := c.r.PostForm
	label := model.CleanLine(f.Get("label"))
	if f.Get("confirm") != resetWord {
		s.exercisePage(c, http.StatusUnprocessableEntity, label, c.t("Type %s in the confirmation field to start a new exercise. Nothing was changed.", resetWord))
		return
	}
	slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(label), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	name := s.Now().UTC().Format(stampLayout)
	if slug != "" {
		name += "-" + slug
	}
	counts := s.Store.Counts()
	info, err := s.Store.CopyTo(store.ArchiveDir, name+".db")
	if s.storeErr(c, err) {
		return
	}
	if s.storeErr(c, s.Store.ResetExercise()) {
		return
	}
	s.audit(c, "exercise_reset", "archive", info.Name, "reports", counts.Reports, "requests", counts.Requests, "chat_messages", counts.ChatMessages)
	s.redirect(c, "/exercise", c.t("A new exercise has started. The previous one is archived on the server as %s.", info.Name))
}
