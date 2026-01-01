package intnl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/object"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/storage"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/util"
	"github.com/mitchellh/hashstructure/v2"
	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

const mapContestSlot = 1_000_000

var mapContestId = "c9354e33-96c2-414a-9f4a-8c2ff4669086"

func (s *server) CreateMap(ctx context.Context, request CreateMapRequestObject) (CreateMapResponseObject, error) {
	size := mapSizeFromAPI(request.Body.Size)
	m, err := model.CreateDefaultMap(request.Body.Owner, size)
	if err != nil {
		return nil, err
	}

	var contestId *string
	var savePlayer *db.MapPlayerData
	if request.Body.IsOrg {
		m.MType = string(model.TypeOrg)
	} else if request.Body.Slot != nil && *request.Body.Slot == mapContestSlot {
		pd, err := s.store.GetPlayerData(ctx, request.Body.Owner)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch player data: %w", err)
		}

		if pd.ContestSlot != nil {
			return CreateMap400JSONResponse{BadRequestJSONResponse{
				Error: "Only one contest map can be created!",
			}}, nil
		}

		m.OptVariant = string(model.Parkour)
		// Contest maps are always 1200x1200
		m.Size = model.MapSizeColossal
		m.Contest = &mapContestId
		contestId = &mapContestId

		pd.ContestSlot = &m.ID
		savePlayer = &pd
	} else {
		pd, err := s.store.GetPlayerData(ctx, request.Body.Owner)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch player data: %w", err)
		}

		// Ensure they have permission for the size map they are creating
		allowedForSize, err := s.ensurePermForMapSize(ctx, pd.ID, size)
		if err != nil {
			return nil, err
		} else if !allowedForSize {
			return CreateMap400JSONResponse{BadRequestJSONResponse{
				Error: "You have not unlocked the requested map size",
			}}, nil
		}

		// Add to the given slot or a free slot (if available)
		if request.Body.Slot != nil {
			added, err := s.addMapToSlot(ctx, pd, m.ID, *request.Body.Slot)
			if err != nil {
				return nil, fmt.Errorf("failed to add map to slot: %w", err)
			}
			if !added {
				return CreateMap400JSONResponse{BadRequestJSONResponse{
					Error: "The slot is already in use",
				}}, nil
			}
		} else {
			_, ok, err := s.addMapToFreeSlot(ctx, pd, m.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to add map to slot: %w", err)
			}
			if !ok {
				return CreateMap400JSONResponse{BadRequestJSONResponse{
					Error: "You have no free map slots",
				}}, nil
			}
		}
		savePlayer = &pd
	}

	createdMap, err := s.safeWriteMapToDatabase(ctx, *m, savePlayer)
	if err != nil {
		return nil, fmt.Errorf("failed to write map to database: %w", err)
	}

	//todo map created should include the size + maybe generator
	go s.metrics.Write(model.MapCreatedEvent{
		PlayerId: request.Body.Owner,
		Contest:  contestId,
	})

	return CreateMap201JSONResponse{s.hydrateMap(*createdMap)}, nil
}

func (s *server) GetMaps(ctx context.Context, request GetMapsRequestObject) (GetMapsResponseObject, error) {
	raw, err := s.store.MultiGetPublishedMapsById(ctx, strings.Split(request.Params.MapIds, ","))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch maps: %w", err)
	}

	res := make([]MapData, len(raw))
	for i, m := range raw {
		res[i] = MapData(s.hydratePublishedMap(m))
	}
	return GetMaps200JSONResponse{res}, nil
}

func (s *server) SearchMaps(ctx context.Context, request SearchMapsRequestObject) (SearchMapsResponseObject, error) {
	var params db.SearchMapsParams
	if errText := parseSearchQueryParams(&params, request.Params.Params); errText != "" {
		return SearchMaps400JSONResponse{BadRequestJSONResponse{Error: errText}}, nil
	}

	// Try to return cached queries
	cacheKey, shouldCache := createMapSearchCacheKey(&params)
	shouldCache = false
	if shouldCache {
		cachedRaw, err := s.redis.Do(ctx, s.redis.B().Get().Key(cacheKey).Build()).AsBytes()
		if err == nil {
			// We have a cached query, yay! Reparsing this to return is kind of stupid, but oh well what can you do :)
			var cached MapSearchResponse
			if err = json.Unmarshal(cachedRaw, &cached); err == nil {
				return SearchMaps200JSONResponse(cached), nil
			}
			s.log.Errorw("failed to parse cached map search result", "err", err)
		} else if !errors.Is(err, rueidis.Nil) {
			// Something else went wrong.
			s.log.Errorw("failed to fetch cached map search result", "err", err)
		}
	}

	entries, err := s.store.SearchMaps(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query maps: %w", err)
	}
	result := SearchMaps200JSONResponse{
		Page:    params.Page,
		Results: make([]MapData, len(entries)),
	}
	for i, entry := range entries {
		result.Results[i] = MapData(s.hydratePublishedMap(entry.PublishedMap))
		if i == 0 {
			result.PageCount = int(math.Ceil(float64(entry.TotalCount) / float64(params.PageSize)))
		}
	}

	// Cache the result for 5 minutes
	if shouldCache {
		var raw []byte
		if raw, err = json.Marshal(result); err == nil {
			err = s.redis.Do(ctx, s.redis.B().Set().Key(cacheKey).Value(string(raw)).Ex(5*time.Minute).Build()).Error()
		}
		if err != nil {
			s.log.Errorw("failed to cache map search result", "err", err)
		}
	}

	return result, nil
}

