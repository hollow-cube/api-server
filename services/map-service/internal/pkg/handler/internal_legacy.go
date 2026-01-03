package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	lru "github.com/hashicorp/golang-lru"
	commonV1 "github.com/hollow-cube/hc-services/libraries/common/pkg/api"
	v1 "github.com/hollow-cube/hc-services/services/map-service/api/v1"
	v3 "github.com/hollow-cube/hc-services/services/map-service/api/v3/intnl"
	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/legacy"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/object"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/util"
	"go.uber.org/zap"
)

type legacyMapInfo struct {
	ID            int    `json:"id"`
	PlayerID      string `json:"uuid"`
	Name          string `json:"name"`
	DisplayItem   string `json:"displayitem"`
	StartLocation string `json:"startlocation"`
}

type legacyDataCache struct {
	client object.Client
	infos  *lru.Cache // map[string]map[string]*legacyMapInfo
}

func (h *InternalHandler) GetLegacyMaps(ctx context.Context, playerId string) ([]*v1.GetLegacyMapsResponseItem, error) {
	//todo permissions

	allInfos, err := h.legacyData.GetAllLegacyMaps(ctx, playerId)
	if err != nil {
		return nil, err
	}

	maps := make([]*v1.GetLegacyMapsResponseItem, 0, len(allInfos))
	for _, info := range allInfos {
		maps = append(maps, &v1.GetLegacyMapsResponseItem{
			Id:   strconv.FormatInt(int64(info.ID), 10),
			Name: info.Name,
		})
	}

	return maps, nil
}

func (h *InternalHandler) ImportLegacyMap(ctx context.Context, playerId string, legacyMapId string) (*v1.MapWithSlot, error) {
	userId := ctx.Value(v1.ContextKeyUser).(string)

	//todo permissions

	playerData, err := h.store.GetPlayerData(ctx, userId)
	if err != nil {
		return nil, err
	}

	hasFreeSlot, err := h.HasFreeMapSlot(ctx, playerData)
	if err != nil {
		return nil, fmt.Errorf("failed to check free slots: %w", err)
	}
	if !hasFreeSlot {
		return nil, &commonV1.Error{HTTP: http.StatusBadRequest, Code: "no_slot_available", Message: "You have no free map slots"}
	}

	// Find the legacy map details
	info, err := h.legacyData.GetLegacyMap(ctx, playerId, legacyMapId)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, &commonV1.Error{HTTP: http.StatusNotFound, Code: "legacy_map_not_found", Message: "legacy map not found"}
	}

	// Create the map
	m, err := model.CreateDefaultMap(userId, 0)
	if err != nil {
		return nil, err
	}
	m.MType = string(model.TypeLegacy)
	m.Column4 = info.Name
	m.Column5, _ = legacy.ConvertItem(info.DisplayItem)
	m.OptVariant = string(model.Parkour)

	spawnPointParts := strings.Split(info.StartLocation, ":")
	if len(spawnPointParts) == 6 {
		spawnPointParts = spawnPointParts[1:]
	}
	if len(spawnPointParts) == 5 {
		x, xerr := strconv.ParseFloat(spawnPointParts[0], 64)
		y, yerr := strconv.ParseFloat(spawnPointParts[1], 64)
		z, zerr := strconv.ParseFloat(spawnPointParts[2], 64)
		yaw, yawerr := strconv.ParseFloat(spawnPointParts[3], 64)
		pitch, pitcherr := strconv.ParseFloat(spawnPointParts[4], 64)

		if xerr == nil && yerr == nil && zerr == nil && yawerr == nil && pitcherr == nil {
			m.OptSpawnPoint = db.Pos{
				X: x, Y: y, Z: z,
				Yaw: yaw, Pitch: pitch,
			}
		} else {
			h.log.Warnw("failed to parse legacy spawn point", "spawnPoint", info.StartLocation)
		}
	} else if spawnPointParts[0] != "" && spawnPointParts[0] != "default" {
		h.log.Warnw("unknown legacy spawn point format", "spawnPoint", info.StartLocation)
	}

	slot, _, err := h.AddMapToFreeSlot(ctx, playerData, m.ID) // Always OK because we checked above that this is fine.
	if err != nil {
		return nil, fmt.Errorf("failed to add slot to free slot: %w", err)
	}

	if err = h.fetchAndMigrateLegacyMapWorld(ctx, m.ID, info); err != nil {
		return nil, err
	}

	//created, err := h.safeWriteMapToDatabase(ctx, *m, &playerData)
	//if err != nil {
	//	return nil, err
	//}

	go h.metrics.Write(model.MapImportedEvent{PlayerId: playerData.ID, Format: "legacy"})

	return &v1.MapWithSlot{
		//MapData: hydrateMap(created),
		Slot: slot,
	}, nil
}

