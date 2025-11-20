package model

import (
	"time"
)

type InviteType string

const (
	InviteTypeInvite  InviteType = "invite"
	InviteTypeRequest InviteType = "request"
)

type MapInvite struct {
	Type InviteType

	SenderId    string
	RecipientId string

	MapId     string
	CreatedAt time.Time
}

type MapInviteOrRequestQuery struct {
	SenderId    string
	RecipientId string
	MapId       string
}

type CreatedMapInviteMessage struct {
	Type        InviteType `json:"type"`
	SenderId    string     `json:"senderId"`
	RecipientId string     `json:"recipientId"`
	MapId       string     `json:"mapId"`
}

type MapInviteAcceptedOrRejectedMessage struct {
	Type        InviteType `json:"type"`
	SenderId    string     `json:"senderId"`
	RecipientId string     `json:"recipientId"`
	MapId       string     `json:"mapId"`
	Accepted    bool       `json:"accepted"`
}
