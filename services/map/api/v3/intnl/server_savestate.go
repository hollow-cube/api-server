package intnl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/storage"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/util"
	playerServiceV2 "github.com/hollow-cube/hc-services/services/player/api/v2/intnl"
	"github.com/redis/rueidis"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

func (s *server) CreateSaveState(ctx context.Context, request CreateSaveStateRequestObject) (CreateSaveStateResponseObject, error) {
	m, err := s.storageClient.GetMapById(ctx, request.MapId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return CreateSaveState404Response{}, nil
		}

		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	var ss model.SaveState
	ss.Id = common.NewUUID()
	ss.PlayerId = request.PlayerId
	ss.MapId = request.MapId
	if m.PublishedAt != nil {
		ss.Type = model.SaveStateTypePlaying
	} else if m.Verification == model.VerificationPending {
		ss.Type = model.SaveStateTypeVerifying
	} else {
		ss.Type = model.SaveStateTypeEditing
	}
	now := util.CurrentTime()
	ss.Created = now
	ss.LastModified = now
	ss.ProtocolVersion = request.Body.ProtocolVersion
	ss.Completed = false
	ss.PlayTime = 0

	if err = s.storageClient.CreateSaveState(ctx, &ss); err != nil {
		return nil, fmt.Errorf("failed to create save state: %w", err)
	}
	return CreateSaveState201JSONResponse{saveStateToAPI(ss)}, nil
}

