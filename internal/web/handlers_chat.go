package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/model"
	"github.com/rom/gr_report/internal/store"
)

// Chat limits.
const (
	chatPageSize = 100
	// A user may send chatBurst messages per chatWindow.
	chatBurst  = 20
	chatWindow = time.Minute
)

// chatLimiter is a fixed-window counter per user. It only has to stop a
// stuck key or a script from flooding a conversation.
type chatLimiter struct {
	mu sync.Mutex
	m  map[string]*chatCount
}

type chatCount struct {
	start time.Time
	n     int
}

func (l *chatLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]*chatCount{}
	}
	c := l.m[key]
	if c == nil || now.Sub(c.start) >= chatWindow {
		// Entries are per account, so the map is bounded by the number
		// of accounts; stale ones are simply overwritten.
		l.m[key] = &chatCount{start: now, n: 1}
		return true
	}
	if c.n >= chatBurst {
		return false
	}
	c.n++
	return true
}

type chatPeer struct {
	ID     string
	Name   string
	Unread int
	Active bool
}

type chatLine struct {
	Seq    int64  `json:"seq"`
	Time   string `json:"time"`
	Sender string `json:"sender"`
	Body   string `json:"body"`
	Mine   bool   `json:"mine"`
	// Overseen: the exercise leader can read this message.
	Overseen bool `json:"overseen"`
}

// oversightSwitch describes the oversight control shown to leaders.
type oversightSwitch struct {
	On        bool
	CanToggle bool
	Info      string // who switched it and when, or that the configuration decides
}

// oversightLink is a group-to-group conversation a leader may look into.
type oversightLink struct {
	Name   string
	URL    string
	Count  int
	Active bool
}

type chatView struct {
	Peers      []chatPeer
	Peer       chatPeer
	Lines      []chatLine
	Last       int64 // highest sequence number shown
	EarlierURL string
	Older      bool // an older page is shown, not the newest messages
	Draft      string
	MaxLen     int
	Privacy    string
	// Oversight is the warning shown to groups while the exercise leader
	// can read what they send in this conversation.
	Oversight string
	// Overseen lists the conversations a leader can look into; ReadOnly
	// marks the page as such a look (no form).
	Overseen []oversightLink
	ReadOnly bool
	BaseURL  string
	// PollURL is where the page script fetches new messages; empty when
	// an older page is shown.
	PollURL string
	// OversightApplies: the conversation is between two groups, so the
	// notice is part of the page (hidden while oversight is off) and the
	// form records which state the writer was shown.
	OversightApplies bool
	OversightOn      bool
	PrivacyOverseen  string
	// Switch is the oversight control on a leader's page.
	Switch *oversightSwitch
}

// chatEnabled wraps the chat routes so they disappear when chat is off.
func (s *Server) chatEnabled(h handler) handler {
	return func(c *reqCtx) {
		if !s.Chat {
			s.fail(c, http.StatusNotFound, c.t("The chat is switched off."))
			return
		}
		h(c)
	}
}

// chatPeerName resolves and authorises the peer of a conversation for the
// current user. A conversation is always derived from the user's own party,
// so nobody can open a conversation they are not part of.
func (s *Server) chatPeerName(c *reqCtx, party, peer string) (string, bool) {
	switch peer {
	case model.ChatAll:
		return c.t("Everyone"), true
	case party:
		return "", false
	case model.ReceiverLeader:
		return c.t(model.ReceiverLeaderName), true
	}
	if u, ok := s.Store.User(peer); ok && u.Role == model.RoleGroup {
		return u.DisplayName, true
	}
	return "", false
}

func chatLines(msgs []model.ChatMessage, party string) []chatLine {
	lines := make([]chatLine, 0, len(msgs))
	for _, m := range msgs {
		lines = append(lines, chatLine{Seq: m.Seq, Time: fmtTime(m.Timestamp), Sender: m.SenderName, Body: m.Body,
			Mine: m.SenderParty == party, Overseen: m.Overseen})
	}
	return lines
}

func (s *Server) chatRoot(c *reqCtx) {
	http.Redirect(c.w, c.r, "/chat/"+model.ChatAll, http.StatusSeeOther)
}

