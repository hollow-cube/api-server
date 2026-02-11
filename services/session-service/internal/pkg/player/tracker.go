package player

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/posthog"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/tracefx"
	playerService "github.com/hollow-cube/hc-services/services/player-service/api/v2/intnl"
	"github.com/hollow-cube/hc-services/services/session-service/internal/db"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/util"
	"github.com/hollow-cube/hc-services/services/session-service/pkg/kafkaModel"
	"github.com/jackc/pgx/v5"
	"github.com/redis/rueidis"
	"github.com/redis/rueidis/rueidislock"
	"github.com/segmentio/kafka-go"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

// Tracker keeps track of the currently online players and their sessions/presences.
type Tracker struct {
	queries  *db.Queries
	producer kafkafx.SyncProducer

	countReportCtx    context.Context
	countReportCancel context.CancelFunc
	countReportLocker rueidislock.Locker
}

type TrackerParams struct {
	fx.In

	Redis    rueidis.Client
	Queries  *db.Queries
	Producer kafkafx.SyncProducer
}

func NewTracker(p TrackerParams) (*Tracker, error) {
	reportCtx, reportCancel := context.WithCancel(context.Background())
	locker, err := rueidislock.NewLocker(rueidislock.LockerOption{
		ClientBuilder: func(_ rueidis.ClientOption) (rueidis.Client, error) {
			return p.Redis, nil
		},
		KeyMajority:    1,
		NoLoopTracking: true,
		KeyPrefix:      "sess:",
	})
	if err != nil {
		reportCancel()
		return nil, fmt.Errorf("failed to create locker: %w", err)
	}

	return &Tracker{
		queries:           p.Queries,
		producer:          p.Producer,
		countReportCtx:    reportCtx,
		countReportCancel: reportCancel,
		countReportLocker: locker,
	}, nil
}

func (t *Tracker) Start(_ context.Context) error {
	go t.playerCountReportLoop()
	return nil
}

func (t *Tracker) Stop(_ context.Context) error {
	t.countReportCancel()
	t.countReportLocker.Close()
	return nil
}

