package model

import "github.com/hollow-cube/hc-services/services/map-service/internal/db"

// PlayerData represents player data related to maps in particular
type PlayerData struct {
	Id            string   `json:"id"`
	Maps          []string `json:"mapSlots"` // Size always >= UnlockedSlots
	LastPlayedMap string   `json:"lastPlayedMap"`
	LastEditedMap string   `json:"lastEditedMap"`
	ContestSlot   *string  `json:"contestSlot"`

	// Contains some reused cached values. May or may not be filled at any point.
	Cached struct {
		TotalUnlockedSlots *int // default would be 2, upgrades could get 3, 4, 5
	} `json:"-"`
}

const PlayerDataUpdateTopic = "map_player_data_mgmt"

type PlayerDataUpdateAction int

const (
	PlayerDataUpdate_Update PlayerDataUpdateAction = iota
)

type PlayerDataUpdateMessage struct {
	Action PlayerDataUpdateAction `json:"action"`
	Data   db.MapPlayerData       `json:"data"` // Present for Update
}
