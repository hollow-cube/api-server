package v4Internal

import "time"

type DisplayNamePart struct {
	Color string `json:"color"`
	Text  string `json:"text"`
	Type  string `json:"type"`
}

type DisplayName []DisplayNamePart

type PlayerSkin struct {
	Signature string `json:"signature"`
	Texture   string `json:"texture"`
}

type PlayerSettings map[string]any

type PlayerData struct {
	BetaEnabled    bool        `json:"betaEnabled"`
	Coins          int         `json:"coins"`
	Cubits         int         `json:"cubits"`
	Username       string      `json:"username"`
	DisplayName    DisplayName `json:"displayName"`
	Experience     int         `json:"experience"`
	FirstJoin      time.Time   `json:"firstJoin"`
	HypercubeUntil *time.Time  `json:"hypercubeUntil"`
	Id             string      `json:"id"`
	LastOnline     time.Time   `json:"lastOnline"`

	// MapSlots Total number of map slots available to the player (incl. default & bonuses).
	MapSlots int `json:"mapSlots"`
	// MapBuilders Amount of builders player is allowed to have on a map they own (not including self)
	MapBuilders int `json:"mapBuilders"`

	// Permissions String of uint64 flags
	Permissions string         `json:"permissions"`
	Playtime    int            `json:"playtime"`
	Settings    PlayerSettings `json:"settings"`
	Skin        *PlayerSkin    `json:"skin"`

	// TempMaxMapSize Map size enum, probably will convert to a string later when we decide about tall or custom map sizes
	TempMaxMapSize int  `json:"tempMaxMapSize"`
	TotpEnabled    bool `json:"totpEnabled"`
}