func (s *Server) chatPage(c *reqCtx, status int, draft, errMsg string) {
	party, peer := model.ChatParty(&c.user), c.r.PathValue("peer")
	name, ok := s.chatPeerName(c, party, peer)
	if !ok {
		s.fail(c, http.StatusNotFound, c.t("No such conversation."))
		return
	}
	conv, _, _ := model.ChatConv(party, peer)
	before, _ := strconv.ParseInt(c.r.URL.Query().Get("before"), 10, 64)
	if before < 0 {
		before = 0
	}
	// One extra row tells whether there are earlier messages.
	msgs := s.Store.ChatMessages(conv, 0, before, chatPageSize+1, false)
	v := &chatView{Peer: chatPeer{ID: peer, Name: name, Active: true}, Draft: draft, MaxLen: model.MaxChatLen, Older: before > 0,
		BaseURL: "/chat/" + peer}
	if len(msgs) > chatPageSize {
		msgs = msgs[1:]
		v.EarlierURL = v.BaseURL + "?before=" + strconv.FormatInt(msgs[0].Seq, 10)
	}
	v.Lines = chatLines(msgs, party)
	if n := len(msgs); n > 0 {
		v.Last = msgs[n-1].Seq
		if before == 0 {
			if s.storeErr(c, s.Store.MarkChatRead(party, conv, v.Last)) {
				return
			}
		}
	}
	if before == 0 {
		v.PollURL = "/api/chat/" + peer
	}
	switch {
	case peer == model.ChatAll:
		v.Privacy = c.t("All groups and the exercise leader can read this conversation.")
	case party == model.ReceiverLeader:
		v.Privacy = c.t("Only %s and the exercise leaders can read this conversation.", name)
	case !oversightApplies(party, peer):
		v.Privacy = c.t("Only %s and your group can read this conversation.", name)
	default:
		// Both wordings are on the page; the one that applies is shown
		// and the page script swaps them if oversight changes meanwhile.
		v.OversightApplies, v.OversightOn = true, s.OversightOn()
		v.Privacy = c.t("Only %s and your group can read this conversation.", name)
		v.PrivacyOverseen = c.t("%s and your group can read this conversation.", name)
		v.Oversight = c.t("Oversight is on: the exercise leader can read the messages you send here. Messages the exercise leader can read are marked.")
	}

	unread := s.Store.ChatUnread(party)
	add := func(id, name string) {
		cv, _, _ := model.ChatConv(party, id)
		v.Peers = append(v.Peers, chatPeer{ID: id, Name: name, Unread: unread[cv], Active: id == peer})
	}
	add(model.ChatAll, c.t("Everyone"))
	if party != model.ReceiverLeader {
		add(model.ReceiverLeader, c.t(model.ReceiverLeaderName))
	}
	for _, g := range s.Store.Users() {
		if g.Role != model.RoleGroup || g.ID == party {
			continue
		}
		cv, _, _ := model.ChatConv(party, g.ID)
		// A disabled group stays listed only while something is unread
		// or it is the conversation being shown.
		if g.Disabled && unread[cv] == 0 && g.ID != peer {
			continue
		}
		add(g.ID, g.DisplayName)
	}
	v.Overseen = s.oversightLinks(c, "")
	v.Switch = s.oversightSwitch(c)
	s.render(c, status, "chat", c.t("Chat with %s", name), v, errMsg)
}

func (s *Server) chat(c *reqCtx) { s.chatPage(c, http.StatusOK, "", "") }

func (s *Server) chatSend(c *reqCtx) {
	party, peer := model.ChatParty(&c.user), c.r.PathValue("peer")
	if _, ok := s.chatPeerName(c, party, peer); !ok {
		s.fail(c, http.StatusNotFound, c.t("No such conversation."))
		return
	}
	draft := c.r.PostForm.Get("body")
	body, err := model.CleanChat(draft)
	if err != nil {
		s.chatPage(c, http.StatusUnprocessableEntity, draft, i18n.TErr(c.lang, err))
		return
	}
	// The writer must have been shown that the exercise leader can read
	// the message. If oversight was switched on after the page was loaded,
	// nothing is sent: the page comes back with the notice and the draft.
	if s.overseen(party, peer) && c.r.PostForm.Get("oversight_seen") != "1" {
		s.chatPage(c, http.StatusConflict, draft, c.t("Oversight was switched on after you opened this page: the exercise leader can now read what you send here. Your message has not been sent. Send it again if you still want to."))
		return
	}
	if !s.chatLimit.allow(c.user.ID, s.Now()) {
		c.w.Header().Set("Retry-After", strconv.Itoa(int(chatWindow.Seconds())))
		s.chatPage(c, http.StatusTooManyRequests, draft, c.t("You are sending messages too quickly. Wait a moment and try again."))
		return
	}
	conv, a, b := model.ChatConv(party, peer)
	m, err := s.Store.AddChatMessage(model.ChatMessage{Conv: conv, Timestamp: s.Now().UTC(), SenderParty: party,
		SenderName: c.user.DisplayName, Body: body, Overseen: s.overseen(party, peer)}, a, b)
	if s.storeErr(c, err) {
		return
	}
	// The content of a message is never written to the log.
	s.audit(c, "chat_sent", "conversation", conv, "seq", m.Seq)
	http.Redirect(c.w, c.r, "/chat/"+peer+"#send", http.StatusSeeOther)
}

