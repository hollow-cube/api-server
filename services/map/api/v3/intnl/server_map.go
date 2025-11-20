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
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/discord"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/object"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/storage"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/util"
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
	var pd *model.PlayerData
	if request.Body.IsOrg {
		m.Type = model.TypeOrg
	} else if request.Body.Slot != nil && *request.Body.Slot == mapContestSlot {
		pd, err = s.storageClient.GetPlayerData2(ctx, request.Body.Owner)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch player data: %w", err)
		}

		if pd.ContestSlot != nil {
			return CreateMap400JSONResponse{BadRequestJSONResponse{
				Error: "Only one contest map can be created!",
			}}, nil
		}

		m.Settings.Variant = model.Parkour
		// Contest maps are always 1200x1200
		m.Settings.Size = model.MapSizeColossal
		m.Contest = &mapContestId
		contestId = &mapContestId

		pd.ContestSlot = &m.Id

	} else {
		pd, err = s.storageClient.GetPlayerData(ctx, request.Body.Owner)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch player data: %w", err)
		}

		// Ensure they have permission for the size map they are creating
		allowedForSize, err := s.ensurePermForMapSize(ctx, pd.Id, size)
		if err != nil {
			return nil, err
		} else if !allowedForSize {
			return CreateMap400JSONResponse{BadRequestJSONResponse{
				Error: "You have not unlocked the requested map size",
			}}, nil
		}

		// Add to the given slot or a free slot (if available)
		if request.Body.Slot != nil {
			added, err := s.addMapToSlot(ctx, pd, m.Id, *request.Body.Slot)
			if err != nil {
				return nil, fmt.Errorf("failed to add map to slot: %w", err)
			}
			if !added {
				return CreateMap400JSONResponse{BadRequestJSONResponse{
					Error: "The slot is already in use",
				}}, nil
			}
		} else {
			_, ok, err := s.addMapToFreeSlot(ctx, pd, m.Id)
			if err != nil {
				return nil, fmt.Errorf("failed to add map to slot: %w", err)
			}
			if !ok {
				return CreateMap400JSONResponse{BadRequestJSONResponse{
					Error: "You have no free map slots",
				}}, nil
			}
		}
	}

	if err = s.safeWriteMapToDatabase(ctx, m, pd); err != nil {
		return nil, fmt.Errorf("failed to write map to database: %w", err)
	}

	//todo map created should include the size + maybe generator
	go s.metrics.Write(model.MapCreatedEvent{
		PlayerId: request.Body.Owner,
		Contest:  contestId,
	})

	return CreateMap201JSONResponse{mapToAPI(m)}, nil
}

func (s *server) GetMaps(ctx context.Context, request GetMapsRequestObject) (GetMapsResponseObject, error) {
	mapIds := strings.Split(request.Params.MapIds, ",")

	if len(mapIds) > 50 || len(mapIds) == 0 {
		return GetMaps400JSONResponse{BadRequestJSONResponse{Error: "Can only fetch 1 to 50 maps at a time"}}, nil
	}

	entries, err := s.storageClient.GetMapsByIds(ctx, mapIds)

	if err != nil {
		return nil, err
	}

	results := make([]MapData, len(entries))

	for i, entry := range entries {
		results[i] = MapData(mapToAPI(entry))
	}

	return GetMaps200JSONResponse{results}, nil
}

func (s *server) SearchMaps(ctx context.Context, request SearchMapsRequestObject) (SearchMapsResponseObject, error) {
	var params storage.SearchQueryV3
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

	entries, err := s.storageClient.SearchMapsV3(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query maps: %w", err)
	}
	result := SearchMaps200JSONResponse{
		Page:    params.Page,
		Results: make([]MapData, len(entries)),
	}
	for i, entry := range entries {
		result.Results[i] = MapData(mapToAPI(entry))
	}

	// If this is page 0 we also need to fetch the total count
	if result.Page == 0 {
		result.PageCount, err = s.storageClient.SearchMapsCountV3(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to query maps count: %w", err)
		}

		result.PageCount = int(math.Ceil(float64(result.PageCount) / float64(params.PageSize)))
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
	entries, err := s.storageClient.GetMapProgress(ctx, request.Params.PlayerId, mapIds)
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
			MapId:    entry.MapId,
			Progress: progress,
			Playtime: entry.Playtime,
		}
	}

	return GetMapProgressBulk200JSONResponse{GetMapProgressBulkJSONResponse{
		Results: result,
	}}, nil
}