func (s *server) GetMapProgressBulk(ctx context.Context, request GetMapProgressBulkRequestObject) (GetMapProgressBulkResponseObject, error) {
	mapIds := strings.Split(request.Params.MapIds, ",")
	entries, err := s.store.GetMultiMapProgress(ctx, request.Params.PlayerId, mapIds)
	if err != nil {
		return nil, err
	}

	result := make([]MapProgressEntry, len(entries))
	for i, entry := range entries {
		progress := None
		if entry.Progress == 1 {
			progress = Started
		} else if entry.Progress == 2 {
			progress = Complete
		}

		result[i] = MapProgressEntry{
			MapId:    entry.MapID,
			Progress: progress,
			Playtime: int(entry.Playtime),
		}
	}

	return GetMapProgressBulk200JSONResponse{GetMapProgressBulkJSONResponse{
		Results: result,
	}}, nil
}

func (s *server) GetMap(ctx context.Context, request GetMapRequestObject) (GetMapResponseObject, error) {
	// todo should switch this to split between getmap and getpublishedmap and change all the dependent places.
	if common.IsUUID(request.MapId) {
		m, err := s.store.GetMapById(ctx, request.MapId)
		if errors.Is(err, db.ErrNoRows) {
			return MapNotFoundResponse{}, nil
		} else if err != nil {
			return nil, fmt.Errorf("failed to fetch map: %w", err)
		}

		// If map is published we need to get that version
		if m.PublishedID != nil {
			pm, err := s.store.GetPublishedMapById(ctx, m.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch published map: %w", err)
			}
			return GetMap200JSONResponse{s.hydratePublishedMap(pm)}, nil
		}

		return GetMap200JSONResponse{s.hydrateMap(m)}, nil
	}

	// Can also search by published id
	pid, err := strconv.Atoi(request.MapId)
	if err != nil {
		return nil, fmt.Errorf("invalid published id: %w", err)
	}
	pm, err := s.store.GetPublishedMapByPublishedId(ctx, &pid)
	if errors.Is(err, db.ErrNoRows) {
		return MapNotFoundResponse{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}
	return GetMap200JSONResponse{s.hydratePublishedMap(pm)}, nil
}

func (s *server) UpdateMap(ctx context.Context, request UpdateMapRequestObject) (UpdateMapResponseObject, error) {
	m, err := s.store.GetMapById(ctx, request.MapId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return MapNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	update := db.UpdateMapParams{
		ID:              m.ID,
		Name:            m.OptName,
		Icon:            m.OptIcon,
		Size:            m.Size,
		Variant:         m.OptVariant,
		Subvariant:      m.OptSubvariant,
		SpawnPoint:      m.OptSpawnPoint,
		Tags:            m.OptTags,
		Ext:             m.Ext,
		Quality:         m.QualityOverride,
		Listed:          m.Listed,
		ProtocolVersion: m.ProtocolVersion,
		OnlySprint:      m.OptOnlySprint,
		NoSprint:        m.OptNoSprint,
		NoJump:          m.OptNoJump,
		NoSneak:         m.OptNoSneak,
		Boat:            m.OptBoat,
		Extra:           m.OptExtra,
	}

	// Update the map
	var changed bool
	if request.Body.ProtocolVersion != nil && *request.Body.ProtocolVersion != 0 && *request.Body.ProtocolVersion != *m.ProtocolVersion {
		update.ProtocolVersion = request.Body.ProtocolVersion
		changed = true
	}
	if request.Body.Name != nil {
		update.Name = request.Body.Name
		changed = true
	}
	if request.Body.Icon != nil {
		update.Icon = request.Body.Icon
		changed = true
	}
	if request.Body.Size != nil {
		size := mapSizeFromAPI(*request.Body.Size)
		if size > model.MapSize__Max {
			return UpdateMap400JSONResponse{BadRequestJSONResponse{
				Error: fmt.Sprintf("invalid map size: ", *request.Body.Size),
			}}, nil
		}
		update.Size = int64(size)
		changed = true
	}
	if request.Body.Variant != nil {
		update.Variant = string(mapVariantFromAPI(*request.Body.Variant))
		changed = true
	}
	if request.Body.Subvariant != nil {
		sv := *request.Body.Subvariant
		if sv == string(model.SubVariantNone) {
			update.Subvariant = nil
		} else {
			variant, ok := model.MapSubVariantTypeMap[model.MapSubVariant(sv)]
			if !ok {
				return UpdateMap400JSONResponse{BadRequestJSONResponse{
					Error: fmt.Sprintf("invalid sub size: ", sv),
				}}, nil
			}
			if string(variant) != update.Variant {
				return UpdateMap400JSONResponse{BadRequestJSONResponse{
					Error: fmt.Sprintf("invalid sub variant for map type: %s and %s", sv, variant),
				}}, nil
			}
			update.Subvariant = &sv
		}
		changed = true
	}
	if request.Body.SpawnPoint != nil {
		update.SpawnPoint = posFromAPI(*request.Body.SpawnPoint)
		changed = true
	}

	//todo ensure there arent any invalid configurations of settings
	if request.Body.Extra != nil && len(*request.Body.Extra) > 0 {
		var extra = make(map[string]interface{})
		_ = json.Unmarshal(update.Extra, &extra)
		for k, v := range *request.Body.Extra {
			switch k {
			case "only_sprint":
				update.OnlySprint = util.Ptr(v.(bool))
			case "no_sprint":
				update.NoSprint = util.Ptr(v.(bool))
			case "no_jump":
				update.NoJump = util.Ptr(v.(bool))
			case "no_sneak":
				update.NoSneak = util.Ptr(v.(bool))
			case "boat":
				update.Boat = util.Ptr(v.(bool))
			default:
				extra[k] = v
			}
		}
		update.Extra, _ = json.Marshal(extra)
		changed = true
	}

	if request.Body.Tags != nil {
		update.Tags = *request.Body.Tags
		changed = true
	}

	if request.Body.NewObjects != nil && len(*request.Body.NewObjects) > 0 {
		if update.Ext.Objects == nil {
			update.Ext.Objects = make(map[string]*db.ObjectData)
		}
		for _, newObject := range *request.Body.NewObjects {
			objectData := &db.ObjectData{
				Id:   newObject.Id,
				Type: newObject.Type,
				Pos: db.Point{
					X: float64(newObject.Pos.X),
					Y: float64(newObject.Pos.Y),
					Z: float64(newObject.Pos.Z),
				},
			}
			if newObject.Data != nil {
				objectData.Data = *newObject.Data
			}
			update.Ext.Objects[newObject.Id] = objectData
		}
		changed = true
	}
	if len(m.Ext.Objects) > 0 && request.Body.RemovedObjects != nil && len(*request.Body.RemovedObjects) > 0 {
		for _, removedObject := range *request.Body.RemovedObjects {
			delete(m.Ext.Objects, removedObject)
		}
		changed = true
	}

	// Post publish bits
	if request.Body.QualityOverride != nil {
		newQuality := int64(mapQualityFromAPI(*request.Body.QualityOverride))
		update.Quality = &newQuality
		changed = true
	}

	// Listing
	if request.Body.Listed != nil {
		update.Listed = *request.Body.Listed
		changed = true
	}

	// If not changed, nothing needs to be rewritten
	if !changed {
		return UpdateMap204Response{}, nil
	}

	// Write back to DB
	if err = s.store.UpdateMap(ctx, update); err != nil {
		return nil, fmt.Errorf("failed to update map: %w", err)
	}

	// If we changed the variant to parkour after publishing, delete any in-progress save states for the map
	if m.PublishedAt != nil && request.Body.Variant != nil && *request.Body.Variant == Parkour {
		err = s.store.Unsafe_DeleteMapSaveStates(ctx, m.ID)
		if err != nil {
			// Not fatal, just error log
			s.log.Errorw("failed to delete save states when map became parkour", "mapId", m.ID, "error", err)
		}
	}

	return UpdateMap204Response{}, nil
}

func (s *server) DeleteMap(ctx context.Context, request DeleteMapRequestObject) (DeleteMapResponseObject, error) {
	// BEGIN HARDCODED SPAWN BEHAVIOR
	// This will be fixed when we have orgs and do not need to hardcode the spawn map
	if request.MapId == model.MapmakerSpawnMapId {
		return DeleteMap400JSONResponse{BadRequestJSONResponse{
			Error: "cannot delete spawn map",
		}}, nil
	}
	// END HARDCODED SPAWN BEHAVIOR

	m, err := s.store.GetMapById(ctx, request.MapId)
	if errors.Is(err, storage.ErrNotFound) {
		return MapNotFoundResponse{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	userId := ctx.Value(ContextKeyUser).(string)

	reasonRequired := m.PublishedID != nil || m.Owner != userId
	if reasonRequired && (request.Body.Reason == nil || *request.Body.Reason == "") {
		return DeleteMap400JSONResponse{BadRequestJSONResponse{
			Error: "reason is required",
		}}, nil
	}

	// Must have admin perms on the map to delete it
	hasPermission, err := s.authzClient.CheckMapAdmin(ctx, request.MapId, userId, authz.NoKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check permission: %w", err)
	}
	if !hasPermission {
		return nil, fmt.Errorf("unauthorized") //todo
	}

	var reason string
	if reasonRequired {
		reason = *request.Body.Reason // Nil checked above
	} else {
		reason = "user_deletion"
	}

	// Delete map from DB first, if the perm delete fails after its not catastrophic
	if err = s.store.DeleteMap(ctx, request.MapId, &userId, &reason); err != nil {
		return nil, fmt.Errorf("failed to delete map: %w", err)
	}
	if err = s.authzClient.DeleteMap(ctx, request.MapId); err != nil {
		return nil, fmt.Errorf("failed to delete map: %w", err)
	}

	// Remove the map from any players slots
	err = s.revokeMapFromSlots(ctx, request.MapId)
	if err != nil {
		return nil, err
	}

	// Send a map delete message for servers to handle
	err = s.writeMapUpdate(&model.MapUpdateMessage{
		Action: model.MapUpdate_Delete,
		ID:     request.MapId,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to write map update: %w", err)
	}

	s.clearCachedSearches(ctx)

	return DeleteMap200Response{}, nil
}

func (s *server) GetMapWorld(ctx context.Context, request GetMapWorldRequestObject) (GetMapWorldResponseObject, error) {
	worldData, err := s.objectClient.Download(ctx, request.MapId)
	if errors.Is(err, object.ErrNotFound) {
		return GetMapWorld204Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch world data: %w", err)
	}

	return GetMapWorld200ApplicationvndHollowcubePolarResponse{MapWorldDataApplicationvndHollowcubePolarResponse{
		Body:          bytes.NewReader(worldData),
		ContentLength: int64(len(worldData)),
	}}, nil
}

func (s *server) UpdateMapWorld(ctx context.Context, request UpdateMapWorldRequestObject) (UpdateMapWorldResponseObject, error) {
	err := s.objectClient.UploadStream(ctx, request.MapId, request.Body)
	return UpdateMapWorld200Response{}, err
}

func (s *server) BeginMapVerification(ctx context.Context, request BeginMapVerificationRequestObject) (BeginMapVerificationResponseObject, error) {
	m, err := s.store.GetMapById(ctx, request.MapId)
	if errors.Is(err, db.ErrNoRows) {
		return MapNotFoundResponse{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	//todo better errors
	if m.PublishedID != nil {
		return BeginMapVerification400JSONResponse{BadRequestJSONResponse{
			Error: "cannot verify a published map",
		}}, nil
	}
	if m.OptVariant == string(model.Building) {
		return BeginMapVerification400JSONResponse{BadRequestJSONResponse{
			Error: "cannot verify a building map",
		}}, nil
	}
	if m.Verification == nil || *m.Verification != int64(model.VerificationUnverified) {
		return BeginMapVerification400JSONResponse{BadRequestJSONResponse{
			Error: "map already being verifified or done verifying",
		}}, nil
	}

	newVerification := int64(model.VerificationPending)
	err = s.store.UpdateMapVerification(ctx, m.ID, &newVerification, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to update map: %w", err)
	}

	return BeginMapVerification200Response{}, nil
}

func (s *server) DeleteMapVerification(ctx context.Context, request DeleteMapVerificationRequestObject) (DeleteMapVerificationResponseObject, error) {
	m, err := s.store.GetMapById(ctx, request.MapId)
	if errors.Is(err, db.ErrNoRows) {
		return MapNotFoundResponse{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	// Delete the leaderboard of the map to wipe the top time
	// Do it first because if it goes without the others it's not a big deal (and it can't be transactional with the others)
	err = s.redis.Do(ctx, s.redis.B().Del().Key(mapLeaderboardKey(m.ID, "playtime")).Build()).Error()
	if err != nil && !errors.Is(err, rueidis.Nil) {
		return nil, fmt.Errorf("failed to delete leaderboard: %w", err)
	}

	// Unset the verification in the database
	err = db.TxNoReturn(ctx, s.store, func(ctx context.Context, tx *db.Store) error {
		newVerification := int64(model.VerificationUnverified)
		if err := s.store.UpdateMapVerification(ctx, m.ID, &newVerification, nil); err != nil {
			return fmt.Errorf("failed to update map: %w", err)
		}

		if err := s.store.DeleteVerifyingStates(ctx, m.ID); err != nil {
			return fmt.Errorf("failed to delete verifying states: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return DeleteMapVerification200Response{}, nil
}

// PublishMap handles verifying that a map may be published, and then publishing it.
// Publishing a map requires that it passes a number of status checks. These checks are all described below,
// and must be implemented here, as well as in the server to provide realtime feedback.
// todo: find a better source of truth for this, perhaps some internal documentation.
//
// 1. The authorizer must have admin permission on the map.
// 2. The map must not be published already.
// 3. The map must have a name.
// 4. The map must have an icon.
// 5. The map must have a world associated (eg must have been edited and saved).
// 6. The map must have a variant.
// 7. If the map is a parkour map, it must be verified.
// todo none of the above checks are implemented
func (s *server) PublishMap(ctx context.Context, request PublishMapRequestObject) (PublishMapResponseObject, error) {
	// BEGIN HARDCODED SPAWN BEHAVIOR
	// This will be fixed when we have orgs and do not need to hardcode the spawn map
	if request.MapId == model.MapmakerSpawnMapId {
		return PublishMap400JSONResponse{BadRequestJSONResponse{
			Error: "cannot publish spawn map",
		}}, nil
	}
	// END HARDCODED SPAWN BEHAVIOR

	m, err := s.store.GetMapById(ctx, request.MapId)
	if errors.Is(err, db.ErrNoRows) {
		return MapNotFoundResponse{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	// If this is an org map, then we need to handle it differently (by sending a webhook)
	if m.MType == string(model.TypeOrg) {
		return PublishMap400JSONResponse{BadRequestJSONResponse{
			Error: "cannot publish org map",
		}}, nil
	}

	// PRECONDITION: World must exist in object storage (sanity check, but needed for metric anyway)
	worldInfo, err := s.objectClient.Stat(ctx, m.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch world info: %w", err)
	}

	// PRECONDITION: Owner must have spent >20m editing the map
	// todo actually implement this (it is currently checked locally before sending request)
	ownerState, err := s.store.GetLatestSaveState(ctx, m.ID, m.Owner, db.SaveStateTypeEditing)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch owner save state: %w", err)
	}

	//todo: check publish preconditions

	// Update the map info with published Id & time
	publishedId, err := s.store.FindNextPublishedId(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to find next published id: %w", err)
	}

	// Make the updates in DB & spicedb as 2pc
	err = db.TxNoReturn(ctx, s.store, func(ctx context.Context, tx *db.Store) error {
		_, err = s.authzClient.PublishMap(ctx, m.ID)
		if err != nil {
			return fmt.Errorf("failed to publish map: %w", err)
		}

		if err = s.store.PublishMap(ctx, m.ID, &publishedId, request.Body.Contest); err != nil {
			return fmt.Errorf("failed to update map: %w", err)
		}

		return nil
	})
	if err != nil {
		// todo this is quite bad, not sure how to roll back the removal of
		zap.S().Errorw("bad thing happened, now we have a published map with no permissions to view it.", "error", err)
		return nil, err
	}

	if err = s.revokeMapFromSlots(ctx, m.ID); err != nil {
		s.log.Errorw("failed to revoke map from slots", "error", err)
		// Non-fatal, still do the other bits here
	}

	s.clearCachedSearches(ctx)

	subVariantStr := ""
	if m.OptSubvariant != nil {
		subVariantStr = *m.OptSubvariant
	}
	go s.metrics.Write(model.MapPublishedEvent{
		PlayerId:       m.Owner,
		MapId:          m.ID,
		PublishedMapId: publishedId,
		MapName:        m.OptName,
		Variant:        m.OptVariant,
		SubVariant:     subVariantStr,
		WorldDataSize:  int(worldInfo.Size),
		OwnerBuildTime: ownerState.Playtime,
		Contest:        m.Contest,
	})

	publishedMap, err := s.store.GetPublishedMapById(ctx, m.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch published map: %w", err)
	}
	return PublishMap200JSONResponse{s.hydratePublishedMap(publishedMap)}, nil
}

func (s *server) ReportMap(ctx context.Context, request ReportMapRequestObject) (ReportMapResponseObject, error) {
	_, err := s.store.GetMapById(ctx, request.MapId)
	if errors.Is(err, db.ErrNoRows) {
		return MapNotFoundResponse{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	reportParams := db.InsertMapReportParams{
		MapID:      request.MapId,
		PlayerID:   request.Body.Reporter,
		Categories: make([]int, len(request.Body.Categories)),
		Comment:    request.Body.Comment,
	}
	for i, category := range request.Body.Categories {
		reportParams.Categories[i] = mapReportCategoryFromAPI(category)
	}

	// Save the report to the database immediately for future lookup
	report, err := s.store.InsertMapReport(ctx, reportParams)
	if err != nil {
		return nil, fmt.Errorf("failed to write report: %w", err)
	}
	s.log.Infow("created map report #"+strconv.Itoa(report.ID), "report", report)

	// Submitting a report always results in disliking the map
	err = s.store.UpsertMapRating(ctx, report.MapID, report.PlayerID, int(model.RatingStateDisliked))
	if err != nil {
		// This is non fatal, just log it
		s.log.Errorw("failed to dislike map during report", "error", err)
	}

	return ReportMap200Response{}, nil
}

func (s *server) GetMapRating(ctx context.Context, request GetMapRatingRequestObject) (GetMapRatingResponseObject, error) {
	rating, err := s.store.GetMapRatingForMapBy(ctx, request.MapId, request.PlayerId)
	if errors.Is(err, db.ErrNoRows) {
		rating.MapID = request.MapId
		rating.PlayerID = request.PlayerId
		rating.Rating = int(model.RatingStateUnrated)
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch map rating: %w", err)
	}

	return GetMapRating200JSONResponse{mapRatingToAPI(rating)}, nil
}

func (s *server) RateMap(ctx context.Context, request RateMapRequestObject) (RateMapResponseObject, error) {
	rating := mapRatingStateFromAPI(request.Body.State)
	if rating < model.RatingState__Min || rating > model.RatingState__Max {
		return RateMap400JSONResponse{BadRequestJSONResponse{
			Error: fmt.Sprintf("invalid rating value: %d", rating),
		}}, nil
	}

	err := s.store.UpsertMapRating(ctx, request.MapId, request.PlayerId, int(rating))
	if err != nil {
		return nil, fmt.Errorf("failed to set map rating: %w", err)
	}

	//todo update map stats

	return RateMap200Response{}, nil
}

var (
	mapSortMap = map[MapSortType]storage.MapSortType{
		Best:      storage.MapSortBest,
		Published: storage.MapSortPublished,
	}
	mapSortOrderMap = map[MapSortOrder]storage.MapSortOrder{
		Asc:  storage.MapSortAsc,
		Desc: storage.MapSortDesc,
	}
)

func parseSearchQueryParams(params *db.SearchMapsParams, req *MapSearchParams) string {
	var err error
	var ok bool
	if req.Page != nil && *req.Page != "" {
		params.Page, err = strconv.Atoi(*req.Page)
		if err != nil {
			return err.Error()
		} else if params.Page < 0 {
			return fmt.Sprintf("invalid page: %d", params.Page)
		}
	}
	if req.PageSize != nil && *req.PageSize != "" {
		params.PageSize, err = strconv.Atoi(*req.PageSize)
		if err != nil {
			return err.Error()
		} else if params.PageSize < 0 || params.PageSize > 100 {
			return fmt.Sprintf("invalid page size: %d", params.PageSize)
		}
	} else {
		params.PageSize = 10
	}

	if req.Sort != nil && *req.Sort != "" {
		params.Sort, ok = mapSortMap[*req.Sort]
		if !ok {
			return fmt.Sprintf("invalid sort value: %s", *req.Sort)
		}
	} else {
		params.Sort = storage.MapSortBest
	}
	if req.SortOrder != nil && *req.SortOrder != "" {
		params.SortOrder, ok = mapSortOrderMap[*req.SortOrder]
		if !ok {
			return fmt.Sprintf("invalid sort order: %s", *req.SortOrder)
		}
	} else {
		params.SortOrder = storage.MapSortDesc
	}

	if req.Parkour != nil && *req.Parkour {
		params.Variants = append(params.Variants, string(model.Parkour))
	}
	if req.Building != nil && *req.Building {
		params.Variants = append(params.Variants, string(model.Building))
	}

	if req.Quality != nil && *req.Quality != "" {
		rawQualities := strings.Split(*req.Quality, ",")
		for _, rawQuality := range rawQualities {
			if rawQuality == "" {
				continue
			}
			quality := mapQualityFromAPI(MapQuality(rawQuality))
			if quality == -1 {
				return fmt.Sprintf("invalid quality: %s", rawQuality)
			}
			params.Quality = append(params.Quality, int(quality))
		}
	}
	if req.Difficulty != nil && *req.Difficulty != "" {
		rawDifficulties := strings.Split(*req.Difficulty, ",")
		for _, rawDifficulty := range rawDifficulties {
			if rawDifficulty == "" {
				continue
			}
			difficulty, ok := mapDifficultyFromAPI(MapDifficulty(rawDifficulty))
			if !ok {
				return fmt.Sprintf("invalid difficulty: %s", rawDifficulty)
			}
			params.Difficulty = append(params.Difficulty, int(difficulty))
		}
	}

	if req.Owner != nil && *req.Owner != "" {
		if !common.IsUUID(*req.Owner) {
			return fmt.Sprintf("invalid owner: %s", *req.Owner)
		}
		params.Owner = req.Owner
	}
	if req.Query != nil && *req.Query != "" {
		params.Name = req.Query
	}
	if req.Contest != nil && *req.Contest != "" {
		params.Contest = req.Contest
	}

	return ""
}

func (s *server) hydrateMap(m db.Map) MapDataJSONResponse {
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

	return MapDataJSONResponse{
		Id:              m.ID,
		Owner:           m.Owner,
		CreatedAt:       m.CreatedAt,
		LastModified:    m.UpdatedAt,
		ProtocolVersion: *m.ProtocolVersion, // todo shouldnt be nullable in db

		Verification: mapVerificationToAPI(m.Verification),
		Settings: MapSettings{
			Name:       util.NilToEmpty(m.OptName), // todo should not be optional in db
			Icon:       util.NilToEmpty(m.OptIcon), // todo should not be optional in db
			Size:       mapSizeToAPI(m.Size),
			Variant:    mapVariantToAPI(m.OptVariant),
			Subvariant: m.OptSubvariant,
			Tags:       m.OptTags,
			SpawnPoint: posToAPI(m.OptSpawnPoint),
			Extra:      extra,
		},

		PublishedId: m.PublishedID,
		PublishedAt: m.PublishedAt,
		Listed:      m.Listed,

		Quality:    mapQualityToAPI(m.QualityOverride),
		Difficulty: Unknown,

		Objects: nil,

		Contest: m.Contest,
	}
}

func (s *server) hydratePublishedMap(m db.PublishedMap) MapDataJSONResponse {
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

	return MapDataJSONResponse{
		Id:              m.ID,
		Owner:           m.Owner,
		CreatedAt:       m.CreatedAt,
		LastModified:    m.UpdatedAt,
		ProtocolVersion: *m.ProtocolVersion, // todo shouldnt be nullable in db

		Verification: mapVerificationToAPI(m.Verification),
		Settings: MapSettings{
			Name:       util.NilToEmpty(m.OptName), // todo should not be optional in db
			Icon:       util.NilToEmpty(m.OptIcon), // todo should not be optional in db
			Size:       mapSizeToAPI(m.Size),
			Variant:    mapVariantToAPI(m.OptVariant),
			Subvariant: m.OptSubvariant,
			Tags:       m.OptTags,
			SpawnPoint: posToAPI(m.OptSpawnPoint),
			Extra:      extra,
		},

		PublishedId: m.PublishedID,
		PublishedAt: m.PublishedAt,
		Listed:      m.Listed,

		Quality:     mapQualityToAPI(m.QualityOverride),
		Difficulty:  mapDifficultyToAPI(m.Difficulty),
		UniquePlays: m.PlayCount,
		ClearRate:   float32(m.ClearRate),
		Likes:       int(m.TotalLikes),

		Objects: nil,

		Contest: m.Contest,
	}
}

func mapSizeToAPI(size int64) MapSize {
	switch size {
	case model.MapSizeNormal:
		return Normal
	case model.MapSizeLarge:
		return Large
	case model.MapSizeMassive:
		return Massive
	case model.MapSizeColossal:
		return Colossal
	case model.MapSizeUnlimited:
		return Unlimited
	case model.MapSizeTall2k:
		return Tall2k
	case model.MapSizeTall4k:
		return Tall4k
	default:
		return Normal
	}
}

func mapSizeFromAPI(size MapSize) int {
	switch size {
	case Normal:
		return model.MapSizeNormal
	case Large:
		return model.MapSizeLarge
	case Massive:
		return model.MapSizeMassive
	case Colossal:
		return model.MapSizeColossal
	case Unlimited:
		return model.MapSizeUnlimited
	case Tall2k:
		return model.MapSizeTall2k
	case Tall4k:
		return model.MapSizeTall4k
	default:
		return model.MapSizeNormal
	}
}

func mapVariantToAPI(variant string) MapVariant {
	return MapVariant(variant)
}

func mapVariantFromAPI(variant MapVariant) model.MapVariant {
	return model.MapVariant(variant)
}

func mapSubVariantToAPI(subVariant *string) *string {
	if subVariant == nil {
		return nil
	}
	svs := string(*subVariant)
	return &svs
}

func mapVerificationToAPI(verification *int64) MapVerification {
	if verification == nil {
		return Unverified
	}
	switch *verification {
	case int64(model.VerificationPending):
		return Pending
	case int64(model.VerificationVerified):
		return Verified
	default:
		return Unverified
	}
}

func mapDifficultyToAPI(difficulty int32) MapDifficulty {
	switch difficulty {
	case int32(model.MapDifficultyEasy):
		return Easy
	case int32(model.MapDifficultyMedium):
		return Medium
	case int32(model.MapDifficultyHard):
		return Hard
	case int32(model.MapDifficultyExpert):
		return Expert
	case int32(model.MapDifficultyNightmare):
		return Nightmare
	default:
		return Unknown
	}
}

func mapDifficultyFromAPI(difficulty MapDifficulty) (model.MapDifficulty, bool) {
	switch difficulty {
	case Unknown:
		return model.MapDifficultyUnknown, true
	case Easy:
		return model.MapDifficultyEasy, true
	case Medium:
		return model.MapDifficultyMedium, true
	case Hard:
		return model.MapDifficultyHard, true
	case Expert:
		return model.MapDifficultyExpert, true
	case Nightmare:
		return model.MapDifficultyNightmare, true
	default:
		return model.MapDifficultyUnknown, false
	}
}

func mapQualityToAPI(quality *int64) MapQuality {
	if quality == nil {
		return MapQualityUnrated
	}
	switch *quality {
	case 1:
		return MapQualityGood
	case 2:
		return MapQualityGreat
	case 3:
		return MapQualityExcellent
	case 4:
		return MapQualityOutstanding
	case 5:
		return MapQualityMasterpiece
	default:
		return MapQualityUnrated
	}
}

func mapQualityFromAPI(quality MapQuality) int8 {
	switch quality {
	case MapQualityUnrated:
		return 0
	case MapQualityGood:
		return 1
	case MapQualityGreat:
		return 2
	case MapQualityExcellent:
		return 3
	case MapQualityOutstanding:
		return 4
	case MapQualityMasterpiece:
		return 5
	default:
		return -1
	}
}

func posToAPI(pos db.Pos) Pos {
	return Pos{
		X:     float32(pos.X),
		Y:     float32(pos.Y),
		Z:     float32(pos.Z),
		Yaw:   float32(pos.Yaw),
		Pitch: float32(pos.Pitch),
	}
}

func posFromAPI(pos Pos) db.Pos {
	return db.Pos{
		X:     float64(pos.X),
		Y:     float64(pos.Y),
		Z:     float64(pos.Z),
		Yaw:   float64(pos.Yaw),
		Pitch: float64(pos.Pitch),
	}
}

func mapRatingToAPI(rating db.MapRating) GetMapRatingJSONResponse {
	return GetMapRatingJSONResponse{
		State:   mapRatingStateToAPI(rating.Rating),
		Comment: rating.Comment,
	}
}

func mapRatingStateToAPI(state int) MapRatingState {
	switch state {
	case int(model.RatingStateLiked):
		return MapRatingStateLiked
	case int(model.RatingStateDisliked):
		return MapRatingStateDisliked
	default:
		return MapRatingStateUnrated
	}
}

func mapRatingStateFromAPI(state MapRatingState) model.RatingState {
	switch state {
	case MapRatingStateLiked:
		return model.RatingStateLiked
	case MapRatingStateDisliked:
		return model.RatingStateDisliked
	default:
		return model.RatingStateUnrated
	}
}

func mapReportCategoryFromAPI(category MapReportCategory) int {
	switch category {
	case Cheated:
		return model.MapReportCheated
	case Discrimination:
		return model.MapReportDiscrimination
	case ExplicitContent:
		return model.MapReportExplicitContent
	case Spam:
		return model.MapReportSpam
	case Dmca:
		return model.MapReportDCMA
	case Troll:
		return model.MapReportTroll
	default:
		return model.MapReportTroll
	}
}

func createMapSearchCacheKey(params *db.SearchMapsParams) (string, bool) {
	if params.Name != nil && *params.Name != "" {
		return "", false // Never cache queries with search text
	}
	hash, err := hashstructure.Hash(params, hashstructure.FormatV2, nil)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("maps:search:%d", hash), true
}
