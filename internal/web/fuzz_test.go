package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rom/gr_report/internal/model"
)

// FuzzQueueParams checks that parsing arbitrary query strings yields a
// normalised, stable set of parameters.
func FuzzQueueParams(f *testing.F) {
	f.Add("group=abc&status=new&sort=subject&dir=asc&page=2&q=fuel+grid&from=2026-05-01&auto=1")
	f.Add("status=%ff&page=-1&sort=;drop&folder=%00&q=" + strings.Repeat("a", 500))
	f.Add("")
	f.Fuzz(func(t *testing.T, raw string) {
		v, err := url.ParseQuery(raw)
		if err != nil {
			return
		}
		p := parseQueueParams(v)
		if p.Page < 0 || len(p.Q) > model.MaxSubjectLen || (p.Status != "" && !model.Status(p.Status).Valid()) {
			t.Fatalf("not normalised: %+v", p)
		}
		for _, id := range []string{p.Group, p.Template, p.Request} {
			if id != "" && !model.ValidID(id) {
				t.Fatalf("invalid id kept: %q", id)
			}
		}
		again := parseQueueParams(p.values())
		if again != p {
			t.Fatalf("round trip changed parameters:\n%+v\n%+v", p, again)
		}
		if link := p.link("/queue"); !strings.HasPrefix(link, "/queue") || strings.ContainsAny(link, "<>\"' \n") {
			t.Fatalf("unsafe link %q", link)
		}
	})
}

// FuzzHandlers throws arbitrary paths, queries and form bodies at the
// router as a logged-in leader and group. No input may cause a 5xx or leak
// unescaped markup.
func FuzzHandlers(f *testing.F) {
	f.Add("/queue", "q=a&sort=subject", "", false)
	f.Add("/reports", "", "template=x&subject=s&content=c", true)
	f.Add("/reports/new", "template=%00&request=../", "", true)
	f.Add("/templates", "", "name=x&rows=99&f0_label=a&f0_type=select&f0_opts=<script>", false)
	f.Add("/queue/bulk", "", "id=a&id=b&action=file&folder=<script>&return=%zz", false)
	f.Add("/requests", "", "subject=<script>&template=x&to=some&recipient=<script>", false)
	f.Add("/admin/users", "", "username=<script>", false)
	e := newEnv(f)
	h := e.srv.Handler()
	cookies := map[bool]*http.Cookie{}
	tokens := map[bool]string{}
	for group, id := range map[bool]string{true: e.id["alpha"], false: e.id["leader"]} {
		tok := e.srv.Sessions.Create(id)
		se, _ := e.srv.Sessions.Get(tok)
		cookies[group] = &http.Cookie{Name: e.srv.cookieName(), Value: tok}
		tokens[group] = se.CSRF
	}
	f.Fuzz(func(t *testing.T, path, query, body string, group bool) {
		if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, " ?#%\x00\r\n") || strings.Contains(path, "logout") || strings.Contains(path, "password") {
			return
		}
		method := http.MethodGet
		if body != "" {
			method = http.MethodPost
			body += "&csrf=" + tokens[group]
		}
		u := url.URL{Path: path, RawQuery: query}
		req, err := http.NewRequest(method, u.String(), strings.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookies[group])
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code >= 500 {
			t.Fatalf("%s %s -> %d\n%s", method, u.String(), rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "<script") {
			t.Fatalf("%s %s: unescaped markup in response", method, u.String())
		}
	})
}
