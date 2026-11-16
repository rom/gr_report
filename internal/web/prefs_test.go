package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/rom/gr_report/internal/i18n"
)

func TestLanguageSelection(t *testing.T) {
	e := newEnv(t)

	// Browser preference.
	c := e.anon()
	c.get("/login").want(t, 200, `<html lang="en"`, "Log in", "Username")
	c.hdr = map[string]string{"Accept-Language": "sv-SE,sv;q=0.9,en;q=0.8"}
	r := c.get("/login").want(t, 200, `<html lang="sv"`, "Logga in", "Användarnamn", "Språk och utseende").wantNot(t, "Username")
	if r.header.Get("Content-Language") != "sv" {
		t.Errorf("Content-Language = %q", r.header.Get("Content-Language"))
	}
	c.postRaw("/login", url.Values{"username": {"alpha"}, "password": {"wrong"}}, nil).want(t, 401, "Fel användarnamn eller lösenord.")

	// Server default when the browser has no supported preference.
	c.hdr = map[string]string{"Accept-Language": "de"}
	c.get("/login").want(t, 200, `<html lang="en"`)
	e.srv.DefaultLang = i18n.SV
	c.get("/login").want(t, 200, `<html lang="sv"`)
	e.srv.DefaultLang = i18n.EN

	// An explicit choice wins over the browser and survives login.
	c.hdr = map[string]string{"Accept-Language": "en"}
	c.get("/settings").want(t, 200, "Settings", "Language", `<option value="sv" lang="sv">Svenska</option>`, `<option value="en" lang="en" selected>English</option>`)
	r = c.postRaw("/settings", url.Values{"lang": {"sv"}}, nil).want(t, 200, `<html lang="sv"`, "Inställningar", "Färgtema")
	if r.path != "/settings" {
		t.Errorf("settings redirected to %s", r.path)
	}
	c.postRaw("/login", url.Values{"username": {"alpha"}, "password": {pw("alpha")}}, nil).
		want(t, 200, `<html lang="sv"`, "Skriv en rapport", "Inkorg", "Logga ut", "grupp").wantNot(t, "Write a report")

	// Invalid values are ignored.
	c.post("/settings", "lang", "xx", "theme", "nope").want(t, 200, `<html lang="sv"`, `data-theme="auto"`, "Inställningarna har sparats.")
	c.post("/settings", "lang", "en").want(t, 200, `<html lang="en"`, "Settings saved.")
}

