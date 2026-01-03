package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/player"
	"github.com/redis/rueidis"
	"github.com/segmentio/kafka-go"
	"github.com/vmihailenco/msgpack/v5"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type InviteManager struct {
	log *zap.SugaredLogger

	redis         rueidis.Client
	producer      kafkafx.SyncProducer
	playerTracker *player.Tracker
}

const (
	inviteTopic             = "invites"
	inviteAcceptRejectTopic = "invite-accept-reject"
)

var (
	inviteExpiryTime  = 5 * time.Minute
	requestExpiryTime = 1 * time.Minute
)

type InviteManagerParams struct {
	fx.In

	Log *zap.SugaredLogger

	Redis         rueidis.Client
	Producer      kafkafx.SyncProducer
	PlayerTracker *player.Tracker
}

func NewInviteManager(params InviteManagerParams) *InviteManager {
	return &InviteManager{
		log: params.Log,

		redis:         params.Redis,
		producer:      params.Producer,
		playerTracker: params.PlayerTracker,
	}
}

type InviteError struct {
	Code    string
	Message string
}

func (e *InviteError) Error() string {
	return e.Code
}

var (
	ErrInviteExists  = &InviteError{Code: "invite_exists", Message: "An invite already exists!"}
	ErrRequestExists = &InviteError{Code: "request_exists", Message: "A request already exists!"}
	ErrAlreadyOnMap  = &InviteError{Code: "already_on_map", Message: "You are already on the map!"}
)

type InviteError2 struct {
	Code    int
	Message string
}

func (e *InviteError2) Error() string {
	return e.Message
}

var (
	ErrInviteNotFound       = &InviteError2{Code: 0, Message: "invite not found"}
	ErrRequestNotFound      = &InviteError2{Code: 1, Message: "request not found"}
	ErrInviteSenderLeftMap  = &InviteError2{Code: 5, Message: "invite sender left map"}
	ErrInviteSenderOffline  = &InviteError2{Code: 6, Message: "invite sender offline"}
	ErrRequestTargetLeftMap = &InviteError2{Code: 7, Message: "request target left map"}
	ErrRequestTargetOffline = &InviteError2{Code: 8, Message: "request target offline"}
	ErrNoInvitesOrRequests  = &InviteError2{Code: 9, Message: "no invites or requests found"}
)

func (i *InviteManager) Create(ctx context.Context, invite *model.MapInvite) error {
	if invite.Type != model.InviteTypeInvite && invite.Type != model.InviteTypeRequest {
		return fmt.Errorf("invalid invite type: %s", invite.Type)
	}

	senderSession, err := i.playerTracker.GetSession(ctx, invite.SenderId)
	if err != nil {
		return fmt.Errorf("failed to get sender session: %w", err)
	}

	targetSession, err := i.playerTracker.GetSession(ctx, invite.RecipientId)
	if err != nil {
		return fmt.Errorf("failed to get recipient session: %w", err)
	}

	if senderSession.PMapID != nil && targetSession.PMapID != nil && *senderSession.PMapID == *targetSession.PMapID {
		return ErrAlreadyOnMap
	}

	raw, err := msgpack.Marshal(invite)
	if err != nil {
		return fmt.Errorf("failed to marshal invite: %w", err)
	}

	cacheKey := createCacheKey(invite.Type, invite.SenderId, invite.RecipientId)
	expiryTime := getInviteExpiryTime(invite)

	err = i.redis.Do(ctx, i.redis.B().Set().Key(cacheKey).Value(string(raw)).Nx().Ex(expiryTime).Build()).Error()
	if errors.Is(err, rueidis.Nil) {
		switch invite.Type {
		case model.InviteTypeInvite:
			return ErrInviteExists
		case model.InviteTypeRequest:
			return ErrRequestExists
		}
	} else if err != nil {
		return fmt.Errorf("failed to set invite in redis: %w", err)
	}

	senderDefaultKey := fmt.Sprintf("sess:default_invite:%s", invite.SenderId)
	err = i.redis.Do(ctx, i.redis.B().Set().Key(senderDefaultKey).Value(cacheKey).Ex(expiryTime).Build()).Error()
	if err != nil {
		return fmt.Errorf("failed to set default invite for sender: %w", err)
	}

	recipientDefaultKey := fmt.Sprintf("sess:default_invite:%s", invite.RecipientId)
	err = i.redis.Do(ctx, i.redis.B().Set().Key(recipientDefaultKey).Value(cacheKey).Ex(expiryTime).Build()).Error()
	if err != nil {
		return fmt.Errorf("failed to set default invite/request for recipient: %w", err)
	}

	if err = i.sendInviteMessage(ctx, invite); err != nil {
		return fmt.Errorf("failed to send invite message: %w", err)
	}

	return nil
}

func (i *InviteManager) sendInviteMessage(ctx context.Context, invite *model.MapInvite) error {
	msg := &model.CreatedMapInviteMessage{
		Type:        invite.Type,
		SenderId:    invite.SenderId,
		RecipientId: invite.RecipientId,
		MapId:       invite.MapId,
	}

	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return i.producer.WriteMessages(ctx, kafka.Message{
		Topic: inviteTopic,
		Value: raw,
	})
}