// chatPoll serves the messages after a sequence number to the page script
// and marks them as read: the conversation is open in front of the user.
func (s *Server) chatPoll(c *reqCtx) {
	party, peer := model.ChatParty(&c.user), c.r.PathValue("peer")
	if _, ok := s.chatPeerName(c, party, peer); !ok {
		http.Error(c.w, "not found", http.StatusNotFound)
		return
	}
	after, _ := strconv.ParseInt(c.r.URL.Query().Get("after"), 10, 64)
	conv, _, _ := model.ChatConv(party, peer)
	var msgs []model.ChatMessage
	if after > 0 {
		msgs = s.Store.ChatMessages(conv, after, 0, chatPageSize, false)
	} else {
		msgs = s.Store.ChatMessages(conv, 0, 0, chatPageSize, false)
	}
	if n := len(msgs); n > 0 {
		if err := s.Store.MarkChatRead(party, conv, msgs[n-1].Seq); err != nil {
			s.Log.Error("store", "err", err, "path", c.r.URL.Path)
		}
	}
	c.w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(c.w).Encode(map[string]any{"messages": chatLines(msgs, party), "oversight": s.overseen(party, peer)})
}

// chatUnreadTotal is the number shown on the Chat menu entry.
func (s *Server) chatUnreadTotal(u *model.User) int {
	party := model.ChatParty(u)
	if !s.Chat || party == "" {
		return 0
	}
	total := 0
	for _, n := range s.Store.ChatUnread(party) {
		total += n
	}
	return total
}

// ---- leader oversight of group-to-group conversations ----

// overseen reports whether a message from party to peer is sent under
// leader oversight: the setting is on and both ends are groups. The answer
// is stored with the message, so what the sender was told stays true.
func (s *Server) overseen(party, peer string) bool {
	return oversightApplies(party, peer) && s.OversightOn()
}

// oversightApplies reports whether a conversation is one between two
// groups, the only kind oversight is about.
func oversightApplies(party, peer string) bool {
	return party != model.ReceiverLeader && peer != model.ReceiverLeader && peer != model.ChatAll
}

// settingOversight is the key of the oversight switch in the database.
const settingOversight = "chat_oversight"

// oversightState returns whether oversight is on and, when that was decided
// with the switch in the user interface, by whom and when.
func (s *Server) oversightState() (on bool, set store.Setting, fromUI bool) {
	if s.ChatOversightToggle {
		if v, ok := s.Store.Setting(settingOversight); ok {
			return v.Value == "on", v, true
		}
	}
	return s.ChatOversight, store.Setting{}, false
}

// OversightOn reports whether leader oversight of group chat is on.
func (s *Server) OversightOn() bool {
	on, _, _ := s.oversightState()
	return on
}

// chatOversightSet is the switch on the leader's chat page.
func (s *Server) chatOversightSet(c *reqCtx) {
	if !s.ChatOversightToggle {
		s.fail(c, http.StatusForbidden, c.t("Oversight is set in the server configuration and cannot be changed here."))
		return
	}
	var on bool
	switch c.r.PostForm.Get("oversight") {
	case "on":
		on = true
	case "off":
	default:
		s.fail(c, http.StatusBadRequest, c.t("Unknown action."))
		return
	}
	if on == s.OversightOn() {
		s.redirect(c, "/chat", "")
		return
	}
	value := "off"
	if on {
		value = "on"
	}
	if s.storeErr(c, s.Store.PutSetting(settingOversight, store.Setting{Value: value, UpdatedBy: c.user.Username, UpdatedAt: s.Now().UTC()})) {
		return
	}
	s.audit(c, "chat_oversight_set", "oversight", value)
	msg := c.t("Oversight is now off. Conversations between groups are private again.")
	if on {
		msg = c.t("Oversight is now on. The groups are told in their conversations; you can read messages they send from now on.")
	}
	s.redirect(c, "/chat", msg)
}

// oversightLinks lists, for a leader and while oversight is on, the
// group-to-group conversations that hold overseen messages.
func (s *Server) oversightLinks(c *reqCtx, activeConv string) []oversightLink {
	if !s.OversightOn() || c.user.Role != model.RoleLeader {
		return nil
	}
	var out []oversightLink
	for _, oc := range s.Store.ChatOverseen() {
		a, okA := s.Store.User(oc.A)
		b, okB := s.Store.User(oc.B)
		if !okA || !okB {
			continue
		}
		conv, _, _ := model.ChatConv(oc.A, oc.B)
		if strings.ToLower(b.DisplayName) < strings.ToLower(a.DisplayName) {
			a, b = b, a // show the pair in alphabetical order
		}
		out = append(out, oversightLink{Name: a.DisplayName + " ↔ " + b.DisplayName, URL: "/chat/oversight/" + a.ID + "/" + b.ID,
			Count: oc.Count, Active: conv == activeConv})
	}
	return out
}

