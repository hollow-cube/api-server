package v4Internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/common"
	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/hollow-cube/api-server/pkg/hog"
	"github.com/hollow-cube/api-server/pkg/ox"
	"go.uber.org/zap"
)

type SaveStateRequest struct {
	MapID    string `path:"mapId"`
	PlayerID string `path:"playerId"`
	StateID  string `path:"stateId"`
}

type GetLatestSaveStateRequest struct {
	MapID    string `path:"mapId"`
	PlayerID string `path:"playerId"`

	Type SaveStateType `query:"type"`
}

// GET /maps/{mapId}/states/{playerId}/latest
func (s *Server) GetLatestSaveState(ctx context.Context, request GetLatestSaveStateRequest) (*SaveState, error) {
	m, err := s.map_(ctx, request.MapID)
	if err != nil {
		return nil, err
	}

	var ssType mapdb.SaveStateType
	if request.Type != "" {
		ssType = mapdb.SaveStateType(request.Type)
	} else if m.PublishedAt != nil {
		ssType = mapdb.SaveStateTypePlaying
	} else if m.Verification != nil && *m.Verification == int64(model.VerificationPending) {
		ssType = mapdb.SaveStateTypeVerifying
	} else {
		ssType = mapdb.SaveStateTypeEditing
	}

	ss, err := s.mapStore.GetLatestSaveState(ctx, request.MapID, request.PlayerID, ssType)
	if errors.Is(err, mapdb.ErrNoRows) {
		return nil, ox.NotFound{}
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch save state: %w", err)
	} else if ss.Completed {
		// If the latest is completed then a new state should be created instead.
		return nil, ox.NotFound{}
	}

	return new(hydrateSaveState(ss)), nil
}

type GetBestSaveStateRequest struct {
	MapID    string `path:"mapId"`
	PlayerID string `path:"playerId"`
}

// GET /maps/{mapId}/states/{playerId}/best
func (s *Server) GetBestSaveState(ctx context.Context, request GetBestSaveStateRequest) (*SaveState, error) {
	ss, err := s.mapStore.GetBestSaveState(ctx, request.MapID, request.PlayerID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return nil, ox.NotFound{}
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch save state: %w", err)
	}

	return new(hydrateSaveState(ss)), nil
}

type UpsertSaveStateRequestBody struct {
	Type            SaveStateType `json:"type"`
	Playtime        int           `json:"playtime"`
	Ticks           int           `json:"ticks"`
	DataVersion     int           `json:"dataVersion"`
	ProtocolVersion int           `json:"protocolVersion"`

	// One of these will be present always

	EditState *map[string]any `json:"editState"`
	PlayState *map[string]any `json:"playState"`

	// Only set when completing a map, otherwise required.

	Completed bool     `json:"completed"`
	Score     *float64 `json:"score"`
}