func TestSwedishFlow(t *testing.T) {
	e := newEnv(t)
	sv := map[string]string{"Accept-Language": "sv"}
	alpha, leader, admin := e.login("alpha"), e.login("leader"), e.login("admin")
	alpha.hdr, leader.hdr, admin.hdr = sv, sv, sv

	// Validation messages, including the translated field name.
	alpha.post("/reports", "template", e.id["Minimal report"], "subject", "", "content", "c").want(t, 422, "ämne måste anges")
	alpha.post("/reports", "template", e.id["SITREP"], "subject", "s", "content", "c").want(t, 422, "Location måste anges")
	alpha.post("/reports", "template", e.id["SITREP"], "subject", "s", "content", "c", "f_location", "x", "f_count", "tolv").want(t, 422, "Count måste vara ett tal")
	alpha.post("/password", "current", pw("alpha"), "new", "kort", "confirm", "kort").want(t, 422, "lösenordet måste ha minst 10 tecken")

	r := alpha.post("/reports", "template", e.id["SITREP"], "subject", "Läge Åre", "content", "Bron är avstängd", "f_location", "Östersund", "f_help_needed", "yes")
	r.want(t, 200, "Rapporten har skickats till Övningsledare.", "Östersund", `<span class="tag new">ny</span>`, "Till</dt><dd>Övningsledare</dd>", "<dd class=\"body\">ja</dd>")
	id := strings.TrimPrefix(r.path, "/reports/")
	alpha.get("/sent").want(t, 200, "Skickade rapporter", "Övningsledare", "1–1 av 1")

	// Search is case-insensitive for Swedish letters too.
	leader.get("/queue?q="+url.QueryEscape("ÅRE AVSTÄNGD")).want(t, 200, "Rapportkö", "Läge Åre", "Alla köer", "1–1 av 1", "Tid", "Ämne", "Från")
	leader.get("/queue?q=östersund").want(t, 200, "Läge Åre")
	leader.get("/queue?q=finnsinte").want(t, 200, "Inga rapporter matchar filtret.", "0 rapporter")
	leader.post("/reports/"+id+"/status", "action", "file", "folder", "Lägesbilder").want(t, 200, "Rapporten har uppdaterats.", "arkiverad", "i mappen <strong>Lägesbilder</strong>")
	leader.post("/queue/bulk", "id", id, "action", "").want(t, 200, "Inget ändrades: välj en åtgärd.")
	leader.post("/queue/bulk", "id", id, "action", "read").want(t, 200, "1 rapport(er) har uppdaterats.")

	leader.post("/templates", "name", "", "rows", "0").want(t, 422, "mallnamn måste anges")
	leader.post("/templates", "name", "Mall", "rows", "1", "f0_label", "Färg", "f0_type", "select").want(t, 422, "fältet &#34;Färg&#34;: ett flervalsfält måste ha alternativ")
	leader.get("/templates/new").want(t, 200, "Ny mall", `<option value="select">flerval</option>`, "Lägg till fält", `aria-label="Etikett, fält 1"`)
	leader.post("/requests", "subject", "", "template", e.id["SITREP"], "to", "all").want(t, 422, "ämne måste anges")
	r = leader.post("/requests", "subject", "Läge kl. 14", "template", e.id["SITREP"], "to", "all").want(t, 200, "Förfrågan har skickats.", "Stäng förfrågan")
	alpha.get("/reports/new?request="+strings.TrimPrefix(r.path, "/requests/")).want(t, 200, `value="Sv: Läge kl. 14"`, "markerar ett obligatoriskt fält")

	admin.get("/admin/users").want(t, 200, "Konton", "Nytt konto", `<option value="group" selected>grupp</option>`, "övningsledare", "administratör")
	admin.post("/admin/users", "username", "x", "display_name", "X", "role", "group").want(t, 422, "användarnamnet måste ha 2–32 tecken")
	admin.post("/admin/users", "username", "charlie", "display_name", "Grupp C", "role", "group").want(t, 200, "Kontot &#34;charlie&#34; har skapats. Engångslösenord: ")
	admin.post("/admin/users/"+e.id["alpha"], "action", "disable").want(t, 200, "Kontot &#34;alpha&#34; har inaktiverats.")
	admin.post("/admin/users/"+e.id["alpha"], "action", "enable").want(t, 200, "Kontot &#34;alpha&#34; har aktiverats.")
	admin.get("/queue").want(t, 403, "Åtkomst nekad", "Du har inte åtkomst till den här sidan.")
	leader.get("/reports/nope").want(t, 404, "Sidan finns inte", "Rapporten finns inte.")
}

func TestThemeSelection(t *testing.T) {
	e := newEnv(t)
	c := e.anon()
	c.get("/login").want(t, 200, `data-theme="auto"`)
	page := c.get("/settings").want(t, 200, "Colour theme", "System default")
	for _, th := range Themes {
		if !strings.Contains(page.body, `<option value="`+th.Name+`"`) {
			t.Errorf("theme %q is not offered", th.Name)
		}
		c.postRaw("/settings", url.Values{"theme": {th.Name}}, nil).want(t, 200, `data-theme="`+th.Name+`"`, `<option value="`+th.Name+`" selected>`)
		c.get("/login").want(t, 200, `<html lang="en" data-theme="`+th.Name+`">`)
	}
	// The requested set of themes exists.
	for _, name := range []string{"light", "dark", "gold", "spring", "autumn", "camouflage", "console", "contrast", "auto"} {
		if !validTheme(name) {
			t.Errorf("theme %q missing", name)
		}
	}
	c.postRaw("/settings", url.Values{"theme": {`"><script>`}}, nil).want(t, 200, `data-theme="console"`).wantNot(t, "<script>")

	// A forged cookie value never reaches the page.
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/login", nil)
	req.AddCookie(&http.Cookie{Name: themeCookie, Value: "x onload=alert(1)"})
	req.AddCookie(&http.Cookie{Name: langCookie, Value: "zz"})
	e.anon().do(req).want(t, 200, `<html lang="en" data-theme="auto">`)

	// The theme follows the browser into the session.
	c.postRaw("/login", url.Values{"username": {"leader"}, "password": {pw("leader")}}, nil).want(t, 200, `data-theme="console"`)
	c.post("/settings", "theme", "gold").want(t, 200, `data-theme="gold"`, "Settings saved.")
}