func (s *server) GetMap(ctx context.Context, request GetMapRequestObject) (GetMapResponseObject, error) {
	var m *model.Map
	var err error
	if common.IsUUID(request.MapId) {
		m, err = s.storageClient.GetMapById(ctx, request.MapId)
	} else {
		//todo add IsPublishedId also
		m, err = s.storageClient.GetMapByPublishedId(ctx, request.MapId)
	}
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return MapNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	return GetMap200JSONResponse{mapToAPI(m)}, nil
}

func (s *server) UpdateMap(ctx context.Context, request UpdateMapRequestObject) (UpdateMapResponseObject, error) {
	m, err := s.storageClient.GetMapById(ctx, request.MapId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return MapNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	// Update the map
	var changed bool
	if request.Body.ProtocolVersion != nil && *request.Body.ProtocolVersion != m.ProtocolVersion {
		m.ProtocolVersion = *request.Body.ProtocolVersion
		changed = true
	}
	if request.Body.Name != nil {
		m.Settings.Name = *request.Body.Name
		changed = true
	}
	if request.Body.Icon != nil {
		m.Settings.Icon = *request.Body.Icon
		changed = true
	}
	if request.Body.Size != nil {
		size := mapSizeFromAPI(*request.Body.Size)
		if size > model.MapSize__Max {
			return UpdateMap400JSONResponse{BadRequestJSONResponse{
				Error: fmt.Sprintf("invalid map size: ", *request.Body.Size),
			}}, nil
		}
		m.Settings.Size = size
		changed = true
	}
	if request.Body.Variant != nil {
		m.Settings.Variant = mapVariantFromAPI(*request.Body.Variant)
		changed = true
	}
	if request.Body.Subvariant != nil {
		sv := model.MapSubVariant(*request.Body.Subvariant)
		if sv == model.SubVariantNone {
			m.Settings.SubVariant = nil
		} else {
			variant, ok := model.MapSubVariantTypeMap[sv]
			if !ok {
				return UpdateMap400JSONResponse{BadRequestJSONResponse{
					Error: fmt.Sprintf("invalid sub size: ", sv),
				}}, nil
			}
			if variant != m.Settings.Variant {
				return UpdateMap400JSONResponse{BadRequestJSONResponse{
					Error: fmt.Sprintf("invalid sub variant for map type: %s and %s", sv, variant),
				}}, nil
			}
			m.Settings.SubVariant = &sv
		}
		changed = true
	}
	if request.Body.SpawnPoint != nil {
		m.Settings.SpawnPoint = posFromAPI(*request.Body.SpawnPoint)
		changed = true
	}

	//todo ensure there arent any invalid configurations of settings
	if request.Body.Extra != nil && len(*request.Body.Extra) > 0 {
		if m.Settings.Extra == nil {
			m.Settings.Extra = make(map[string]interface{})
		}
		for k, v := range *request.Body.Extra {
			switch k {
			case "only_sprint":
				m.Settings.OnlySprint = v.(bool)
			case "no_sprint":
				m.Settings.NoSprint = v.(bool)
			case "no_jump":
				m.Settings.NoJump = v.(bool)
			case "no_sneak":
				m.Settings.NoSneak = v.(bool)
			case "boat":
				m.Settings.Boat = v.(bool)
			default:
				m.Settings.Extra[k] = v
			}
		}
		changed = true
	}

	if request.Body.Tags != nil {
		m.Settings.Tags = *request.Body.Tags
		changed = true
	}

	if request.Body.NewObjects != nil && len(*request.Body.NewObjects) > 0 {
		if m.Objects == nil {
			m.Objects = make(map[string]*model.ObjectData)
		}
		for _, newObject := range *request.Body.NewObjects {
			objectData := &model.ObjectData{
				Id:   newObject.Id,
				Type: newObject.Type,
				Pos: model.Point{
					X: float64(newObject.Pos.X),
					Y: float64(newObject.Pos.Y),
					Z: float64(newObject.Pos.Z),
				},
			}
			if newObject.Data != nil {
				objectData.Data = *newObject.Data
			}
			m.Objects[newObject.Id] = objectData
		}
		changed = true
	}
	if len(m.Objects) > 0 && request.Body.RemovedObjects != nil && len(*request.Body.RemovedObjects) > 0 {
		for _, removedObject := range *request.Body.RemovedObjects {
			delete(m.Objects, removedObject)
		}
		changed = true
	}

	// Post publish bits
	if request.Body.QualityOverride != nil {
		m.QualityOverride = mapQualityFromAPI(*request.Body.QualityOverride)
		changed = true
	}

	// Listing
	if request.Body.Listed != nil {
		m.Listed = *request.Body.Listed
		changed = true
	}

	// If not changed, nothing needs to be rewritten
	if !changed {
		return UpdateMap204Response{}, nil
	}

	// Write back to DB
	if err = s.storageClient.UpdateMap(ctx, m); err != nil {
		return nil, fmt.Errorf("failed to update map: %w", err)
	}

	// If we changed the variant to parkour after publishing, delete any in-progress save states for the map
	if m.PublishedAt != nil && request.Body.Variant != nil && *request.Body.Variant == Parkour {
		err = s.storageClient.SoftDeleteMapSaveStates(ctx, m.Id, true)
		if err != nil {
			// Not fatal, just error log
			// In the future this should be recorded in sentry or something maybe?
			s.log.Errorw("failed to delete save states when map became parkour", "mapId", m.Id, "error", err)
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

	m, err := s.storageClient.GetMapById(ctx, request.MapId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return MapNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	userId := ctx.Value(ContextKeyUser).(string)

	reasonRequired := m.PublishedId != nil || m.Owner != userId
	if reasonRequired && (request.Body.Reason == nil || *request.Body.Reason == "") {
		return DeleteMap400JSONResponse{BadRequestJSONResponse{
			Error: "reason is required",
		}}, nil
	}

	// Must have admin perms on the map to delete it
	hasPermission, err := s.authzClient.CheckMapAdmin(ctx, request.MapId, userId, m.AuthzKey)
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
	if err = s.storageClient.DeleteMapSoft(ctx, request.MapId, userId, reason); err != nil {
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
	m, err := s.storageClient.GetMapById(ctx, request.MapId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return MapNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	//todo better errors
	if m.PublishedId != nil {
		return BeginMapVerification400JSONResponse{BadRequestJSONResponse{
			Error: "cannot verify a published map",
		}}, nil
	}
	if m.Settings.Variant == model.Building {
		return BeginMapVerification400JSONResponse{BadRequestJSONResponse{
			Error: "cannot verify a building map",
		}}, nil
	}
	if m.Verification != model.VerificationUnverified {
		return BeginMapVerification400JSONResponse{BadRequestJSONResponse{
			Error: "map already being verifified or done verifying",
		}}, nil
	}

	m.Verification = model.VerificationPending

	if err := s.storageClient.UpdateMap(ctx, m); err != nil {
		return nil, fmt.Errorf("failed to update map: %w", err)
	}

	return BeginMapVerification200Response{}, nil
}

func (s *server) DeleteMapVerification(ctx context.Context, request DeleteMapVerificationRequestObject) (DeleteMapVerificationResponseObject, error) {
	m, err := s.storageClient.GetMapById(ctx, request.MapId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return MapNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	// Delete the leaderboard of the map to wipe the top time
	// Do it first because if it goes without the others it's not a big deal (and it can't be transactional with the others)
	err = s.redis.Do(ctx, s.redis.B().Del().Key(mapLeaderboardKey(m.Id, "playtime")).Build()).Error()
	if err != nil && !errors.Is(err, rueidis.Nil) {
		return nil, fmt.Errorf("failed to delete leaderboard: %w", err)
	}

	// Unset the verification in the database
	m.Verification = model.VerificationUnverified
	err = s.storageClient.RunTransaction(ctx, func(ctx context.Context) error {
		if err := s.storageClient.UpdateMap(ctx, m); err != nil {
			return fmt.Errorf("failed to update map: %w", err)
		}

		if err := s.storageClient.DeleteVerifyingStates(ctx, m.Id); err != nil {
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

	m, err := s.storageClient.GetMapById(ctx, request.MapId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return MapNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	// If this is an org map, then we need to handle it differently (by sending a webhook)
	if m.Type == model.TypeOrg {
		return PublishMap400JSONResponse{BadRequestJSONResponse{
			Error: "cannot publish org map",
		}}, nil
	}

	// PRECONDITION: World must exist in object storage (sanity check, but needed for metric anyway)
	worldInfo, err := s.objectClient.Stat(ctx, m.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch world info: %w", err)
	}

	// PRECONDITION: Owner must have spent >5m editing the map
	// todo actually implement this (it is currently checked locally before sending request)
	ownerState, err := s.storageClient.GetLatestSaveState(ctx, m.Id, m.Owner, model.SaveStateTypeEditing)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch owner save state: %w", err)
	}

	//todo: check publish preconditions

	// Update the map info with published Id & time
	publishedId, err := s.storageClient.FindNextPublishedId(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to find next published id: %w", err)
	}
	now := time.Now()
	m.PublishedId = &publishedId
	m.PublishedAt = &now
	m.UpdatedAt = now
	if request.Body != nil && request.Body.Contest != nil {
		m.Contest = request.Body.Contest
	}

	// Make the updates in DB & spicedb as 2pc
	err = s.storageClient.RunTransaction(ctx, func(ctx context.Context) error {
		authzKey, err := s.authzClient.PublishMap(ctx, m.Id)
		if err != nil {
			return fmt.Errorf("failed to publish map: %w", err)
		}

		m.AuthzKey = authzKey
		if err = s.storageClient.UpdateMap(ctx, m); err != nil {
			return fmt.Errorf("failed to update map: %w", err)
		}

		return nil
	})
	if err != nil {
		// todo this is quite bad, not sure how to roll back the removal of
		zap.S().Errorw("bad thing happened, now we have a published map with no permissions to view it.", "error", err)
		return nil, err
	}

	if err = s.revokeMapFromSlots(ctx, m.Id); err != nil {
		s.log.Errorw("failed to revoke map from slots", "error", err)
		// Non-fatal, still do the other bits here
	}

	s.clearCachedSearches(ctx)

	subVariantStr := ""
	if m.Settings.SubVariant != nil {
		subVariantStr = string(*m.Settings.SubVariant)
	}
	go s.metrics.Write(model.MapPublishedEvent{
		PlayerId:       m.Owner,
		MapId:          m.Id,
		PublishedMapId: publishedId,
		MapName:        m.Settings.Name,
		Variant:        string(m.Settings.Variant),
		SubVariant:     subVariantStr,
		WorldDataSize:  int(worldInfo.Size),
		OwnerBuildTime: ownerState.PlayTime,
		Contest:        m.Contest,
	})

	return PublishMap200JSONResponse{mapToAPI(m)}, nil
}

func (s *server) ReportMap(ctx context.Context, request ReportMapRequestObject) (ReportMapResponseObject, error) {
	m, err := s.storageClient.GetMapById(ctx, request.MapId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return MapNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	report := &model.MapReport{
		MapId:      request.MapId,
		PlayerId:   request.Body.Reporter,
		Timestamp:  time.Now(),
		Categories: make([]int, len(request.Body.Categories)),
		Comment:    request.Body.Comment,
	}
	for i, category := range request.Body.Categories {
		report.Categories[i] = mapReportCategoryFromAPI(category)
	}

	// Save the report to the database immediately for future lookup
	reportId, err := s.storageClient.WriteReport(ctx, report)
	if err != nil {
		return nil, fmt.Errorf("failed to write report: %w", err)
	}
	s.log.Infow("created map report #"+strconv.Itoa(reportId), "report", report)

	// Submitting a report always results in disliking the map
	err = s.storageClient.UpsertMapRating(ctx, &model.MapRating{
		MapId:    report.MapId,
		PlayerId: report.PlayerId,
		Rating:   model.RatingStateDisliked,
	})
	if err != nil {
		// This is non fatal, just log it
		s.log.Errorw("failed to dislike map during report", "error", err)
	}

	// Fetch the player info for the webhook message
	username, avatar, err := util.GetPlayerInfo(ctx, report.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch player info: %w", err)
	}

	// Build a discord embed to send to the reports channel
	content := "Reported for "
	for i, category := range report.Categories {
		if i > 0 {
			content += ", "
		}
		content += fmt.Sprintf("**%s**", model.ReportCategoryNameMap[category])
	}
	if report.Comment != nil {
		content += fmt.Sprintf(": \"%s\"", *report.Comment)
	}
	fields := []discord.EmbedField{
		{"Map ID", m.Id, true},
		{"Player ID", report.PlayerId, true},
	}
	embed := &discord.Embed{
		Title:       fmt.Sprintf("New Report for %s", m.Settings.Name),
		Description: content,
		Timestamp:   report.Timestamp.Format(time.RFC3339),
		Color:       0xFF0000,
		Fields:      fields,
		Footer: discord.EmbedFooter{
			Text:    fmt.Sprintf("by %s (Report #%d)", username, reportId),
			IconUrl: avatar,
		},
	}

	// Send the webhook
	webhookUrl := "https://discord.com/api/webhooks/1195400776667893802/8gNLuc2_Q0cRx43JQDi9w3N-9SXwd9N_tNsJYEMfVke-oRu257sdX2G8J7rxg1PJHLPa"
	if err = discord.SendWebhookEmbed(ctx, webhookUrl, embed); err != nil {
		s.log.Errorw("failed to send webhook embed", "error", err)
	}

	return ReportMap200Response{}, nil
}

func (s *server) GetMapRating(ctx context.Context, request GetMapRatingRequestObject) (GetMapRatingResponseObject, error) {
	rating, err := s.storageClient.GetMapRating(ctx, request.MapId, request.PlayerId)
	if errors.Is(err, storage.ErrNotFound) {
		rating = &model.MapRating{
			MapId:    request.MapId,
			PlayerId: request.PlayerId,
			Rating:   model.RatingStateUnrated,
		}
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

	err := s.storageClient.UpsertMapRating(ctx, &model.MapRating{
		MapId:    request.MapId,
		PlayerId: request.PlayerId,
		Rating:   rating,
		Comment:  request.Body.Comment,
	})
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

func parseSearchQueryParams(params *storage.SearchQueryV3, req *MapSearchParams) string {
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
		params.Variant = append(params.Variant, model.Parkour)
	}
	if req.Building != nil && *req.Building {
		params.Variant = append(params.Variant, model.Building)
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
			params.Difficulty = append(params.Difficulty, difficulty)
		}
	}

	if req.Owner != nil && *req.Owner != "" {
		if !common.IsUUID(*req.Owner) {
			return fmt.Sprintf("invalid owner: %s", *req.Owner)
		}
		params.Owner = *req.Owner
	}
	if req.Query != nil && *req.Query != "" {
		params.Query = *req.Query
	}
	if req.Contest != nil && *req.Contest != "" {
		params.Contest = *req.Contest
	}

	return ""
}

func mapToAPI(m *model.Map) MapDataJSONResponse {
	extra := make(map[string]interface{})
	if m.Settings.Extra != nil {
		for k, v := range m.Settings.Extra {
			extra[k] = v
		}
	}
	extra["only_sprint"] = m.Settings.OnlySprint
	extra["no_sprint"] = m.Settings.NoSprint
	extra["no_jump"] = m.Settings.NoJump
	extra["no_sneak"] = m.Settings.NoSneak
	extra["boat"] = m.Settings.Boat

	return MapDataJSONResponse{
		Id:              m.Id,
		Owner:           m.Owner,
		CreatedAt:       m.CreatedAt,
		LastModified:    m.UpdatedAt,
		ProtocolVersion: m.ProtocolVersion,

		Verification: mapVerificationToAPI(m.Verification),
		Settings: MapSettings{
			Name:       m.Settings.Name,
			Icon:       m.Settings.Icon,
			Size:       mapSizeToAPI(m.Settings.Size),
			Variant:    mapVariantToAPI(m.Settings.Variant),
			Subvariant: mapSubVariantToAPI(m.Settings.SubVariant),
			Tags:       m.Settings.Tags,
			SpawnPoint: posToAPI(m.Settings.SpawnPoint),
			Extra:      extra,
		},

		PublishedId: m.PublishedId,
		PublishedAt: m.PublishedAt,
		Listed:      m.Listed,

		Quality:     mapQualityToAPI(m.QualityOverride),
		Difficulty:  mapDifficultyToAPI(m.Difficulty()),
		UniquePlays: m.UniquePlays,
		ClearRate:   float32(m.ClearRate),
		Likes:       m.Likes,

		Objects: nil,

		Contest: m.Contest,
	}
}

func mapSizeToAPI(size int) MapSize {
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

func mapVariantToAPI(variant model.MapVariant) MapVariant {
	return MapVariant(variant)
}

func mapVariantFromAPI(variant MapVariant) model.MapVariant {
	return model.MapVariant(variant)
}

func mapSubVariantToAPI(subVariant *model.MapSubVariant) *string {
	if subVariant == nil {
		return nil
	}
	svs := string(*subVariant)
	return &svs
}

func mapVerificationToAPI(verification model.Verification) MapVerification {
	switch verification {
	case model.VerificationPending:
		return Pending
	case model.VerificationVerified:
		return Verified
	default:
		return Unverified
	}
}

func mapDifficultyToAPI(difficulty model.MapDifficulty) MapDifficulty {
	switch difficulty {
	case model.MapDifficultyEasy:
		return Easy
	case model.MapDifficultyMedium:
		return Medium
	case model.MapDifficultyHard:
		return Hard
	case model.MapDifficultyExpert:
		return Expert
	case model.MapDifficultyNightmare:
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

func mapQualityToAPI(quality int8) MapQuality {
	switch quality {
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

func posToAPI(pos model.Pos) Pos {
	return Pos{
		X:     float32(pos.X),
		Y:     float32(pos.Y),
		Z:     float32(pos.Z),
		Pitch: float32(pos.Pitch),
		Yaw:   float32(pos.Yaw),
	}
}

func posFromAPI(pos Pos) model.Pos {
	return model.Pos{
		X:     float64(pos.X),
		Y:     float64(pos.Y),
		Z:     float64(pos.Z),
		Yaw:   float64(pos.Yaw),
		Pitch: float64(pos.Pitch),
	}
}

func mapRatingToAPI(rating *model.MapRating) GetMapRatingJSONResponse {
	return GetMapRatingJSONResponse{
		State:   mapRatingStateToAPI(rating.Rating),
		Comment: rating.Comment,
	}
}

func mapRatingStateToAPI(state model.RatingState) MapRatingState {
	switch state {
	case model.RatingStateLiked:
		return MapRatingStateLiked
	case model.RatingStateDisliked:
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

func createMapSearchCacheKey(params *storage.SearchQueryV3) (string, bool) {
	if params.Query != "" {
		return "", false // Never cache queries with search text
	}
	hash, err := hashstructure.Hash(params, hashstructure.FormatV2, nil)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("maps:search:%d", hash), true
}
