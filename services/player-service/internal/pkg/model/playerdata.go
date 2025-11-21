package model

import (
	"time"
)

type LinkedAccountType string

const ( // Does NOT replace VerificationType
	AccountTypeDiscord LinkedAccountType = "discord"
	// Add Instagram, TikTok, forums or any other social media platform
)

type LinkedAccount struct {
	Type   LinkedAccountType
	UserId string
}

type LinkedAccountsSlice []LinkedAccount

type PlayerData struct {
	Id         string
	Username   string
	FirstJoin  time.Time
	LastOnline time.Time // Updated when the player joins and leaves
	Playtime   int64     // In Milliseconds
	Settings   PlayerSettings

	BetaEnabled bool

	Experience   int
	HypercubeExp int // The total amount of hypercube exp they have. Never resets.
	Coins        int
	Cubits       int

	LinkedAccounts LinkedAccountsSlice // LinkedAccount Type - User IDs
}

type PlayerSettings map[string]interface{}

type PlayerDataUpdateAction int

const (
	PlayerDataUpdate_Modify PlayerDataUpdateAction = iota
)

type PlayerDataUpdateMessage struct {
	Action PlayerDataUpdateAction `json:"action"`
	Id     string                 `json:"id"` // Player ID of the update

	// Modify (all optional, only present will be applied)
	//todo reenable sending backpack,exp,coins in the future.
	Backpack     PlayerBackpack `json:"-"`                      // Present fields will replace current value
	Exp          *int           `json:"-"`                      // Replaces the current exp
	HypercubeExp *int           `json:"hypercubeExp,omitempty"` // Replaces the current hypercube exp
	Coins        *int           `json:"-"`                      // Replaces the current coins
	Cubits       *int           `json:"cubits,omitempty"`       // Replaces the current cubits

	// The reason for the update, or nil if it is unspecified.
	// For example, used to acknowledge a cubits/hypercube/etc purchase
	Reason *UpdateReason `json:"reason,omitempty"`
}

type UpdateReasonType int

const (
	UpdateReason_Cubits UpdateReasonType = iota
	UpdateReason_Hypercube
	UpdateReason_Vote
)

type UpdateReason struct {
	Type UpdateReasonType `json:"type"`

	// Present for cubits or hypercube
	// Number of purchased cubits (total), Added hypercube duration in minutes (total)
	Quantity int `json:"quantity,omitempty"`
	// Present for votes, identifies where they voted (eg minecraftservers.org)
	VoteSource string `json:"voteSource,omitempty"`
	// Present for votes, contains the same updates as the parent update, but they are relative not absolute.
	// Action, Id, DisplayName, Reason are always omitted
	// todo not a huge fan of how this works, but its OK for now.
	RelativeUpdate *PlayerDataUpdateMessage `json:"relativeUpdate,omitempty"`
}