// PUT /maps/{mapId}/states/{playerId}/{stateId}
func (s *Server) UpsertSaveState(ctx context.Context, request SaveStateRequest, body UpsertSaveStateRequestBody) error {
	if body.Type == "" {
		return ox.BadRequest{}
	}

	m, err := s.map_(ctx, request.MapID)
	if err != nil {
		return err
	}

	ss, err := s.mapStore.GetSaveState(ctx, request.StateID, request.MapID, request.PlayerID)
	if errors.Is(err, mapdb.ErrNoRows) {
		// If not found, set up a default state

		// We subtract the playtime from the creation time to determine when it was actually created.
		// TODO: This isnt right because playtime isnt counted when spectating so we should really just have the
		//       server send us the creation time rather than guess.
		createdTime := time.Now().Add(-time.Duration(body.Playtime) * time.Millisecond)
		ss = mapdb.SaveState{
			ID:              request.StateID,
			MapID:           request.MapID,
			PlayerID:        request.PlayerID,
			Type:            mapdb.SaveStateType(body.Type),
			Created:         createdTime,
			Updated:         time.Now(),
			ProtocolVersion: new(defaultProtocolVersion),
		}
	} else if err != nil {
		return fmt.Errorf("failed to fetch save state: %w", err)
	}

	// Do not allow updating already-completed save states under any circumstance.
	if ss.Completed {
		return ox.Conflict{}
	}

	update := mapdb.UpsertSaveStateParams{
		ID:              ss.ID,
		MapID:           ss.MapID,
		PlayerID:        ss.PlayerID,
		Type:            ss.Type,
		Created:         ss.Created,
		Updated:         time.Now(),
		Completed:       ss.Completed,
		Playtime:        body.Playtime,
		Ticks:           body.Ticks,
		Score:           ss.Score,
		StateV2:         ss.StateV2,
		DataVersion:     body.DataVersion,
		ProtocolVersion: new(body.ProtocolVersion),
	}

	if ss.Type == mapdb.SaveStateTypeEditing {
		if body.EditState != nil {
			update.StateV2, err = json.Marshal(body.EditState)
			if err != nil {
				return fmt.Errorf("failed to marshal edit state: %w", err)
			}
		}
	} else if ss.Type == mapdb.SaveStateTypePlaying || ss.Type == mapdb.SaveStateTypeVerifying {
		if body.PlayState != nil {
			update.StateV2, err = json.Marshal(body.PlayState)
			if err != nil {
				return fmt.Errorf("failed to marshal play state: %w", err)
			}
		}
	}
	if body.Completed {
		update.Completed = true
		update.Score = body.Score
	}

	if ss.Playtime == 0 && update.Completed {
		s.log.Warnw("save state being completed with zero playtime", "mapId", request.MapID,
			"playerId", request.PlayerID, "saveStateId", request.StateID)
	}
	if ss.Playtime > update.Playtime {
		s.log.Warnw("save state playtime being decreased", "mapId", request.MapID, "playerId",
			request.PlayerID, "saveStateId", request.StateID, "oldPlaytime", ss.Playtime, "newPlaytime", update.Playtime)
	}

	// If the save state is completed never store the play/edit state of it.
	// TODO: migrate StateV2 to jsonb, this is dumb
	if update.Completed {
		update.StateV2 = []byte("null")
	}

	// If completed without a score set the playtime for backwards compatibility for now.
	if update.Score == nil && update.Completed && (update.Type == mapdb.SaveStateTypePlaying || update.Type == mapdb.SaveStateTypeVerifying) {
		update.Score = new(float64(max(update.Playtime, update.Ticks*50)))
	}

	s.log.Infow("updated save state", "mapId", request.MapID, "playerId", request.PlayerID,
		"saveStateId", request.StateID, "completed", update.Completed, "type", update.Type, "playtime", update.Playtime, "score", update.Score)

	err = mapdb.TxNoReturn(ctx, s.mapStore, func(ctx context.Context, tx *mapdb.Store) (err error) {
		if err = tx.UpsertSaveState(ctx, update); err != nil {
			return fmt.Errorf("failed to update save state: %w", err)
		}

		// If this is a verification and was just completed, we need to also update the map to verified.
		if update.Type == mapdb.SaveStateTypeVerifying && update.Completed { // Always set the map protocol version to the version which verified it.
			err = tx.UpdateMapVerification(ctx, m.ID, new(int64(model.VerificationVerified)), new(*update.ProtocolVersion))
			if err != nil {
				return fmt.Errorf("failed to update map verification: %w", err)
			}
		}

		return
	})
	if err != nil {
		return err
	}

	// If the map was just completed, we should add this playtime to the leaderboard.
	if update.Completed && m.OptVariant == string(model.Parkour) && (update.Type == mapdb.SaveStateTypePlaying || update.Type == mapdb.SaveStateTypeVerifying) {
		isAsc := m.Leaderboard == nil || m.Leaderboard.Asc
		leaderboardKey := mapLeaderboardKey(m.ID, "playtime")

		// the types on redis commands are kinda weird so we just duplicate the whole thing here.
		if isAsc {
			err = s.redis.Do(ctx, s.redis.B().Zadd().Key(leaderboardKey).Lt().ScoreMember().
				ScoreMember(*update.Score, string(common.UUIDToBin(request.PlayerID))).Build()).Error()
		} else {
			err = s.redis.Do(ctx, s.redis.B().Zadd().Key(leaderboardKey).Gt().ScoreMember().
				ScoreMember(*update.Score, string(common.UUIDToBin(request.PlayerID))).Build()).Error()
		}
		if err != nil {
			//todo i guess this should go to DLQ or something, but we do not stop the request from succeeding in this case.
			zap.S().Errorw("failed to update leaderboard", "mapId", request.MapID,
				"playerId", request.PlayerID, "score", update.Score, "err", err)
		}

		hog.Enqueue(hog.Capture{
			Event:      "map_completed",
			DistinctId: request.PlayerID,
			Properties: hog.NewProperties().
				Set("map_id", m.ID).
				Set("variant", m.OptVariant).
				Set("playtime", update.Playtime).
				Set("score", update.Score),
		})
	}

	// Async update the map stats, if it fails it doesnt really matter much
	// todo figure out this context since it's done in the background, the parent context will be cancelled.
	go s.mapStore.UpdateMapStats(context.TODO(), ss.MapID)

	return nil
}
