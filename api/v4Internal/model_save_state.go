package v4Internal

import (
	"encoding/json"
	"time"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"go.uber.org/zap"
)

const defaultProtocolVersion = 769

type SaveState struct {
	ID              string        `json:"id"`
	MapID           string        `json:"mapId"`
	PlayerID        string        `json:"playerId"`
	Created         time.Time     `json:"created"`
	LastModified    time.Time     `json:"lastModified"`
	Type            SaveStateType `json:"type"`
	DataVersion     int           `json:"dataVersion"`
	ProtocolVersion int           `json:"protocolVersion"`
	Playtime        int           `json:"playtime"`
	Ticks           int           `json:"ticks"`

	EditState *map[string]any `json:"editState,omitempty"`
	PlayState *map[string]any `json:"playState,omitempty"`

	Completed bool     `json:"completed"`
	Score     *float64 `json:"score,omitempty"`
}

type SaveStateType string

const (
	SaveStateTypeEditing   SaveStateType = "editing"
	SaveStateTypePlaying   SaveStateType = "playing"
	SaveStateTypeVerifying SaveStateType = "verifying"
)

func hydrateSaveState(ss mapdb.SaveState) SaveState {
	var playingState, editingState *map[string]interface{}
	if ss.Type == mapdb.SaveStateTypePlaying || ss.Type == mapdb.SaveStateTypeVerifying {
		state := map[string]any{}
		err := json.Unmarshal(ss.StateV2, &state)
		if err != nil {
			zap.S().Errorw("failed to unmarshal play state", "err", err)
		}
		playingState = &state
	} else if ss.Type == mapdb.SaveStateTypeEditing {
		state := map[string]any{}
		err := json.Unmarshal(ss.StateV2, &state)
		if err != nil {
			zap.S().Errorw("failed to unmarshal edit state", "err", err)
		}
		editingState = &state
	}

	// As a slightly weird compatibility behavior we set the score to the legacy behavior ONLY if
	// the save state is completed without a score. This is so the server doesnt need to worry about
	// the old behavior. Eventually we can fill in all the scores and remove this special case.
	score := ss.Score
	if ss.Completed && ss.Score == nil {
		score = new(float64(max(ss.Playtime, ss.Ticks*50)))
	}

	pvn := defaultProtocolVersion
	if ss.ProtocolVersion != nil {
		pvn = *ss.ProtocolVersion
	}
	return SaveState{
		ID:              ss.ID,
		MapID:           ss.MapID,
		PlayerID:        ss.PlayerID,
		Created:         ss.Created,
		LastModified:    ss.Updated,
		Type:            SaveStateType(ss.Type),
		DataVersion:     ss.DataVersion,
		ProtocolVersion: pvn,
		Playtime:        ss.Playtime,
		Ticks:           ss.Ticks,

		PlayState: playingState,
		EditState: editingState,

		Completed: ss.Completed,
		Score:     score,
	}
}
