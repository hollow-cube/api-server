package player

import "time"

const (
	SettingKey_Hidden = "is_hidden"
)

type Session struct {
	PlayerId        string    `json:"playerId"`
	CreatedAt       time.Time `json:"createdAt"` // The time the session was created (used to track playtime)
	ProtocolVersion int64     `json:"protocolVersion"`

	ProxyId  string  `json:"proxyId"`  // The proxy the player is currently on.
	ServerId *string `json:"serverId"` // The server the player is currently on. Or missing if in connecting state.

	// If true, the player is hidden from the tab list, /list, in game, etc.
	// Hidden players are also unable to chat publicly.
	Hidden   bool   `json:"hidden"`
	Username string `json:"username"` // The player's username.
	Skin     Skin   `json:"skin"`     // The player's skin. Always present but fields may be empty.

	Presence *Presence `json:"presence"` // Holds the current playing state for the player.
}

type PresenceType string

const (
	// PresenceTypeMapMakerHub is the presence type for the mapmaker hub.
	// The state field is always empty.
	// The map_id field is either "hub" for the main hub, or "backrooms" for the backrooms.
	PresenceTypeMapMakerHub PresenceType = "mapmaker:hub"
	// PresenceTypeMapMakerMap is the presence type for a mapmaker map.
	// The state field is one of "editing", "testing", "verifying", "playing", "spectating"
	// The map_id field is the UUID of the map.
	PresenceTypeMapMakerMap PresenceType = "mapmaker:map"
)

// Presence is a description of what the player is currently doing.
//
// Some examples:
//
//	In the MapMaker hub: 			{type: "mapmaker:hub", state: "", map_id: "hub"}
//	In the MapMaker hub backrooms: 	{type: "mapmaker:hub", state: "", map_id: "backrooms"}
//	In a MapMaker map:   			{type: "mapmaker:map", state: "editing", map_id: "map_uuid_here"}
type Presence struct {
	Type  PresenceType `json:"type"`
	State string       `json:"state"` // Depends on the type, see above.

	InstanceId string `json:"instanceId"` // The server id hosting the player.
	MapId      string `json:"mapId"`      // The ID of the map (in a general sense. In the future this could be Obungus floor id for example)

	StartTime time.Time `json:"startTime"` // The time the player started this presence.
}

type Skin struct {
	Texture   string `json:"texture,omitempty"`
	Signature string `json:"signature,omitempty"`
}