func (s *server) GetLatestSaveState(ctx context.Context, request GetLatestSaveStateRequestObject) (GetLatestSaveStateResponseObject, error) {
	m, err := s.storageClient.GetMapById(ctx, request.MapId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return SaveStateNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	var ssType model.SaveStateType
	if request.Params.TypeFilter != nil && *request.Params.TypeFilter != "" {
		ssType = model.SaveStateType(*request.Params.TypeFilter)
	} else if m.PublishedAt != nil {
		ssType = model.SaveStateTypePlaying
	} else if m.Verification == model.VerificationPending {
		ssType = model.SaveStateTypeVerifying
	} else {
		ssType = model.SaveStateTypeEditing
	}

	ss, err := s.storageClient.GetLatestSaveState(ctx, request.MapId, request.PlayerId, ssType)
	if err != nil || ss == nil {
		if ss == nil || errors.Is(err, storage.ErrNotFound) {
			return GetLatestSaveState404Response{}, nil
		}

		return nil, fmt.Errorf("failed to fetch save state: %w", err)
	}

	return GetLatestSaveState200JSONResponse{saveStateToAPI(*ss)}, nil
}

func (s *server) GetBestSaveState(ctx context.Context, request GetBestSaveStateRequestObject) (GetBestSaveStateResponseObject, error) {
	ss, err := s.storageClient.GetBestSaveState(ctx, request.MapId, request.PlayerId)
	if err != nil || ss == nil {
		if errors.Is(err, storage.ErrNotFound) || ss == nil {
			return SaveStateNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to fetch save state: %w", err)
	}

	return GetBestSaveState200JSONResponse{saveStateToAPI(*ss)}, err
}

func (s *server) UpdateSaveState(ctx context.Context, request UpdateSaveStateRequestObject) (UpdateSaveStateResponseObject, error) {
	ss, err := s.storageClient.GetSaveStateById(ctx, request.MapId, request.PlayerId, request.SaveStateId)
	if errors.Is(err, storage.ErrNotFound) {
		if request.Body.Type == nil {
			return SaveStateNotFoundResponse{}, nil
		}

		createdTime := time.Now()
		if request.Body.Playtime != nil {
			createdTime = createdTime.Add(-time.Duration(*request.Body.Playtime) * time.Millisecond)
		}
		ss = &model.SaveState{
			Id:              request.SaveStateId,
			PlayerId:        request.PlayerId,
			MapId:           request.MapId,
			Type:            model.SaveStateType(*request.Body.Type),
			Created:         createdTime,
			LastModified:    time.Now(),
			ProtocolVersion: 769, // Remains our default version
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

	ss.LastModified = util.CurrentTime()

	var changed bool
	if request.Body.Completed != nil {
		ss.Completed = *request.Body.Completed
		changed = true
	}
	if request.Body.Playtime != nil {
		ss.PlayTime = *request.Body.Playtime
		changed = true
	}
	if ss.Type == model.SaveStateTypeEditing {
		if request.Body.EditState != nil {
			ss.EditingState = *request.Body.EditState
			changed = true
		}
	} else if ss.Type == model.SaveStateTypePlaying || ss.Type == model.SaveStateTypeVerifying {
		if request.Body.PlayState != nil {
			ss.PlayingState = *request.Body.PlayState
			changed = true
		}
	}
	if request.Body.DataVersion != nil && *request.Body.DataVersion != ss.DataVersion {
		ss.DataVersion = *request.Body.DataVersion
		changed = true
	}
	if request.Body.ProtocolVersion != nil && *request.Body.ProtocolVersion != ss.ProtocolVersion {
		ss.ProtocolVersion = *request.Body.ProtocolVersion
		changed = true
	}

	if !changed {
		s.log.Infow("no changes to save state", "mapId", request.MapId, "playerId", request.PlayerId)
		return nil, nil
	}

	// If the save state is completed never store the play/edit state of it.
	if ss.Completed {
		ss.EditingState = nil
		ss.PlayingState = nil
	}

	// Get the best savestate to decide if this is the first completion (BEFORE UPDATING)
	_, err = s.storageClient.GetBestSaveStateSinceBeta(ctx, request.MapId, request.PlayerId)
	isFirstCompletion := errors.Is(err, storage.ErrNotFound)
	if !isFirstCompletion && err != nil {
		return nil, fmt.Errorf("failed to fetch best save state: %w", err)
	}

	if err = s.storageClient.UpdateSaveState(ctx, ss); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return SaveStateNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to update save state: %w", err)
	}

	s.log.Infow("updated save state", "mapId", request.MapId, "playerId", request.PlayerId,
		"saveStateId", request.SaveStateId, "completed", ss.Completed, "type", ss.Type)

	var m *model.Map

	// If this is a verification and was just completed, we need to also update the map to verified.
	// todo we need to do this update as a transaction
	// todo random thought, but we should reject any world update message where verification != unverified
	if ss.Type == model.SaveStateTypeVerifying && ss.Completed {
		m, err = s.storageClient.GetMapById(ctx, request.MapId)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch map: %w", err)
		}

		m.Verification = model.VerificationVerified
		// Always set the map protocol version to the version which verified it.
		m.ProtocolVersion = ss.ProtocolVersion
		if err = s.storageClient.UpdateMap(ctx, m); err != nil {
			return nil, fmt.Errorf("failed to update map: %w", err)
		}
	}

	var currentPlacement = -1
	var newPlacement = -1

	// If the map was just completed, we should add this playtime to the leaderboard.
	if ss.Completed && (ss.Type == model.SaveStateTypePlaying || ss.Type == model.SaveStateTypeVerifying) {
		if m == nil {
			m, err = s.storageClient.GetMapById(ctx, request.MapId)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch map: %w", err)
			}
		}

		// This is only relevant for parkour maps
		if m.Settings.Variant == model.Parkour {
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
				ScoreMember(float64(ss.PlayTime), string(common.UUIDToBin(request.PlayerId))).Build()).Error()
			if err != nil {
				//todo i guess this should go to DLQ or something, but we do not stop the request from succeeding in this case.
				zap.S().Errorw("failed to update leaderboard", "mapId", request.MapId,
					"playerId", request.PlayerId, "playTime", ss.PlayTime, "err", err)
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
	if ss.Completed && ss.Type == model.SaveStateTypePlaying {
		if m == nil {
			m, err = s.storageClient.GetMapById(ctx, request.MapId)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch map: %w", err)
			}
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
			if newPlacement < 3 && m.QualityOverride >= 4 {
				raw, err := json.Marshal(map[string]interface{}{
					"type":      "map_top_placement",
					"mapId":     request.MapId,
					"playerId":  request.PlayerId,
					"placement": newPlacement,
				})
				if err != nil {
					zap.S().Errorw("failed to marshal message", "err", err)
				}
				go s.producer.WriteMessages(context.Background(), kafka.Message{
					Topic: "chat_announcements",
					Value: raw,
				})
			}
		}

		go func() {
			svtString := ""
			if m.Settings.SubVariant != nil {
				svtString = string(*m.Settings.SubVariant)
			}
			go s.metrics.Write(&model.MapCompletedEvent{
				PlayerId:   request.PlayerId,
				MapId:      m.Id,
				Variant:    string(m.Settings.Variant),
				SubVariant: svtString,
				Playtime:   ss.PlayTime,
				Difficulty: m.Difficulty().String(),
			})
		}()
	}

	return UpdateSaveState200JSONResponse{resp}, nil
}

func (s *server) DeleteSaveState(ctx context.Context, request DeleteSaveStateRequestObject) (DeleteSaveStateResponseObject, error) {
	err := s.storageClient.DeleteSaveState(ctx, request.MapId, request.PlayerId, request.SaveStateId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return DeleteSaveState404Response{}, nil
		}

		return nil, fmt.Errorf("failed to delete save state: %w", err)
	}

	return DeleteSaveState200Response{}, nil
}

func (s *server) computeMapRewards(_ context.Context, _ *model.Map, _ bool, _ *model.SaveState) (*playerServiceV2.PlayerInventory, map[string]interface{}, error) {
	return &playerServiceV2.PlayerInventory{}, nil, nil
}

func saveStateToAPI(ss model.SaveState) SaveStateDataJSONResponse {
	return SaveStateDataJSONResponse{
		Id:           ss.Id,
		PlayerId:     ss.PlayerId,
		MapId:        ss.MapId,
		Type:         SaveStateType(ss.Type),
		Created:      ss.Created,
		LastModified: ss.LastModified,
		Completed:    ss.Completed,
		Playtime:     ss.PlayTime,

		DataVersion: ss.DataVersion,
		PlayState:   &ss.PlayingState,
		EditState:   &ss.EditingState,
	}
}
