package transform

import (
	v1 "github.com/hollow-cube/hc-services/services/map/api/v1"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/model"
)

func SaveState2API(ss *model.SaveState) *v1.SaveState {
	return &v1.SaveState{
		Id:           ss.Id,
		PlayerId:     ss.PlayerId,
		MapId:        ss.MapId,
		Type:         v1.SaveStateType(ss.Type),
		Created:      ss.Created,
		LastModified: ss.LastModified,
		Completed:    ss.Completed,
		Playtime:     &ss.PlayTime,

		DataVersion: ss.DataVersion,
		PlayState:   ss.PlayingState,
		EditState:   ss.EditingState,
	}
}
