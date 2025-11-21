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
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/legacy"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model/transform"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/object"
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

	playerData, err := h.storageClient.GetPlayerData(ctx, userId)
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
	m.Type = model.TypeLegacy
	m.Settings.Name = info.Name
	m.Settings.Icon, _ = legacy.ConvertItem(info.DisplayItem)
	m.Settings.Variant = model.Parkour

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
			m.Settings.SpawnPoint = model.Pos{
				X: x, Y: y, Z: z,
				Yaw: yaw, Pitch: pitch,
			}
		} else {
			h.log.Warnw("failed to parse legacy spawn point", "spawnPoint", info.StartLocation)
		}
	} else if spawnPointParts[0] != "" && spawnPointParts[0] != "default" {
		h.log.Warnw("unknown legacy spawn point format", "spawnPoint", info.StartLocation)
	}

	slot, _, err := h.AddMapToFreeSlot(ctx, playerData, m.Id) // Always OK because we checked above that this is fine.
	if err != nil {
		return nil, fmt.Errorf("failed to add slot to free slot: %w", err)
	}

	if err = h.fetchAndMigrateLegacyMapWorld(ctx, m, info); err != nil {
		return nil, err
	}

	err = h.safeWriteMapToDatabase(ctx, m, playerData)
	if err != nil {
		return nil, err
	}

	go h.metrics.Write(model.MapImportedEvent{PlayerId: playerData.Id, Format: "legacy"})

	return &v1.MapWithSlot{
		MapData: *transform.Map2API(m),
		Slot:    slot,
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

func (h *InternalHandler) fetchAndMigrateLegacyMapWorld(ctx context.Context, m *model.Map, info *legacyMapInfo) error {
	mapDataPath := fmt.Sprintf("/%s/%s/%d.polar", info.PlayerID[0:2], info.PlayerID, info.ID)
	zap.S().Infow("trying to download legacy map", "path", mapDataPath)
	worldData, err := h.legacyData.client.Download(ctx, mapDataPath)
	if err != nil {
		if errors.Is(err, object.ErrNotFound) {
			return &commonV1.Error{HTTP: http.StatusNotFound, Code: "legacy_map_not_found", Message: "legacy map not found"}
		}
		return fmt.Errorf("failed to download legacy map: %w", err)
	}

	if err = h.objectClient.Upload(ctx, m.Id, worldData); err != nil {
		return fmt.Errorf("failed to upload map converted: %w", err)
	}

	return nil
}