func TestSettingsCookiesAndCSRF(t *testing.T) {
	for _, secure := range []bool{false, true} {
		e := newEnv(t)
		e.srv.Secure = secure
		c := e.anon()
		c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		req, _ := http.NewRequest(http.MethodPost, e.ts.URL+"/settings", strings.NewReader("lang=sv&theme=dark"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := c.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		got := map[string]*http.Cookie{}
		for _, ck := range resp.Cookies() {
			got[ck.Name] = ck
		}
		for name, want := range map[string]string{langCookie: "sv", themeCookie: "dark"} {
			ck := got[name]
			if ck == nil || ck.Value != want || !ck.HttpOnly || ck.Secure != secure || ck.SameSite != http.SameSiteLaxMode || ck.MaxAge <= 0 || ck.Path != "/" {
				t.Errorf("secure=%v: cookie %s = %+v", secure, name, ck)
			}
		}
	}
	e := newEnv(t)
	// Cross-site requests cannot change preferences; a logged-in user
	// needs the CSRF token like everywhere else.
	e.anon().postRaw("/settings", url.Values{"theme": {"dark"}}, map[string]string{"Sec-Fetch-Site": "cross-site"}).want(t, 403)
	c := e.login("alpha")
	c.postRaw("/settings", url.Values{"theme": {"dark"}}, nil).want(t, 403, "form has expired")
	c.get("/settings").want(t, 200, `data-theme="auto"`, `name="csrf"`, "Group Alpha")
	// The settings stay reachable while a one-time password is pending.
	u, _ := e.st.User(e.id["bravo"])
	u.MustChange = true
	if err := e.st.PutUser(u); err != nil {
		t.Fatal(err)
	}
	b := e.anon()
	b.postRaw("/login", url.Values{"username": {"bravo"}, "password": {pw("bravo")}}, nil).want(t, 200, "one-time password")
	if got := b.get("/settings").want(t, 200, "Colour theme").path; got != "/settings" {
		t.Errorf("settings redirected to %s", got)
	}
}

func TestNewCountAPI(t *testing.T) {
	e := newEnv(t)
	alpha, leader := e.login("alpha"), e.login("leader")
	count := func(c *client) int {
		t.Helper()
		r := c.get("/api/new-count").want(t, 200)
		if ct := r.header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type %q", ct)
		}
		if r.header.Get("Cache-Control") != "no-store" {
			t.Error("count response is cacheable")
		}
		var v struct{ New int }
		if err := json.Unmarshal([]byte(r.body), &v); err != nil {
			t.Fatalf("%v: %s", err, r.body)
		}
		return v.New
	}
	if count(leader) != 0 {
		t.Error("empty queue has new reports")
	}
	id := alpha.send("one", "c")
	alpha.send("two", "c")
	if count(leader) != 2 || count(alpha) != 0 {
		t.Errorf("leader=%d alpha=%d", count(leader), count(alpha))
	}
	leader.get("/reports/" + id)
	if count(leader) != 1 {
		t.Errorf("after reading one: %d", count(leader))
	}
	e.login("admin").get("/api/new-count").want(t, 403)
	if got := e.anon().get("/api/new-count").path; got != "/login" {
		t.Errorf("anonymous count request ended at %s", got)
	}
	// The page carries what the script needs, and nothing inline.
	leader.get("/queue").want(t, 200, `id="live"`, `data-url="/api/new-count"`, `data-count="1"`, `<script src="/static/app.js" defer></script>`, `id="select-all"`)
	r := e.anon().get("/static/app.js").want(t, 200, "'use strict'")
	// The script must stay within what the CSP argument relies on: it never
	// builds markup or evaluates text.
	for _, banned := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function", "http://", "https://"} {
		if strings.Contains(r.body, banned) {
			t.Errorf("app.js uses %s", banned)
		}
	}
	if ct := r.header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("script content type %q", ct)
	}
}

func TestPageHelpers(t *testing.T) {
	p := &page{Lang: i18n.SV, Path: "/requests/abc"}
	if !p.Nav("/requests") || p.Nav("/req") || p.Nav("/queue") {
		t.Error("Nav")
	}
	if p.Tr(i18n.Label("Gold")) != "Guld" || p.Tr("new") != "ny" || p.Tr(7) != "" {
		t.Error("Tr")
	}
}
