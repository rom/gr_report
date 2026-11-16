package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rom/gr_report/internal/model"
)

type pollResult struct {
	Messages []chatLine `json:"messages"`
}

func poll(t *testing.T, c *client, path string) pollResult {
	t.Helper()
	r := c.get(path).want(t, 200)
	if ct := r.header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("%s: content type %q", path, ct)
	}
	var v pollResult
	if err := json.Unmarshal([]byte(r.body), &v); err != nil {
		t.Fatalf("%s: %v: %s", path, err, r.body)
	}
	return v
}

func counts(t *testing.T, c *client) (reports, chat int) {
	t.Helper()
	var v struct{ New, Chat int }
	if err := json.Unmarshal([]byte(c.get("/api/new-count").body), &v); err != nil {
		t.Fatal(err)
	}
	return v.New, v.Chat
}

func TestChatFlow(t *testing.T) {
	e := newEnv(t)
	alpha, bravo, leader := e.login("alpha"), e.login("bravo"), e.login("leader")
	fixed := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	e.srv.Now = func() time.Time { return fixed }
	A, B := e.id["alpha"], e.id["bravo"]

	// The entry page is the shared conversation.
	r := alpha.get("/chat").want(t, 200, "Chat", "Everyone", "Exercise leader", "Group Bravo", "No messages yet.",
		"All groups and the exercise leader can read this conversation.", `role="log"`, `data-url="/api/chat/all"`, `data-after="0"`)
	if r.path != "/chat/all" {
		t.Errorf("/chat ended at %s", r.path)
	}
	r.wantNot(t, `href="/chat/`+A+`"`) // nobody chats with themselves
	leader.get("/chat/all").want(t, 200, "Group Alpha", "Group Bravo").wantNot(t, `href="/chat/leader"`)

	// Everyone.
	r = alpha.post("/chat/all", "body", "  Hello everyone\r\nsecond line  ")
	r.want(t, 200, "Hello everyone\nsecond line", "Group Alpha", "2026-05-01 12:00:00Z", `class="chat-line mine"`, `data-after="1"`)
	if r.path != "/chat/all" {
		t.Errorf("after sending at %s", r.path)
	}
	bravo.get("/home").want(t, 200, `<span class="badge js-chat-count">1</span>`)
	if _, chat := counts(t, bravo); chat != 1 {
		t.Errorf("bravo unread = %d", chat)
	}
	if _, chat := counts(t, alpha); chat != 0 {
		t.Errorf("own message counted as unread: %d", chat)
	}
	bravo.get("/chat/all").want(t, 200, "Hello everyone", `<li class="chat-line">`).wantNot(t, `class="chat-line mine"`)
	bravo.get("/home").want(t, 200, `<span class="badge js-chat-count" hidden>0</span>`) // reading clears the counter

	// Direct: group to group. Both sides see the same conversation; the
	// leader and a third party do not.
	alpha.post("/chat/"+B, "body", "psst bravo").want(t, 200, "psst bravo", "Only Group Bravo and your group can read this conversation.")
	bravo.get("/chat/all").want(t, 200, `<span class="badge">1</span>`).wantNot(t, "psst bravo")
	bravo.get("/chat/"+A).want(t, 200, "psst bravo", "Only Group Alpha and your group")
	bravo.post("/chat/"+A, "body", "yes?")
	alpha.get("/chat/"+B).want(t, 200, "psst bravo", "yes?")
	leader.get("/chat/"+A).want(t, 200, "No messages yet.").wantNot(t, "psst bravo")
	leader.get("/chat/"+B).wantNot(t, "psst bravo", "yes?")
	leader.get("/chat/all").wantNot(t, "psst bravo")
	if got := poll(t, leader, "/api/chat/"+A); len(got.Messages) != 0 {
		t.Errorf("leader polled a group-to-group conversation: %+v", got)
	}

	// Direct: group to the exercise leaders, who share the conversation.
	alpha.post("/chat/leader", "body", "question for the staff").want(t, 200, "Only Exercise leader and your group")
	if _, chat := counts(t, leader); chat != 1 { // "Hello everyone" was read above
		t.Errorf("leader unread = %d", chat)
	}
	leader.get("/chat/"+A).want(t, 200, "question for the staff", "Only Group Alpha and the exercise leaders can read this conversation.")
	leader.post("/chat/"+A, "body", "answer from the staff").want(t, 200, "answer from the staff", "Exercise leader")
	alpha.get("/chat/leader").want(t, 200, "answer from the staff")
	bravo.get("/chat/leader").want(t, 200, "No messages yet.").wantNot(t, "question for the staff")

	// Conversations that do not exist for the user.
	for _, path := range []string{"/chat/" + A, "/chat/nope", "/chat/" + e.id["leader"], "/chat/" + e.id["admin"], "/chat/x%7Cy"} {
		alpha.get(path).want(t, 404, "No such conversation.")
		alpha.post(path, "body", "x").want(t, 404)
	}
	leader.get("/chat/leader").want(t, 404)
	alpha.get("/api/chat/"+A).want(t, 404)
	alpha.get("/api/chat/nope").want(t, 404)

	// Validation keeps the draft.
	alpha.post("/chat/all", "body", "   ").want(t, 422, "message is required")
	long := strings.Repeat("å", model.MaxChatLen+1)
	alpha.post("/chat/all", "body", long).want(t, 422, "message must be at most 2000 characters", long[:200])
	alpha.post("/chat/all", "body", strings.Repeat("å", model.MaxChatLen)).want(t, 200)

	// Not logged in, wrong role, missing CSRF token.
	if got := e.anon().get("/chat/all").path; got != "/login" {
		t.Errorf("anonymous chat ended at %s", got)
	}
	e.login("admin").get("/chat").want(t, 403)
	e.login("admin").get("/admin/users").wantNot(t, `href="/chat"`)
	alpha.postRaw("/chat/all", map[string][]string{"body": {"forged"}}, nil).want(t, 403)
	alpha.get("/chat/all").wantNot(t, "forged")
}