// oversightPair resolves the two groups of an oversight request. It fails
// unless oversight is on and both are distinct group accounts.
func (s *Server) oversightPair(c *reqCtx) (a, b model.User, conv string, ok bool) {
	a, okA := s.Store.User(c.r.PathValue("a"))
	b, okB := s.Store.User(c.r.PathValue("b"))
	if !s.OversightOn() || !okA || !okB || a.Role != model.RoleGroup || b.Role != model.RoleGroup || a.ID == b.ID {
		return a, b, "", false
	}
	conv, _, _ = model.ChatConv(a.ID, b.ID)
	return a, b, conv, true
}

// chatOversight shows a leader the overseen messages of a conversation
// between two groups. It is read-only and every look is written to the
// audit log.
func (s *Server) chatOversight(c *reqCtx) {
	a, b, conv, ok := s.oversightPair(c)
	if !ok {
		s.fail(c, http.StatusNotFound, c.t("No such conversation."))
		return
	}
	before, _ := strconv.ParseInt(c.r.URL.Query().Get("before"), 10, 64)
	if before < 0 {
		before = 0
	}
	name := a.DisplayName + " ↔ " + b.DisplayName
	v := &chatView{Peer: chatPeer{Name: name}, ReadOnly: true, Older: before > 0, BaseURL: "/chat/oversight/" + a.ID + "/" + b.ID,
		Privacy: c.t("Oversight: you can read this conversation between two groups but not write in it. Only messages sent while oversight was switched on are shown; the groups see which ones.")}
	msgs := s.Store.ChatMessages(conv, 0, before, chatPageSize+1, true)
	if len(msgs) > chatPageSize {
		msgs = msgs[1:]
		v.EarlierURL = v.BaseURL + "?before=" + strconv.FormatInt(msgs[0].Seq, 10)
	}
	v.Lines = chatLines(msgs, "")
	if n := len(msgs); n > 0 {
		v.Last = msgs[n-1].Seq
	}
	if before == 0 {
		v.PollURL = "/api" + v.BaseURL
	}
	unread := s.Store.ChatUnread(model.ReceiverLeader)
	cvAll, _, _ := model.ChatConv(model.ReceiverLeader, model.ChatAll)
	v.Peers = append(v.Peers, chatPeer{ID: model.ChatAll, Name: c.t("Everyone"), Unread: unread[cvAll]})
	for _, g := range s.Store.Groups() {
		cv, _, _ := model.ChatConv(model.ReceiverLeader, g.ID)
		v.Peers = append(v.Peers, chatPeer{ID: g.ID, Name: g.DisplayName, Unread: unread[cv]})
	}
	v.Overseen = s.oversightLinks(c, conv)
	v.Switch = s.oversightSwitch(c)
	s.audit(c, "chat_oversight_view", "conversation", conv, "messages", len(msgs))
	s.render(c, http.StatusOK, "chat", name, v, "")
}

// chatOversightPoll serves new overseen messages to the oversight page. It
// applies the same checks as the page, never marks anything read, and adds
// an audit entry whenever it actually delivers messages - an idle poll
// every few seconds is not logged.
func (s *Server) chatOversightPoll(c *reqCtx) {
	_, _, conv, ok := s.oversightPair(c)
	if !ok {
		http.Error(c.w, "not found", http.StatusNotFound)
		return
	}
	after, _ := strconv.ParseInt(c.r.URL.Query().Get("after"), 10, 64)
	if after < 0 {
		after = 0
	}
	msgs := s.Store.ChatMessages(conv, after, 0, chatPageSize, true)
	if len(msgs) > 0 {
		s.audit(c, "chat_oversight_view", "conversation", conv, "messages", len(msgs), "live", true)
	}
	c.w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(c.w).Encode(map[string]any{"messages": chatLines(msgs, "")})
}

// oversightSwitch builds the oversight control for a leader's chat page.
func (s *Server) oversightSwitch(c *reqCtx) *oversightSwitch {
	if c.user.Role != model.RoleLeader {
		return nil
	}
	on, set, fromUI := s.oversightState()
	sw := &oversightSwitch{On: on, CanToggle: s.ChatOversightToggle}
	switch {
	case fromUI && on:
		sw.Info = c.t("Switched on by %s at %s.", set.UpdatedBy, fmtTime(set.UpdatedAt))
	case fromUI:
		sw.Info = c.t("Switched off by %s at %s.", set.UpdatedBy, fmtTime(set.UpdatedAt))
	case !s.ChatOversightToggle:
		sw.Info = c.t("This is set in the server configuration and cannot be changed here.")
	}
	return sw
}
