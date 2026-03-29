package world

type UpdateAction string

const (
	ActionCreated   UpdateAction = "created"
	ActionDestroyed UpdateAction = "destroyed"
)

type UpdateMessage struct {
	Action  UpdateAction `json:"action"`
	WorldID string       `json:"worldId"`

	// Create only
	MapID string `json:"mapId"`
	Type  string `json:"type"`

	// Server metadata (always present)
	ServerID        string `json:"serverId"`
	ServerVersion   string `json:"serverVersion"`
	ProtocolVersion int    `json:"protocolVersion"`
}