func (i *InviteManager) sendAcceptedOrRejectedMessage(ctx context.Context, invite *model.MapInvite, accepted bool) error {
	msg := &model.MapInviteAcceptedOrRejectedMessage{
		Type:        invite.Type,
		SenderId:    invite.SenderId,
		RecipientId: invite.RecipientId,
		MapId:       invite.MapId,
		Accepted:    accepted,
	}

	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return i.producer.WriteMessages(ctx, kafka.Message{
		Topic: inviteAcceptRejectTopic,
		Value: raw,
	})
}

func (i *InviteManager) getDefaultKey(ctx context.Context, senderId string) (string, error) {
	value, err := i.redis.Do(ctx, i.redis.B().Get().Key(fmt.Sprintf("sess:default_invite:%s", senderId)).Build()).ToString()
	if errors.Is(err, rueidis.Nil) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("failed to get default invite key: %w", err)
	}
	return value, nil
}

func (i *InviteManager) removeDefaultKey(ctx context.Context, senderId string) error {
	return i.redis.Do(ctx, i.redis.B().Del().Key(fmt.Sprintf("sess:default_invite:%s", senderId)).Build()).Error()
}

func (i *InviteManager) GetInvite(ctx context.Context, senderId string, recipientId string) (*model.MapInvite, error) {
	return i.get(ctx, createCacheKey(model.InviteTypeInvite, senderId, recipientId))
}

func (i *InviteManager) GetRequest(ctx context.Context, senderId string, recipientId string) (*model.MapInvite, error) {
	return i.get(ctx, createCacheKey(model.InviteTypeRequest, senderId, recipientId))
}

func (i *InviteManager) get(ctx context.Context, cacheKey string) (*model.MapInvite, error) {
	raw, err := i.redis.Do(ctx, i.redis.B().Get().Key(cacheKey).Build()).AsBytes()
	if errors.Is(err, rueidis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get invite from redis: %w", err)
	}

	var invite model.MapInvite
	if err = msgpack.Unmarshal(raw, &invite); err != nil {
		return nil, fmt.Errorf("failed to unmarshal invite: %w", err)
	}

	return &invite, nil
}

func (i *InviteManager) remove(ctx context.Context, invite *model.MapInvite) error {
	cacheKey := createCacheKey(invite.Type, invite.SenderId, invite.RecipientId)

	err := i.redis.Do(ctx, i.redis.B().Del().Key(cacheKey).Build()).Error()
	if err != nil {
		return fmt.Errorf("failed to delete invite from redis: %w", err)
	}

	senderDefaultKey := createDefaultKey(invite.SenderId)
	senderDefault, err := i.redis.Do(ctx, i.redis.B().Get().Key(senderDefaultKey).Build()).ToString()
	if err != nil {
		return fmt.Errorf("failed to get default invite for sender: %w", err)
	}

	if senderDefault == cacheKey {
		err = i.reselectDefault(ctx, invite.SenderId)
		if err != nil {
			return fmt.Errorf("failed to reselect default invite for sender: %w", err)
		}
	}

	recipientDefaultKey := createDefaultKey(invite.RecipientId)
	recipientDefault, err := i.redis.Do(ctx, i.redis.B().Get().Key(recipientDefaultKey).Build()).ToString()
	if err != nil {
		return fmt.Errorf("failed to get default invite for recipient: %w", err)
	}

	if recipientDefault == cacheKey {
		err = i.reselectDefault(ctx, invite.RecipientId)
		if err != nil {
			return fmt.Errorf("failed to reselect default invite for recipient: %w", err)
		}
	}
	return nil
}

func (i *InviteManager) reselectDefault(ctx context.Context, id string) error {
	candidates, err := i.redis.Do(ctx, i.redis.B().Keys().Pattern(fmt.Sprintf("sess:*:%s/*", id)).Build()).AsStrSlice()
	if err != nil {
		return fmt.Errorf("failed to get invites/requests for player: %w", err)
	}
	if len(candidates) == 0 {
		// No invites
		return nil
	}

	var latestInvite *model.MapInvite
	for _, candidate := range candidates {
		invite, err := i.get(ctx, candidate)
		if err != nil {
			return fmt.Errorf("failed to get invite: %w", err)
		}
		if invite == nil {
			// Should be impossible but just in case
			continue
		}

		if latestInvite == nil || invite.CreatedAt.After(latestInvite.CreatedAt) {
			latestInvite = invite
		}
	}

	latestInviteKey := createCacheKey(latestInvite.Type, latestInvite.SenderId, latestInvite.RecipientId)
	expiryTime := getInviteExpiryTime(latestInvite)

	err = i.redis.Do(ctx, i.redis.B().Set().Key(fmt.Sprintf("sess:default_invite:%s", id)).Value(latestInviteKey).Ex(expiryTime).Build()).Error()
	if err != nil {
		return fmt.Errorf("failed to set default invite for player: %w", err)
	}

	return nil
}

func createCacheKey(keyType model.InviteType, senderId string, recipientId string) string {
	return fmt.Sprintf("sess:%s:%s/%s", keyType, senderId, recipientId)
}

func createDefaultKey(id string) string {
	return fmt.Sprintf("sess:default_invite:%s", id)
}

func getInviteExpiryTime(invite *model.MapInvite) time.Duration {
	switch invite.Type {
	case model.InviteTypeInvite:
		return inviteExpiryTime
	case model.InviteTypeRequest:
		return requestExpiryTime
	}
	// If we somehow have an invite type that isn't invite or request, default to invite expiry time
	return inviteExpiryTime
}