func TestChatPollAndPaging(t *testing.T) {
	e := newEnv(t)
	alpha, bravo := e.login("alpha"), e.login("bravo")
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= chatPageSize+30; i++ {
		_, err := e.st.AddChatMessage(model.ChatMessage{Conv: model.ChatAll, Timestamp: base.Add(time.Duration(i) * time.Second),
			SenderParty: e.id["alpha"], SenderName: "Group Alpha", Body: fmt.Sprintf("msg-%03d-end", i)}, "", "")
		if err != nil {
			t.Fatal(err)
		}
	}
	// The page shows the newest messages, oldest first, with a way back.
	r := bravo.get("/chat/all").want(t, 200, "msg-130-end", "msg-031-end", "Show earlier messages", `data-after="130"`).wantNot(t, "msg-030-end", "Show the latest")
	if strings.Index(r.body, "msg-031-end") > strings.Index(r.body, "msg-130-end") {
		t.Error("messages are not in chronological order")
	}
	// An older page does not poll and does not mark anything read.
	bravo.get("/chat/all?before=31").want(t, 200, "msg-030-end", "msg-001-end", "Show the latest messages").wantNot(t, "msg-031-end", "Show earlier", `data-url="/api/chat`)
	bravo.get("/chat/all?before=-5").want(t, 200, "msg-130-end")
	bravo.get("/chat/all?before=x").want(t, 200, "msg-130-end")

	// Polling returns only what is new and marks it read.
	if got := poll(t, bravo, "/api/chat/all?after=130"); len(got.Messages) != 0 {
		t.Errorf("nothing new, got %d", len(got.Messages))
	}
	alpha.post("/chat/all", "body", "fresh <b>one</b>")
	if _, chat := counts(t, bravo); chat != 1 {
		t.Errorf("unread before poll = %d", chat)
	}
	got := poll(t, bravo, "/api/chat/all?after=130")
	if len(got.Messages) != 1 || got.Messages[0].Seq != 131 || got.Messages[0].Body != "fresh <b>one</b>" || got.Messages[0].Mine ||
		got.Messages[0].Sender != "Group Alpha" || got.Messages[0].Time == "" {
		t.Errorf("poll = %+v", got)
	}
	if _, chat := counts(t, bravo); chat != 0 {
		t.Errorf("unread after poll = %d", chat)
	}
	if got := poll(t, alpha, "/api/chat/all?after=130"); len(got.Messages) != 1 || !got.Messages[0].Mine {
		t.Errorf("own message in poll = %+v", got)
	}
	// Without a cursor the newest page comes back; bad cursors are ignored.
	if got := poll(t, bravo, "/api/chat/all"); len(got.Messages) != chatPageSize || got.Messages[chatPageSize-1].Seq != 131 {
		t.Errorf("poll without cursor: %d messages", len(got.Messages))
	}
	if got := poll(t, bravo, "/api/chat/all?after=abc"); len(got.Messages) != chatPageSize {
		t.Errorf("poll with a bad cursor: %d messages", len(got.Messages))
	}
}