func (h *InternalHandler) GetLegacyMapWorld(ctx context.Context, playerId string, legacyMapId string) (*v1.MapWorldData, error) {

	//todo format option, permissions

	mapDataPath := fmt.Sprintf("/%s/%s/%s.zip", playerId[0:2], playerId, legacyMapId)
	zap.S().Infow("trying to download legacy map", "path", mapDataPath)
	data, err := h.legacyData.client.DownloadStream(ctx, mapDataPath)
	if err != nil {
		if errors.Is(err, object.ErrNotFound) {
			return nil, &commonV1.Error{HTTP: http.StatusNotFound, Code: "legacy_map_not_found", Message: "legacy map not found"}
		}
		return nil, fmt.Errorf("failed to download legacy map: %w", err)
	}
	defer data.Close()

	rawWorldData, err := io.ReadAll(data)
	if err != nil {
		return nil, fmt.Errorf("failed to read legacy map: %w", err)
	}

	return &v1.MapWorldData{Anvil18: rawWorldData}, nil
}

func (l *legacyDataCache) GetAllLegacyMaps(ctx context.Context, playerId string) (map[string]*legacyMapInfo, error) {
	var infos map[string]*legacyMapInfo
	if existing, ok := l.infos.Get(playerId); ok {
		infos = existing.(map[string]*legacyMapInfo)
	} else {
		zap.S().Infof("fetching legacy map info for player %s", playerId)
		infoDocPath := fmt.Sprintf("%s/%s/data.json", playerId[0:2], playerId)
		infoDocRaw, err := l.client.Download(ctx, infoDocPath)
		if err != nil {
			if errors.Is(err, object.ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}

		err = json.Unmarshal(infoDocRaw, &infos)
		if err != nil {
			return nil, err
		}

		l.infos.Add(playerId, infos)
	}

	return infos, nil
}

func (l *legacyDataCache) GetLegacyMap(ctx context.Context, playerId, legacyMapId string) (*legacyMapInfo, error) {
	infos, err := l.GetAllLegacyMaps(ctx, playerId)
	if err != nil {
		return nil, err
	}

	if infos == nil {
		return nil, nil
	}
	return infos[legacyMapId], nil
}

func (h *InternalHandler) fetchAndMigrateLegacyMapWorld(ctx context.Context, mapId string, info *legacyMapInfo) error {
	mapDataPath := fmt.Sprintf("/%s/%s/%d.polar", info.PlayerID[0:2], info.PlayerID, info.ID)
	zap.S().Infow("trying to download legacy map", "path", mapDataPath)
	worldData, err := h.legacyData.client.Download(ctx, mapDataPath)
	if err != nil {
		if errors.Is(err, object.ErrNotFound) {
			return &commonV1.Error{HTTP: http.StatusNotFound, Code: "legacy_map_not_found", Message: "legacy map not found"}
		}
		return fmt.Errorf("failed to download legacy map: %w", err)
	}

	if err = h.objectClient.Upload(ctx, mapId, worldData); err != nil {
		return fmt.Errorf("failed to upload map converted: %w", err)
	}

	return nil
}

func hydrateMap(m db.Map) v3.MapData {
	extra := make(map[string]interface{})
	if m.OptExtra != nil {
		_ = json.Unmarshal(m.OptExtra, &extra)
	}
	if m.OptOnlySprint != nil && *m.OptOnlySprint {
		extra["only_sprint"] = true
	}
	if m.OptNoSprint != nil && *m.OptNoSprint {
		extra["no_sprint"] = true
	}
	if m.OptNoJump != nil && *m.OptNoJump {
		extra["no_jump"] = true
	}
	if m.OptNoSneak != nil && *m.OptNoSneak {
		extra["no_sneak"] = true
	}
	if m.OptBoat != nil && *m.OptBoat {
		extra["boat"] = true
	}

	return v3.MapData{
		Id:              m.ID,
		Owner:           m.Owner,
		CreatedAt:       m.CreatedAt,
		LastModified:    m.UpdatedAt,
		ProtocolVersion: *m.ProtocolVersion, // todo shouldnt be nullable in db

		Verification: v3.Unverified,
		Settings: v3.MapSettings{
			Name:       util.NilToEmpty(m.OptName), // todo should not be optional in db
			Icon:       util.NilToEmpty(m.OptIcon), // todo should not be optional in db
			Size:       mapSizeToAPI(m.Size),
			Variant:    v3.Parkour,
			Subvariant: m.OptSubvariant,
			Tags:       m.OptTags,
			SpawnPoint: posToAPI(m.OptSpawnPoint),
			Extra:      extra,
		},

		PublishedId: m.PublishedID,
		PublishedAt: m.PublishedAt,
		Listed:      m.Listed,

		Quality:    v3.MapQualityUnrated,
		Difficulty: v3.Unknown,

		Objects: nil,

		Contest: m.Contest,
	}
}

func mapSizeToAPI(size int64) v3.MapSize {
	switch size {
	case model.MapSizeNormal:
		return v3.Normal
	case model.MapSizeLarge:
		return v3.Large
	case model.MapSizeMassive:
		return v3.Massive
	case model.MapSizeColossal:
		return v3.Colossal
	case model.MapSizeUnlimited:
		return v3.Unlimited
	case model.MapSizeTall2k:
		return v3.Tall2k
	case model.MapSizeTall4k:
		return v3.Tall4k
	default:
		return v3.Normal
	}
}

func posToAPI(pos db.Pos) v3.Pos {
	return v3.Pos{
		X:     float32(pos.X),
		Y:     float32(pos.Y),
		Z:     float32(pos.Z),
		Yaw:   float32(pos.Yaw),
		Pitch: float32(pos.Pitch),
	}
}
