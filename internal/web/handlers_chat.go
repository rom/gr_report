package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/model"
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
		lines = append(lines, chatLine{Seq: m.Seq, Time: fmtTime(m.Timestamp), Sender: m.SenderName, Body: m.Body, Mine: m.SenderParty == party})
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
	msgs := s.Store.ChatMessages(conv, 0, before, chatPageSize+1)
	v := &chatView{Peer: chatPeer{ID: peer, Name: name, Active: true}, Draft: draft, MaxLen: model.MaxChatLen, Older: before > 0}
	if len(msgs) > chatPageSize {
		msgs = msgs[1:]
		v.EarlierURL = "/chat/" + peer + "?before=" + strconv.FormatInt(msgs[0].Seq, 10)
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
	switch {
	case peer == model.ChatAll:
		v.Privacy = c.t("All groups and the exercise leader can read this conversation.")
	case party == model.ReceiverLeader:
		v.Privacy = c.t("Only %s and the exercise leaders can read this conversation.", name)
	default:
		v.Privacy = c.t("Only %s and your group can read this conversation.", name)
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
	if !s.chatLimit.allow(c.user.ID, s.Now()) {
		c.w.Header().Set("Retry-After", strconv.Itoa(int(chatWindow.Seconds())))
		s.chatPage(c, http.StatusTooManyRequests, draft, c.t("You are sending messages too quickly. Wait a moment and try again."))
		return
	}
	conv, a, b := model.ChatConv(party, peer)
	m, err := s.Store.AddChatMessage(model.ChatMessage{Conv: conv, Timestamp: s.Now().UTC(), SenderParty: party,
		SenderName: c.user.DisplayName, Body: body}, a, b)
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
		msgs = s.Store.ChatMessages(conv, after, 0, chatPageSize)
	} else {
		msgs = s.Store.ChatMessages(conv, 0, 0, chatPageSize)
	}
	if n := len(msgs); n > 0 {
		if err := s.Store.MarkChatRead(party, conv, msgs[n-1].Seq); err != nil {
			s.Log.Error("store", "err", err, "path", c.r.URL.Path)
		}
	}
	c.w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(c.w).Encode(map[string]any{"messages": chatLines(msgs, party)})
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
