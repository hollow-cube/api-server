package model

import "time"

type MessageType int

const (
	ChatUnsigned MessageType = iota
	ChatSystem               // System message for a particular player. Only sent from here to a server, never from a server
)

// ChatChannel is where the person is speaking. it has some known values, but can also be a player uuid for a direct message to that player.
type ChatChannel string

const (
	ChannelGlobal ChatChannel = "global"
	ChannelReply  ChatChannel = "reply"
	ChannelStaff  ChatChannel = "staff"
)

type ClientChatMessage struct {
	Type MessageType `json:"type"`

	// Unsigned chat
	Channel    ChatChannel `json:"channel"` // Global for global chat, a player uuid for a dm.
	Sender     string      `json:"sender"`
	Message    string      `json:"message"`
	CurrentMap string      `json:"currentMap"` // The map ID the sender is in if it is published, or empty.
	Seed       int64       `json:"seed"`
}

type ChatMessage struct {
	Type MessageType `json:"type"`

	// Unsigned chat
	Channel            ChatChannel   `json:"channel,omitempty"`
	Sender             string        `json:"sender,omitempty"`
	Parts              []MessagePart `json:"parts,omitempty"`
	Seed               int64         `json:"seed,omitempty"`               // Passed along seed for generating random parts of this message (sus emojis)
	SenderHasHypercube bool          `json:"senderHasHypercube,omitempty"` // Whether the sender has a hypercube, used to determine if they can send hypercube messages.

	// System message
	Target string   `json:"target,omitempty"` // The player the message is for
	Key    string   `json:"key,omitempty"`    // The translation key of the message
	Args   []string `json:"args,omitempty"`   // The arguments to the message

	Extra *ChatMessage `json:"extra,omitempty"` // A follow up message, typically a system message.
}

type MessagePart interface {
	messagePart()
}

type MessagePartType int

const (
	PartTypeRaw MessagePartType = iota
	PartTypeEmoji
	PartTypeMap
	PartTypeUrl
)

type RawMessagePart struct {
	Type MessagePartType `json:"type"`
	Text string          `json:"text"`
}

type UrlMessagePart struct {
	Type MessagePartType `json:"type"`
	Text string          `json:"text"`
}

type EmojiMessagePart struct {
	Type MessagePartType `json:"type"`
	Name string          `json:"name"`
}

type MapMessagePart struct {
	Type  MessagePartType `json:"type"`
	MapID string          `json:"mapId"`
}

func (*RawMessagePart) messagePart()   {}
func (*UrlMessagePart) messagePart()   {}
func (*EmojiMessagePart) messagePart() {}
func (*MapMessagePart) messagePart()   {}

type StoredChatMessage struct {
	Timestamp time.Time
	ServerId  string
	Channel   ChatChannel // Depends on context. Global for global chat, a player uuid for a dm.
	Sender    string
	Target    *string // unused, should delete
	Content   string

	CensoredBy     *string // Present if the message was censored. Can be from a moderator (their uuid), or the server ie `filter`, `openai`, `perspective`, etc
	CensoredDetail *string // detail from the model that censored the message, or nil.
}