func TestChatRateLimit(t *testing.T) {
	e := newEnv(t)
	alpha, bravo := e.login("alpha"), e.login("bravo")
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	e.srv.Now = func() time.Time { return now }
	for i := 0; i < chatBurst; i++ {
		alpha.post("/chat/all", "body", fmt.Sprint("m", i)).want(t, 200)
	}
	r := alpha.post("/chat/all", "body", "one too many").want(t, http.StatusTooManyRequests, "sending messages too quickly", ">one too many</textarea>")
	if r.header.Get("Retry-After") == "" {
		t.Error("no Retry-After")
	}
	bravo.post("/chat/all", "body", "others are unaffected").want(t, 200)
	if n := len(e.st.ChatMessages(model.ChatAll, 0, 0, 100, false)); n != chatBurst+1 {
		t.Errorf("%d messages stored", n)
	}
	now = now.Add(chatWindow)
	alpha.post("/chat/all", "body", "after the pause").want(t, 200, "after the pause")
}

func TestChatDisabledAndSwedish(t *testing.T) {
	e := newEnv(t)
	alpha := e.login("alpha")
	alpha.hdr = map[string]string{"Accept-Language": "sv"}
	alpha.post("/chat/leader", "body", "").want(t, 422, "meddelande måste anges")
	alpha.post("/chat/leader", "body", "Hej").want(t, 200, "Chatt", "Konversationer", "Alla", "Övningsledare", "Skicka",
		"Endast Övningsledare och er grupp kan läsa den här konversationen.", "Meddelande till Övningsledare")
	alpha.get("/chat/nope").want(t, 404, "Konversationen finns inte.")

	// A disabled group disappears from the list unless something is unread.
	u, _ := e.st.User(e.id["bravo"])
	u.Disabled = true
	if err := e.st.PutUser(u); err != nil {
		t.Fatal(err)
	}
	alpha.get("/chat/all").want(t, 200).wantNot(t, "Group Bravo")
	conv, a, b := model.ChatConv(e.id["bravo"], e.id["alpha"])
	if _, err := e.st.AddChatMessage(model.ChatMessage{Conv: conv, SenderParty: e.id["bravo"], SenderName: "Group Bravo", Body: "last words"}, a, b); err != nil {
		t.Fatal(err)
	}
	alpha.get("/chat/all").want(t, 200, "Group Bravo")
	alpha.get("/chat/"+e.id["bravo"]).want(t, 200, "last words")

	e.srv.Chat = false
	alpha.get("/home").want(t, 200).wantNot(t, `href="/chat"`)
	alpha.get("/chat").want(t, 404, "Chatten är avstängd.")
	alpha.get("/chat/all").want(t, 404)
	alpha.post("/chat/all", "body", "x").want(t, 404)
	alpha.get("/api/chat/all").want(t, 404)
	if _, chat := counts(t, alpha); chat != 0 {
		t.Errorf("chat count with chat off = %d", chat)
	}
}

