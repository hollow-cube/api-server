package intnl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/common"
	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

func (s *server) CreateSaveState(ctx context.Context, request CreateSaveStateRequestObject) (CreateSaveStateResponseObject, error) {
	m, err := s.store.GetMapById(ctx, request.MapId)
	if errors.Is(err, mapdb.ErrNoRows) {
		return CreateSaveState404Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	var stateType mapdb.SaveStateType
	if m.PublishedAt != nil {
		stateType = mapdb.SaveStateTypePlaying
	} else if m.Verification != nil && *m.Verification == int64(model.VerificationPending) {
		stateType = mapdb.SaveStateTypeVerifying
	} else {
		stateType = mapdb.SaveStateTypeEditing
	}

	ss, err := s.store.CreateSaveState(ctx, mapdb.CreateSaveStateParams{
		MapID:           request.PlayerId,
		PlayerID:        request.MapId,
		Type:            stateType,
		ProtocolVersion: &request.Body.ProtocolVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create save state: %w", err)
	}
	return CreateSaveState201JSONResponse{hydrateSaveState(ss)}, nil
}

func (s *server) GetLatestSaveState(ctx context.Context, request GetLatestSaveStateRequestObject) (GetLatestSaveStateResponseObject, error) {
	m, err := s.store.GetMapById(ctx, request.MapId)
	if errors.Is(err, mapdb.ErrNoRows) {
		return GetLatestSaveState404Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	var ssType mapdb.SaveStateType
	if request.Params.TypeFilter != nil && *request.Params.TypeFilter != "" {
		ssType = mapdb.SaveStateType(*request.Params.TypeFilter)
	} else if m.PublishedAt != nil {
		ssType = mapdb.SaveStateTypePlaying
	} else if m.Verification != nil && *m.Verification == int64(model.VerificationPending) {
		ssType = mapdb.SaveStateTypeVerifying
	} else {
		ssType = mapdb.SaveStateTypeEditing
	}

	ss, err := s.store.GetLatestSaveState(ctx, request.MapId, request.PlayerId, ssType)
	if errors.Is(err, mapdb.ErrNoRows) {
		return GetLatestSaveState404Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch save state: %w", err)
	} else if ss.Completed {
		// If the latest is completed then a new state should be created instead.
		return GetLatestSaveState404Response{}, nil
	}

	return GetLatestSaveState200JSONResponse{hydrateSaveState(ss)}, nil
}

func (s *server) GetBestSaveState(ctx context.Context, request GetBestSaveStateRequestObject) (GetBestSaveStateResponseObject, error) {
	ss, err := s.store.GetBestSaveState(ctx, request.MapId, request.PlayerId)
	if errors.Is(err, mapdb.ErrNoRows) {
		return SaveStateNotFoundResponse{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch save state: %w", err)
	}

	return GetBestSaveState200JSONResponse{hydrateSaveState(ss)}, err
}

func (s *server) UpdateSaveState(ctx context.Context, request UpdateSaveStateRequestObject) (UpdateSaveStateResponseObject, error) {
	ss, err := s.store.GetSaveState(ctx, request.SaveStateId, request.MapId, request.PlayerId)
	if errors.Is(err, mapdb.ErrNoRows) {
		if request.Body.Type == nil {
			return SaveStateNotFoundResponse{}, nil
		}

		createdTime := time.Now()
		if request.Body.Playtime != nil {
			createdTime = createdTime.Add(-time.Duration(*request.Body.Playtime) * time.Millisecond)
		}
		ss = mapdb.SaveState{
			ID:              request.SaveStateId,
			MapID:           request.MapId,
			PlayerID:        request.PlayerId,
			Type:            mapdb.SaveStateType(*request.Body.Type),
			Created:         createdTime,
			Updated:         time.Now(),
			ProtocolVersion: new(769), // Remains our default version
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch save state: %w", err)
	}

	// Do not allow updating already-completed save states.
	if ss.Completed {
		return UpdateSaveState400JSONResponse{BadRequestJSONResponse{
			Error: "The save state is already completed",
		}}, nil
	}

	var changed bool
	update := mapdb.UpsertSaveStateParams{
		ID:              ss.ID,
		MapID:           ss.MapID,
		PlayerID:        ss.PlayerID,
		Type:            ss.Type,
		Created:         ss.Created,
		Updated:         time.Now(),
		Completed:       ss.Completed,
		Playtime:        ss.Playtime,
		Ticks:           ss.Ticks,
		Score:           ss.Score,
		StateV2:         ss.StateV2,
		DataVersion:     ss.DataVersion,
		ProtocolVersion: ss.ProtocolVersion,
	}

	if request.Body.Completed != nil {
		update.Completed = *request.Body.Completed
		changed = true
	}
	if request.Body.Playtime != nil {
		update.Playtime = *request.Body.Playtime
		changed = true
	}
	if request.Body.Ticks != nil {
		update.Ticks = *request.Body.Ticks
		changed = true
	}
	if request.Body.Score != nil && update.Completed {
		update.Score = request.Body.Score
	}
	if ss.Type == mapdb.SaveStateTypeEditing {
		if request.Body.EditState != nil {
			update.StateV2, err = json.Marshal(request.Body.EditState)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal edit state: %w", err)
			}
			changed = true
		}
	} else if ss.Type == mapdb.SaveStateTypePlaying || ss.Type == mapdb.SaveStateTypeVerifying {
		if request.Body.PlayState != nil {
			update.StateV2, err = json.Marshal(request.Body.PlayState)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal play state: %w", err)
			}
			changed = true
		}
	}
	if request.Body.DataVersion != nil && *request.Body.DataVersion != ss.DataVersion {
		update.DataVersion = *request.Body.DataVersion
		changed = true
	}
	if request.Body.ProtocolVersion != nil && (ss.ProtocolVersion == nil || *request.Body.ProtocolVersion != *ss.ProtocolVersion) {
		update.ProtocolVersion = request.Body.ProtocolVersion
		changed = true
	}

	if !changed {
		s.log.Infow("no changes to save state", "mapId", request.MapId, "playerId", request.PlayerId)
		return nil, nil
	}

	if ss.Playtime == 0 && update.Completed {
		s.log.Warnw("save state being completed with zero playtime", "mapId", request.MapId, "playerId", request.PlayerId, "saveStateId", request.SaveStateId)
	}

	if ss.Playtime > update.Playtime {
		s.log.Warnw("save state playtime being decreased", "mapId", request.MapId, "playerId", request.PlayerId, "saveStateId", request.SaveStateId, "oldPlaytime", ss.Playtime, "newPlaytime", update.Playtime)
	}

	// If the save state is completed never store the play/edit state of it.
	if update.Completed {
		update.StateV2 = []byte("null")
	}

	// Get the best savestate to decide if this is the first completion (BEFORE UPDATING)
	_, err = s.store.GetBestSaveStateSinceBeta(ctx, request.MapId, request.PlayerId)
	isFirstCompletion := errors.Is(err, mapdb.ErrNoRows)
	if !isFirstCompletion && err != nil {
		return nil, fmt.Errorf("failed to fetch best save state: %w", err)
	}

	// If completed without a score set the playtime for backwards compatibility for now.
	if update.Score == nil && update.Completed && (update.Type == mapdb.SaveStateTypePlaying || update.Type == mapdb.SaveStateTypeVerifying) {
		update.Score = new(float64(max(update.Playtime, update.Ticks*50)))
	}

	if err = s.store.UpsertSaveState(ctx, update); err != nil {
		if errors.Is(err, mapdb.ErrNoRows) {
			return SaveStateNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to update save state: %w", err)
	}

	// Async update the map stats, if it fails it doesnt really matter much
	go s.store.UpdateMapStats(context.TODO(), ss.MapID) // todo figure out this context since it's done in the background, the parent context will be cancelled.

	s.log.Infow("updated save state", "mapId", request.MapId, "playerId", request.PlayerId,
		"saveStateId", request.SaveStateId, "completed", update.Completed, "type", update.Type, "playtime", update.Playtime, "score", update.Score)

	// If this is a verification and was just completed, we need to also update the map to verified.
	// todo we need to do this update as a transaction
	// todo random thought, but we should reject any world update message where verification != unverified
	if update.Type == mapdb.SaveStateTypeVerifying && update.Completed {
		newVerification := int64(model.VerificationVerified)
		// Always set the map protocol version to the version which verified it.
		newProtocolVersion := int(*update.ProtocolVersion)
		if err = s.store.UpdateMapVerification(ctx, request.MapId, &newVerification, &newProtocolVersion); err != nil {
			return nil, fmt.Errorf("failed to update map: %w", err)
		}
	}

	var currentPlacement = -1
	var newPlacement = -1

	// If the map was just completed, we should add this playtime to the leaderboard.
	if update.Completed && (update.Type == mapdb.SaveStateTypePlaying || update.Type == mapdb.SaveStateTypeVerifying) {
		m, err := s.store.GetMapById(ctx, request.MapId)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch map: %w", err)
		}

		// This is only relevant for parkour maps
		if m.OptVariant == string(model.Parkour) {
			leaderboardKey := mapLeaderboardKey(request.MapId, "playtime")

			// Fetch their placement before update
			var placement int64
			placement, err = s.redis.Do(ctx, s.redis.B().Zrank().Key(leaderboardKey).Member(string(common.UUIDToBin(request.PlayerId))).Build()).AsInt64()
			if err != nil {
				if !errors.Is(err, rueidis.Nil) {
					return nil, fmt.Errorf("failed to fetch leaderboard placement: %w", err)
				}
			} else {
				currentPlacement = int(placement)
			}

			err = s.redis.Do(ctx, s.redis.B().Zadd().Key(leaderboardKey).Lt().ScoreMember().
				ScoreMember(*update.Score, string(common.UUIDToBin(request.PlayerId))).Build()).Error()
			if err != nil {
				//todo i guess this should go to DLQ or something, but we do not stop the request from succeeding in this case.
				zap.S().Errorw("failed to update leaderboard", "mapId", request.MapId,
					"playerId", request.PlayerId, "score", update.Score, "err", err)
			}

			// Fetch their placement after update
			placement, err = s.redis.Do(ctx, s.redis.B().Zrank().Key(leaderboardKey).Member(string(common.UUIDToBin(request.PlayerId))).Build()).AsInt64()
			if err != nil {
				if !errors.Is(err, rueidis.Nil) {
					return nil, fmt.Errorf("failed to fetch leaderboard placement: %w", err)
				}
			} else {
				newPlacement = int(placement)
			}
		}
	}

	// If the map was just completed (during play), we should compute the rewards and apply them to the player.
	var resp SaveStateUpdateJSONResponse
	if update.Completed && update.Type == mapdb.SaveStateTypePlaying {
		m, err := s.store.GetPublishedMapById(ctx, request.MapId)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch map: %w", err)
		}

		// TODO: Reenable when giving rewards
		//rewards, txMeta, err := s.computeMapRewards(ctx, m, isFirstCompletion, ss)
		//if err != nil {
		//	return nil, fmt.Errorf("failed to compute map rewards: %w", err)
		//}
		//
		//_, psRes, err := s.playerSvc.GivePlayerItems(ctx, ss.PlayerId, &playerServiceV1.GivePlayerItemsRequest{TxMeta: txMeta, Change: rewards})
		//if err != nil {
		//	return nil, fmt.Errorf("failed to give player items: %w", err)
		//}
		//
		//rmPsRes := string(psRes)
		//resp.Rewards = &rmPsRes

		if newPlacement > currentPlacement {
			resp.NewPlacement = &newPlacement

			// Broadcast a message about the higher placement position
			// IF map quality >= outstanding && placement is 1st/2nd/3rd
			if newPlacement < 3 && m.QualityOverride != nil && *m.QualityOverride >= 4 {
				//raw, err := json.Marshal(map[string]interface{}{
				//	"type":      "map_top_placement",
				//	"mapId":     request.MapId,
				//	"playerId":  request.PlayerId,
				//	"placement": newPlacement,
				//})
				//if err != nil {
				//	zap.S().Errorw("failed to marshal message", "err", err)
				//}
				//go s.producer.WriteMessages(context.Background(), kafka.Message{
				//	Topic: kafkafx.TopicChatAnnouncements,
				//	Value: raw,
				//})
			}
		}

		go func() {
			svtString := ""
			if m.OptSubvariant != nil {
				svtString = *m.OptSubvariant
			}
			go s.metrics.Write(&model.MapCompletedEvent{
				PlayerId:   request.PlayerId,
				MapId:      m.ID,
				Variant:    m.OptVariant,
				SubVariant: svtString,
				Playtime:   update.Playtime,
				Score:      update.Score,
				Difficulty: model.MapDifficulty(m.Difficulty).String(),
			})
		}()
	}

	return UpdateSaveState200JSONResponse{resp}, nil
}

func (s *server) DeleteSaveState(ctx context.Context, request DeleteSaveStateRequestObject) (DeleteSaveStateResponseObject, error) {
	deleted, err := s.store.DeleteSaveState(ctx, request.MapId, request.PlayerId, request.SaveStateId)
	if err != nil {
		return nil, fmt.Errorf("failed to delete save state: %w", err)
	}
	if deleted == 0 {
		return DeleteSaveState404Response{}, nil
	}

	return DeleteSaveState200Response{}, nil
}

func hydrateSaveState(ss mapdb.SaveState) SaveStateDataJSONResponse {
	var playingState, editingState *map[string]interface{}
	if ss.Type == mapdb.SaveStateTypePlaying || ss.Type == mapdb.SaveStateTypeVerifying {
		state := map[string]interface{}{}
		err := json.Unmarshal(ss.StateV2, &state)
		if err != nil {
			zap.S().Errorw("failed to unmarshal play state", "err", err)
		}
		playingState = &state
	} else if ss.Type == mapdb.SaveStateTypeEditing {
		state := map[string]interface{}{}
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

	return SaveStateDataJSONResponse{
		Id:           ss.ID,
		PlayerId:     ss.PlayerID,
		MapId:        ss.MapID,
		Type:         SaveStateType(ss.Type),
		Created:      ss.Created,
		LastModified: ss.Updated,
		Completed:    ss.Completed,
		Playtime:     ss.Playtime,
		Ticks:        &ss.Ticks,
		Score:        score,

		DataVersion: ss.DataVersion,
		PlayState:   playingState,
		EditState:   editingState,
	}
}
