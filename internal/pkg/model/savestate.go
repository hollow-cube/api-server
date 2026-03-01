package model

import "time"

type SaveStateType string

const (
	SaveStateTypeNone      SaveStateType = ""
	SaveStateTypeEditing   SaveStateType = "editing"
	SaveStateTypePlaying   SaveStateType = "playing"
	SaveStateTypeVerifying SaveStateType = "verifying"
)

type SaveState struct {
	Id              string
	PlayerId        string
	MapId           string
	Type            SaveStateType
	Created         time.Time
	LastModified    time.Time
	ProtocolVersion int
	Completed       bool
	PlayTime        int
	Ticks           int

	DataVersion int // Game data version, used by the Minecraft servers to upgrade.

	// Only one of EditingState and PlayingState is present, depending on the Type
	EditingState map[string]interface{}
	// Only one of EditingState and PlayingState is present, depending on the Type
	PlayingState map[string]interface{}
}