func TestChatOversight(t *testing.T) {
	e := newEnv(t)
	alpha, bravo, leader := e.login("alpha"), e.login("bravo"), e.login("leader")
	A, B := e.id["alpha"], e.id["bravo"]
	view := "/chat/oversight/" + A + "/" + B
	const tag = `class="tag chat-overseen">visible to the exercise leader</span>`

	// Off (the default): private, no tags, no way in for the leader.
	alpha.post("/chat/"+B, "body", "private before oversight").want(t, 200, "Only Group Bravo and your group can read this conversation.").
		wantNot(t, "Oversight is on", tag)
	leader.get(view).want(t, 404)
	leader.get("/chat/all").wantNot(t, "Oversight of group conversations")

	// On: the groups are told before they write, and each message that
	// the leader can read is marked - for sender and receiver.
	e.srv.ChatOversight = true
	r := alpha.get("/chat/"+B).want(t, 200, "Group Bravo and your group can read this conversation.",
		"Oversight is on: the exercise leader can read the messages you send here.", `aria-describedby="oversight-notice"`, "private before oversight")
	r.wantNot(t, "Only Group Bravo", tag) // the earlier message stays unmarked
	alpha.post("/chat/"+B, "body", "sent under oversight").want(t, 200, tag)
	bravo.get("/chat/"+A).want(t, 200, "Oversight is on", "sent under oversight", tag)
	bravo.post("/chat/"+A, "body", "reply under oversight")
	got := poll(t, alpha, "/api/chat/"+B)
	if len(got.Messages) != 3 || got.Messages[0].Overseen || !got.Messages[1].Overseen || !got.Messages[2].Overseen {
		t.Errorf("poll flags = %+v", got.Messages)
	}
	// Conversations that the leader is part of anyway carry no notice.
	alpha.post("/chat/all", "body", "to all").wantNot(t, "Oversight is on", tag)
	alpha.post("/chat/leader", "body", "to the staff").wantNot(t, "Oversight is on", tag)
	for _, m := range e.st.ChatMessages(model.ChatAll, 0, 0, 10, false) {
		if m.Overseen {
			t.Error("shared conversation message flagged as overseen")
		}
	}

	// The leader sees exactly the messages sent under oversight, read-only.
	leader.get("/chat/all").want(t, 200, "Oversight of group conversations", "Group Alpha ↔ Group Bravo", `href="`+view+`"`)
	r = leader.get(view).want(t, 200, "Group Alpha ↔ Group Bravo", "sent under oversight", "reply under oversight",
		"Oversight: you can read this conversation between two groups but not write in it.")
	r.wantNot(t, "private before oversight", "<textarea", `id="send"`, `data-url="/api/chat/`+A, "chat-overseen")
	// The view updates live from its own endpoint, which serves only
	// overseen messages and does not mark anything read.
	api := "/api" + view
	if !strings.Contains(r.body, `data-url="`+api+`" data-after="3"`) {
		t.Errorf("oversight page does not poll its own endpoint: %s", r.body)
	}
	if got := poll(t, leader, api); len(got.Messages) != 2 || got.Messages[0].Body != "sent under oversight" || got.Messages[0].Mine {
		t.Errorf("oversight poll = %+v", got.Messages)
	}
	if got := poll(t, leader, api+"?after=3"); len(got.Messages) != 0 {
		t.Errorf("nothing new expected, got %+v", got.Messages)
	}
	alpha.post("/chat/"+B, "body", "arrives live")
	if got := poll(t, leader, api+"?after=3"); len(got.Messages) != 1 || got.Messages[0].Body != "arrives live" || got.Messages[0].Sender != "Group Alpha" {
		t.Errorf("live oversight poll = %+v", got.Messages)
	}
	if got := poll(t, leader, api+"?after=-9"); len(got.Messages) != 3 {
		t.Errorf("bad cursor: %+v", got.Messages)
	}
	for _, m := range poll(t, leader, api).Messages {
		if strings.Contains(m.Body, "private") {
			t.Errorf("oversight poll leaked a private message: %+v", m)
		}
	}
	// Bravo still has both unread: alpha's live message here and the one
	// in the shared conversation.
	if _, chat := counts(t, bravo); chat != 2 {
		t.Errorf("bravo unread = %d, want 2; the leader's polling must not mark messages read", chat)
	}
	alpha.get(api).want(t, 403)
	e.login("admin").get(api).want(t, 403)
	leader.get("/api/chat/oversight/"+A+"/nope").want(t, 404)
	leader.get("/api/chat/oversight/"+A+"/"+A).want(t, 404)
	leader.get("/chat/oversight/"+B+"/"+A).want(t, 200, "sent under oversight") // either order
	// The normal routes still give the leader nothing of it, and there is
	// no write path.
	leader.get("/chat/"+A).wantNot(t, "sent under oversight", "private before")
	for _, m := range poll(t, leader, "/api/chat/"+A).Messages {
		if strings.Contains(m.Body, "oversight") {
			t.Errorf("leader poll of its own conversation leaked: %+v", m)
		}
	}
	leader.post(view, "body", "leader writes").want(t, http.StatusMethodNotAllowed)
	// Looking does not mark anything read for the groups.
	bravo.post("/chat/"+A, "body", "second reply under oversight")
	leader.get(view).want(t, 200, "second reply under oversight")
	if _, chat := counts(t, alpha); chat != 1 {
		t.Errorf("alpha unread = %d, want 1 (bravo's second reply)", chat)
	}

	// Only leaders, only real pairs of groups.
	alpha.get(view).want(t, 403)
	e.login("admin").get(view).want(t, 403)
	for _, p := range []string{"/chat/oversight/" + A + "/" + A, "/chat/oversight/" + A + "/nope", "/chat/oversight/" + A + "/" + e.id["leader"], "/chat/oversight/leader/" + A} {
		leader.get(p).want(t, 404, "No such conversation.")
	}
	leader.get(view+"?before=-3").want(t, 200, "sent under oversight")

	// Swedish.
	alpha.hdr = map[string]string{"Accept-Language": "sv"}
	alpha.get("/chat/"+B).want(t, 200, "Insyn är påslagen", "synligt för övningsledaren")
	alpha.hdr = nil

	// Switching oversight off closes the leader's view again and new
	// messages are private; what was marked stays marked for the groups.
	e.srv.ChatOversight = false
	leader.get(view).want(t, 404)
	leader.get("/api"+view).want(t, 404) // an already open page stops receiving
	alpha.post("/chat/"+B, "body", "private again").want(t, 200, "Only Group Bravo and your group", tag).wantNot(t, "Oversight is on")
	e.srv.ChatOversight = true
	leader.get(view).want(t, 200, "sent under oversight").wantNot(t, "private again", "private before oversight")

	// Paging of the oversight view.
	for i := 0; i < chatPageSize+5; i++ {
		conv, a, b := model.ChatConv(A, B)
		if _, err := e.st.AddChatMessage(model.ChatMessage{Conv: conv, SenderParty: A, SenderName: "Group Alpha", Body: fmt.Sprintf("bulk-%03d-end", i), Overseen: true}, a, b); err != nil {
			t.Fatal(err)
		}
	}
	r = leader.get(view).want(t, 200, "bulk-104-end", "Show earlier messages").wantNot(t, "bulk-004-end")
	m := regexp.MustCompile(`href="(/chat/oversight/[^"]+\?before=\d+)"`).FindStringSubmatch(r.body)
	if m == nil {
		t.Fatal("no link to earlier messages")
	}
	leader.get(m[1]).want(t, 200, "bulk-004-end", "sent under oversight", "Show the latest messages").wantNot(t, "private before oversight", `data-url="/api/chat`)
}
