package web

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func attr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

func ancestor(n *html.Node, a atom.Atom) *html.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && p.DataAtom == a {
			return p
		}
	}
	return nil
}

func child(n *html.Node, a atom.Atom) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == a {
			return c
		}
	}
	return nil
}

// audit checks a rendered page against the accessibility rules that can be
// verified from markup alone (WCAG 2.1 success criteria in brackets).
func audit(t *testing.T, where, lang, body string) {
	t.Helper()
	for _, problem := range problems(lang, body) {
		t.Errorf("%s: %s", where, problem)
	}
}

// problems returns the accessibility violations found in a page.
func problems(lang, body string) []string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return []string{err.Error()}
	}
	var found []string
	fail := func(format string, a ...any) {
		found = append(found, fmt.Sprintf(format, a...))
	}
	var all []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			all = append(all, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	ids := map[string]bool{}
	labelFor := map[string]bool{}
	for _, n := range all {
		if id, ok := attr(n, "id"); ok {
			if id == "" || ids[id] {
				fail("duplicate or empty id %q [4.1.1]", id)
			}
			ids[id] = true
		}
		if n.DataAtom == atom.Label {
			if f, ok := attr(n, "for"); ok {
				labelFor[f] = true
			}
		}
	}

	h1, lastHeading, mains, firstLink := 0, 0, 0, true
	for _, n := range all {
		// No inline code or styling: required by the CSP and it keeps
		// user style sheets and themes working. [1.4.12]
		for _, a := range n.Attr {
			if a.Key == "style" || strings.HasPrefix(a.Key, "on") {
				fail("<%s> has inline %s attribute", n.Data, a.Key)
			}
			if a.Key == "aria-describedby" || a.Key == "aria-labelledby" || (a.Key == "for" && n.DataAtom == atom.Label) || a.Key == "list" {
				for _, ref := range strings.Fields(a.Val) {
					if !ids[ref] {
						fail("<%s %s=%q> refers to a missing id [1.3.1]", n.Data, a.Key, ref)
					}
				}
			}
			if a.Key == "tabindex" && a.Val != "-1" && a.Val != "0" {
				fail("positive tabindex on <%s> [2.4.3]", n.Data)
			}
		}
		switch n.DataAtom {
		case atom.Html:
			if v, _ := attr(n, "lang"); v != lang {
				fail("<html lang=%q>, want %q [3.1.1]", v, lang)
			}
			if v, _ := attr(n, "data-theme"); !validTheme(v) {
				fail("invalid data-theme %q", v)
			}
		case atom.Title:
			if textOf(n) == "" || strings.HasPrefix(textOf(n), "–") {
				fail("empty <title> [2.4.2]")
			}
		case atom.Meta:
			if v, _ := attr(n, "name"); v == "viewport" {
				if c, _ := attr(n, "content"); strings.Contains(c, "user-scalable=no") || strings.Contains(c, "maximum-scale") {
					fail("viewport blocks zooming [1.4.4]")
				}
			}
		case atom.Script:
			if _, ok := attr(n, "src"); !ok || n.FirstChild != nil {
				fail("inline <script>")
			}
		case atom.Style:
			fail("inline <style>")
		case atom.Main:
			mains++
			if id, _ := attr(n, "id"); id != "main" {
				fail("<main> lacks id=main (skip link target) [2.4.1]")
			}
		case atom.H1, atom.H2, atom.H3, atom.H4:
			level := int(n.Data[1] - '0')
			if level == 1 {
				h1++
			}
			if level > lastHeading+1 {
				fail("heading level jumps from h%d to h%d [1.3.1]", lastHeading, level)
			}
			lastHeading = level
			if textOf(n) == "" {
				fail("empty heading [2.4.6]")
			}
		case atom.A:
			href, _ := attr(n, "href")
			if firstLink {
				firstLink = false
				if href != "#main" {
					fail("the first link is %q, not the skip link [2.4.1]", href)
				}
			}
			if _, ok := attr(n, "aria-label"); !ok && textOf(n) == "" {
				fail("link %q has no text [2.4.4]", href)
			}
			if t, _ := attr(n, "target"); t == "_blank" {
				fail("link %q opens a new window without warning [3.2.5]", href)
			}
		case atom.Button:
			if _, ok := attr(n, "aria-label"); !ok && textOf(n) == "" {
				fail("button without a name [4.1.2]")
			}
		case atom.Img:
			if _, ok := attr(n, "alt"); !ok {
				fail("<img> without alt [1.1.1]")
			}
		case atom.Input, atom.Select, atom.Textarea:
			typ, _ := attr(n, "type")
			if typ == "hidden" {
				continue
			}
			id, _ := attr(n, "id")
			_, aria := attr(n, "aria-label")
			_, labelled := attr(n, "aria-labelledby")
			name, _ := attr(n, "name")
			named := aria || labelled || (id != "" && labelFor[id]) || ancestor(n, atom.Label) != nil
			if !named {
				fail("form control %q (type %q) has no label [1.3.1, 4.1.2]", name+id, typ)
			}
			if typ == "radio" {
				fs := ancestor(n, atom.Fieldset)
				if fs == nil || child(fs, atom.Legend) == nil {
					fail("radio button %q is not in a fieldset with a legend [1.3.1]", name)
				}
			}
			if _, ok := attr(n, "autofocus"); ok && name != "username" {
				fail("unexpected autofocus on %q [3.2.1]", name)
			}
		case atom.Fieldset:
			if child(n, atom.Legend) == nil {
				fail("<fieldset> without <legend> [1.3.1]")
			}
		case atom.Table:
			_, labelled := attr(n, "aria-labelledby")
			if !labelled && child(n, atom.Caption) == nil {
				fail("<table> without a caption or aria-labelledby [1.3.1]")
			}
		case atom.Th:
			if _, ok := attr(n, "scope"); !ok {
				fail("<th> %q without scope [1.3.1]", textOf(n))
			}
			if _, ok := attr(n, "aria-label"); !ok && textOf(n) == "" {
				fail("empty table header [1.3.1]")
			}
		case atom.Nav, atom.Aside, atom.Section:
			_, a := attr(n, "aria-label")
			_, b := attr(n, "aria-labelledby")
			if !a && !b {
				fail("<%s> landmark without a name [1.3.1]", n.Data)
			}
		}
		if role, _ := attr(n, "role"); role == "alert" || role == "status" {
			if textOf(n) == "" {
				fail("empty live region")
			}
		}
	}
	if h1 != 1 {
		fail("%d <h1> elements, want exactly one [2.4.6]", h1)
	}
	if mains != 1 {
		fail("%d <main> elements, want one [1.3.1]", mains)
	}
	return found
}

