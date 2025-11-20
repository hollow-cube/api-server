package transform

import (
	v1 "github.com/hollow-cube/hc-services/services/map/api/v1"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/util"
)

func PlayerData2API(pd *model.PlayerData) *v1.MapPlayerData {
	mapSlots := make([]*string, len(pd.Maps))
	for i, mapId := range pd.Maps {
		s := mapId // bad gross but required copy
		mapSlots[i] = &s
	}
	return &v1.MapPlayerData{
		Id:            pd.Id,
		MapSlots:      mapSlots,
		LastPlayedMap: util.EmptyToNil(pd.LastPlayedMap),
		LastEditedMap: util.EmptyToNil(pd.LastEditedMap),
	}
}
