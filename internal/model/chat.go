package model

import (
	"time"

	"github.com/rom/gr_report/internal/i18n"
)

// MaxChatLen is the maximum length of a chat message in characters.
const MaxChatLen = 2000

// ChatAll is the conversation every group and the exercise leader share.
const ChatAll = "all"

// ChatMessage is one message. Messages are immutable.
//
// A conversation has parties, not users: a party is a group account's ID,
// or ReceiverLeader for the exercise leaders, who share their conversations
// the same way they share the report queue.
type ChatMessage struct {
	Seq         int64 // assigned by the store; orders messages
	Conv        string
	Timestamp   time.Time
	SenderParty string
	SenderName  string
	Body        string
	// Overseen is set on a message between two groups that was sent while
	// leader oversight was switched on. Only such messages are ever shown
	// to the exercise leader; the flag is fixed when the message is sent.
	Overseen bool
}

// ChatParty returns the party a user chats as, or "" when the role takes
// no part in the exercise traffic (administrators).
func ChatParty(u *User) string { return u.Party() }

// ChatConv returns the conversation key and its two parties for a party
// and the peer it talks to. peer is ChatAll, ReceiverLeader or a group ID.
// The key of a direct conversation is the same from both sides.
func ChatConv(party, peer string) (conv, a, b string) {
	if peer == ChatAll {
		return ChatAll, "", ""
	}
	a, b = party, peer
	if b < a {
		a, b = b, a
	}
	return a + "|" + b, a, b
}

// CleanChat normalises and validates the text of a chat message.
func CleanChat(body string) (string, error) {
	body = CleanText(body)
	if err := checkLen(i18n.Label("message"), body, 1, MaxChatLen); err != nil {
		return "", err
	}
	return body, nil
}