// TestAuditDetects makes sure the audit is not vacuous: each kind of
// mistake in a deliberately bad page must be reported.
func TestAuditDetects(t *testing.T) {
	const bad = `<!doctype html><html lang="en" data-theme="pink"><head><title></title>
<meta name="viewport" content="width=device-width, user-scalable=no"><style>p{}</style><script>alert(1)</script></head>
<body><a href="/x"></a><h1>One</h1><h1>Two</h1><h3>Skipped</h3><h2></h2>
<nav><a href="/y" target="_blank">y</a></nav>
<form><input type="text" name="nolabel"><input type="radio" name="r" aria-label="r"><label for="missing">L</label>
<input id="dup" aria-label="a" aria-describedby="gone"><input id="dup" aria-label="b" autofocus tabindex="3">
<fieldset><input type="checkbox" aria-label="c"></fieldset><button></button><select name="s"></select></form>
<img src="x.png"><table><tr><th>H</th><th scope="col"></th></tr></table>
<p style="color:red" onclick="x()" role="alert"></p></body></html>`
	got := strings.Join(problems("sv", bad), "\n")
	for _, want := range []string{
		`<html lang="en">, want "sv"`, "invalid data-theme", "empty <title>", "viewport blocks zooming", "inline <style>", "inline <script>",
		"not the skip link", `link "/x" has no text`, "2 <h1> elements", "jumps from h1 to h3", "empty heading", "<nav> landmark without a name",
		"opens a new window", `form control "nolabel"`, `form control "s"`, "not in a fieldset with a legend", `refers to a missing id`,
		`duplicate or empty id "dup"`, "unexpected autofocus", "positive tabindex", "<fieldset> without <legend>", "button without a name",
		"<img> without alt", "<table> without a caption", `<th> "H" without scope`, "empty table header", "inline style attribute",
		"inline onclick attribute", "empty live region", "0 <main> elements",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("audit missed: %s", want)
		}
	}
	if t.Failed() {
		t.Logf("reported:\n%s", got)
	}
}