func (t *Tracker) GetSession(ctx context.Context, playerId string) (*db.PlayerSession, error) {
	s, err := t.queries.GetPlayerSession(ctx, playerId)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

func (t *Tracker) GetAllSessions(ctx context.Context) ([]*db.PlayerSession, error) {
	return t.queries.ListPlayerSessions(ctx)
}

func (t *Tracker) CreateSession(
	ctx context.Context, proxyId string, pd *playerService.PlayerData,
	skinTexture, skinSignature string, connectedHost *string,
	playerIP string, protocolVersion int, version string,
) (*db.PlayerSession, error) {
	isHidden, _ := pd.Settings[kafkaModel.SettingKey_Hidden].(bool)

	// Upsert the session into the table
	s, err := t.queries.UpsertPlayerSession(ctx, db.UpsertPlayerSessionParams{
		PlayerID:        pd.Id,
		ProxyID:         proxyId,
		Hidden:          isHidden,
		Username:        util.Pointer(pd.Username),
		SkinTexture:     skinTexture,
		SkinSignature:   skinSignature,
		ProtocolVersion: int32(protocolVersion),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to record session: %w", err)
	}

	go t.sendSessionUpdate(ctx, kafkaModel.SessionUpdateMessage{
		Action:   kafkaModel.Session_Create,
		PlayerId: s.PlayerID,
		Session: &kafkaModel.Session{
			PlayerId:        s.PlayerID,
			CreatedAt:       s.CreatedAt,
			ProxyId:         s.ProxyID,
			Hidden:          s.Hidden,
			Username:        *s.Username,
			ProtocolVersion: int64(protocolVersion),
			Skin: kafkaModel.Skin{
				Texture:   s.SkinTexture,
				Signature: s.SkinSignature,
			},
		},
	})

	properties := posthog.NewProperties()

	var isHypercube bool
	if pd.HypercubeUntil != nil {
		isHypercube = pd.HypercubeUntil.After(time.Now())
	}

	if connectedHost != nil {
		properties.
			Set("$set", posthog.NewProperties().
				Set("username", s.Username).
				Set("last_host", *connectedHost).
				Set("last_protocol_version", version).
				Set("is_hypercube", isHypercube).
				Set("$ip", playerIP)).
			Set("$set_once", posthog.NewProperties().Set("initial_host", *connectedHost))
	} else {
		properties.Set("$set", posthog.NewProperties().
			Set("username", s.Username).
			Set("last_protocol_version", version).
			Set("is_hypercube", isHypercube).
			Set("$ip", playerIP))
	}

	posthog.Enqueue(posthog.Capture{
		Event:      "session_start",
		DistinctId: pd.Id,
		Properties: properties.
			Set("protocol_version", version).
			Set("$ip", playerIP),
	})

	return s, nil
}

func (t *Tracker) DeleteSession(ctx context.Context, playerId string) (sessionLength int, err error) {
	s, err := t.queries.DeletePlayerSession(ctx, playerId)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil // Player session did not exist, do nothing for now.
	} else if err != nil {
		return 0, fmt.Errorf("failed to delete session: %w", err)
	}

	sessionLength = int(time.Since(s.CreatedAt).Milliseconds())
	go t.sendSessionUpdate(ctx, kafkaModel.SessionUpdateMessage{
		Action:   kafkaModel.Session_Delete,
		PlayerId: playerId,
	})
	posthog.Enqueue(posthog.Capture{
		Event:      "session_end",
		DistinctId: playerId,
		Properties: posthog.NewProperties().
			Set("length", sessionLength).
			Set("$geoip_disable", true),
	})

	return sessionLength, nil
}

func (t *Tracker) TransferSession(ctx context.Context, playerId string, newPresence *kafkaModel.Presence) (*db.PlayerSession, bool, error) {
	if newPresence == nil || newPresence.Type == "" {
		panic("newPresence must not be nil") // Sanity, but would get us into a bad state if set.
	}

	s, err := t.GetSession(ctx, playerId)
	if err != nil {
		return nil, false, err
	} else if s == nil {
		return nil, false, nil // They don't have a session currently.
	}

	isFirstPresence := s.PType == nil
	if s.PType != nil {
		// Record old presence
		length := time.Since(*s.PStartTime).Milliseconds()
		posthog.Enqueue(posthog.Capture{
			Event:      "presence_end",
			DistinctId: playerId,
			Properties: posthog.NewProperties().
				Set("type", s.PType).
				Set("state", s.PState).
				Set("map_id", s.PMapID).
				Set("length", length).
				Set("$geoip_disable", true),
		})
	}

	s.ServerID = &newPresence.InstanceId
	s.PType = util.Pointer(string(newPresence.Type))
	s.PState = &newPresence.State
	s.PInstanceID = &newPresence.InstanceId
	s.PMapID = &newPresence.MapId
	s.PStartTime = &newPresence.StartTime

	if err = t.UpdateSessionWithMetadata(ctx, s, map[string]interface{}{}); err != nil {
		return nil, false, err
	}

	posthog.Enqueue(posthog.Capture{
		Event:      "presence_start",
		DistinctId: playerId,
		Properties: posthog.NewProperties().
			Set("state", newPresence.State).
			Set("map_id", newPresence.MapId).
			Set("$geoip_disable", true),
	})

	return s, isFirstPresence, nil
}

func (t *Tracker) UpdateSessionWithMetadata(ctx context.Context, s *db.PlayerSession, metadata map[string]interface{}) error {
	if s == nil {
		return fmt.Errorf("session must not be nil")
	}

	if _, err := t.queries.UpsertPlayerSession(ctx, db.UpsertPlayerSessionParams{
		PlayerID:        s.PlayerID,
		ProxyID:         s.ProxyID,
		Hidden:          s.Hidden,
		Username:        s.Username,
		SkinTexture:     s.SkinTexture,
		SkinSignature:   s.SkinSignature,
		ProtocolVersion: s.ProtocolVersion,
		PType:           s.PType,
		PState:          s.PState,
		PInstanceID:     s.PInstanceID,
		PMapID:          s.PMapID,
		PStartTime:      s.PStartTime,
	}); err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	var presence *kafkaModel.Presence
	if s.PType != nil {
		presence = &kafkaModel.Presence{
			Type:       kafkaModel.PresenceType(*s.PType),
			State:      *s.PState,
			InstanceId: *s.PInstanceID,
			MapId:      *s.PMapID,
			StartTime:  *s.PStartTime,
		}
	}

	go t.sendSessionUpdate(ctx, kafkaModel.SessionUpdateMessage{
		Action:   kafkaModel.Session_Update,
		PlayerId: s.PlayerID,
		Session: &kafkaModel.Session{
			PlayerId:        s.PlayerID,
			CreatedAt:       s.CreatedAt,
			ProxyId:         s.ProxyID,
			Hidden:          s.Hidden,
			Username:        *s.Username,
			ProtocolVersion: int64(s.ProtocolVersion),
			Skin: kafkaModel.Skin{
				Texture:   s.SkinTexture,
				Signature: s.SkinSignature,
			},
			Presence: presence,
		},
		Metadata: metadata,
	})

	return nil
}

func (t *Tracker) sendSessionUpdate(ctx context.Context, msg kafkaModel.SessionUpdateMessage) {
	msgContent, err := json.Marshal(msg)
	if err != nil {
		zap.S().Errorw("failed to marshal session update message", "error", err)
		return
	}

	zap.S().Infow("sending session update", "session", msgContent)

	// Use a new context to avoid inheriting a canceled request context
	writeCtx, cancel := context.WithTimeout(tracefx.NewCtxWithTraceCtx(ctx), 15*time.Second)
	defer cancel()

	err = t.producer.WriteMessages(writeCtx, kafka.Message{
		Topic: kafkafx.TopicSessionUpdates,
		Key:   []byte(msg.PlayerId),
		Value: msgContent,
	})
	if err != nil {
		zap.S().Errorw("failed to send session update message", "error", err)
	}
}