// TestAccessibility renders every page for every role in both languages,
// with data present, and audits the markup.
func TestAccessibility(t *testing.T) {
	e := newEnv(t)
	alpha, bravo, leader, admin := e.login("alpha"), e.login("bravo"), e.login("leader"), e.login("admin")

	r := leader.post("/requests", "subject", "Status at 14:00", "body", "Report status", "template", e.id["SITREP"], "to", "some", "recipient", e.id["alpha"])
	reqPath := r.path
	reqID := strings.TrimPrefix(reqPath, "/requests/")
	r = alpha.post("/reports", "request", reqID, "subject", "Re: Status", "content", "All fine", "f_location", "Grid 1", "f_status", "Red", "f_help_needed", "yes")
	sitrep := r.path
	toAlpha := "/reports/" + bravo.send("Need a radio", "Can you lend us one?", "receiver", e.id["alpha"])
	bravo.send("Arrived", "All present")
	leader.post(sitrep+"/status", "action", "file", "folder", "Logistics")
	alpha.post("/chat/all", "body", "Hello everyone")
	leader.post("/chat/"+e.id["alpha"], "body", "A question for Alpha")
	e.srv.ChatOversight = true
	e.srv.ChatOversightToggle = true
	bravo.post("/chat/"+e.id["alpha"], "body", "Sent under oversight", "oversight_seen", "1")

	pages := []struct {
		c     *client
		who   string
		paths []string
	}{
		{e.anon(), "anonymous", []string{"/login", "/settings"}},
		{alpha, "group", []string{"/home", "/requests", reqPath, "/queue", "/sent", "/sent?q=x", toAlpha, sitrep, "/settings", "/password",
			"/reports/new?template=" + e.id["SITREP"], "/reports/new?request=" + reqID, "/reports/nope", "/admin/users",
			"/chat/all", "/chat/leader", "/chat/" + e.id["bravo"], "/chat/nope"}},
		{leader, "leader", []string{"/queue", "/queue?sort=subject&dir=asc&q=fine&status=filed", "/queue?q=nomatch", "/queue?auto=1", sitrep, "/requests", "/requests/new", reqPath,
			"/templates", "/templates/new", "/templates/" + e.id["SITREP"], "/settings", "/password", "/chat/all", "/chat/" + e.id["alpha"],
			"/chat/oversight/" + e.id["alpha"] + "/" + e.id["bravo"]}},
		{admin, "admin", []string{"/admin/users", "/templates", "/templates/new", "/settings", "/queue"}},
	}
	for _, lang := range []string{"en", "sv"} {
		for _, p := range pages {
			p.c.hdr = map[string]string{"Accept-Language": lang}
			for _, path := range p.paths {
				res := p.c.get(path)
				audit(t, fmt.Sprintf("%s %s as %s", lang, path, p.who), lang, res.body)
			}
		}
		// Pages that show a validation error.
		alpha.hdr = map[string]string{"Accept-Language": lang}
		res := alpha.post("/reports", "template", e.id["SITREP"], "subject", "", "content", "")
		audit(t, lang+" report form with error", lang, res.body)
		if !strings.Contains(res.body, `role="alert"`) || !strings.Contains(res.body, `aria-describedby="error"`) {
			t.Errorf("%s: the validation error is not announced and linked to the form", lang)
		}
		res = e.anon().postRaw("/login", url.Values{"username": {"x"}, "password": {"y"}}, map[string]string{"Accept-Language": lang})
		audit(t, lang+" login with error", lang, res.body)
	}

	// Specific promises of the user interface.
	leader.hdr = nil
	q := leader.get("/queue?sort=subject&dir=asc").body
	for _, want := range []string{`aria-sort="ascending"`, `aria-sort="none"`, `aria-current="page"`, `aria-current="true"`,
		`role="search"`, `class="skip" href="#main"`, `<main id="main" tabindex="-1">`, `aria-label="Select: Arrived"`, `aria-live="polite"`} {
		if !strings.Contains(q, want) {
			t.Errorf("queue page lacks %s", want)
		}
	}
	// Status is never conveyed by colour alone: the word is always there.
	if !strings.Contains(q, `<span class="tag new">new</span>`) || !strings.Contains(q, `<span class="tag filed">filed</span>`) {
		t.Error("status tags lack text")
	}
}
