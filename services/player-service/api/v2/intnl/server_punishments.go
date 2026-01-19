package intnl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/segmentio/kafka-go"
)

func (s *server) GetActivePunishment(ctx context.Context, request GetActivePunishmentRequestObject) (GetActivePunishmentResponseObject, error) {
	ty := model.PunishmentType(request.Params.PunishmentType)
	if ty != model.PunishmentTypeBan && ty != model.PunishmentTypeMute {
		return nil, fmt.Errorf("invalid active punishment type: %s", ty)
	}

	p, err := s.store.GetActivePunishment(ctx, string(ty), request.PlayerId)
	if errors.Is(err, db.ErrNoRows) {
		return GetActivePunishment404Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get active punishment: %w", err)
	}

	return GetActivePunishment200JSONResponse(punishmentToAPI(p)), nil
}

func (s *server) GetPunishments(ctx context.Context, request GetPunishmentsRequestObject) (GetPunishmentsResponseObject, error) {
	punishmentType := ""
	if request.Params.PunishmentType != nil {
		punishmentType = string(*request.Params.PunishmentType)
	}

	var executorId string
	if request.Params.ExecutorId != nil && *request.Params.ExecutorId != "" {
		executorId = *request.Params.ExecutorId
	}
	punishments, err := s.store.SearchPunishments(ctx, db.SearchPunishmentsParams{
		Type:       punishmentType,
		PlayerID:   request.Params.PlayerId,
		ExecutorID: executorId,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get punishments: %w", err)
	}

	result := make(GetPunishments200JSONResponse, 0, len(punishments))
	for _, punishment := range punishments {
		result = append(result, punishmentToAPI(punishment))
	}
	return result, nil
}

func (s *server) CreatePunishment(ctx context.Context, request CreatePunishmentRequestObject) (CreatePunishmentResponseObject, error) {
	if ok := punishmentTypeValidationMap[request.Body.PunishmentType]; !ok {
		return nil, fmt.Errorf("invalid punishment type: %s", request.Body.PunishmentType)
	}

	// Validate the reason
	var rawReason *string
	if request.Body.Reason != nil && *request.Body.Reason != "" {
		rawReason = request.Body.Reason
	}

	// Kicks may not have a reason
	if request.Body.PunishmentType == PunishmentTypeKick && rawReason != nil {
		return nil, fmt.Errorf("kicks cannot have a ladder id")
	}
	// Mute and ban must have a ladder id
	if (request.Body.PunishmentType == PunishmentTypeBan || request.Body.PunishmentType == PunishmentTypeMute) && rawReason == nil {
		return nil, fmt.Errorf("mute and ban must have a ladder id")
	}

	// Compute the expiration time (if this is not a kick)
	var ladderId *string
	var expiresAt *time.Time
	if request.Body.PunishmentType == PunishmentTypeBan || request.Body.PunishmentType == PunishmentTypeMute {
		// Find the ladder associated with this reason
		//goland:noinspection GoDfaNilDereference
		ladder, ok := s.punishmentAliases[model.PunishmentType(request.Body.PunishmentType)][*rawReason]
		if !ok || ladder == nil {
			return nil, fmt.Errorf("ladder not found: %s", *rawReason)
		}
		ladderId = &ladder.Id

		// Check if the player is already punished with this type
		_, err := s.store.GetActivePunishment(ctx, string(request.Body.PunishmentType), request.Body.PlayerId)
		if err != nil && !errors.Is(err, db.ErrNoRows) {
			return nil, fmt.Errorf("failed to get active punishment: %w", err)
		} else if err == nil {
			return CreatePunishment409Response{}, nil
		}

		// Find the existing punishments in the given ladder
		punishments, err := s.store.SearchPunishments(ctx, db.SearchPunishmentsParams{
			Type:     string(request.Body.PunishmentType),
			PlayerID: request.Body.PlayerId,
			LadderID: &ladder.Id,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to search punishments: %w", err)
		}

		index := 0
		for _, punishment := range punishments {
			if punishment.CreatedAt.Before(time.Now().Add(-6 * 31 * 24 * time.Hour)) {
				// Ignore punishments older than 6 months
				continue
			}

			index++
		}
		entry := ladder.Entries[min(index, len(ladder.Entries)-1)]
		if entry == nil {
			return nil, fmt.Errorf("ladder entry not found: %s/%d", ladder.Id, index)
		}

		if entry.Duration > 0 {
			expireTime := time.Now().Add(time.Duration(entry.Duration) * time.Second)
			expiresAt = &expireTime
		}

		// Otherwise it is permanent so should not have an expiration set
	}

	comment := "unspecified"
	if request.Body.Comment != nil {
		comment = *request.Body.Comment
	}
	if rawReason != nil && ladderId != nil && *ladderId != *rawReason {
		comment = fmt.Sprintf("%s: %s", *rawReason, comment)
	}

	punishment, err := s.store.CreatePunishment(ctx, db.CreatePunishmentParams{
		PlayerID:   request.Body.PlayerId,
		ExecutorID: request.Body.ExecutorId,
		Type:       string(request.Body.PunishmentType),
		LadderID:   ladderId,
		Comment:    comment,
		ExpiresAt:  expiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create punishment: %w", err)
	}

	err = s.sendPunishmentUpdateMessage(ctx, model.PunishmentUpdateAction_Create, &punishment)
	if err != nil {
		return nil, fmt.Errorf("failed to send kick punishment target message: %w", err)
	}

	go func() {
		switch punishment.Type {
		case string(model.PunishmentTypeBan):
			s.metrics.Write(&model.PlayerBanned{
				PlayerId:   request.Body.PlayerId,
				ExecutorId: request.Body.ExecutorId,
				LadderId:   *ladderId,
			})
		case string(model.PunishmentTypeMute):
			s.metrics.Write(&model.PlayerMuted{
				PlayerId:   request.Body.PlayerId,
				ExecutorId: request.Body.ExecutorId,
				LadderId:   *ladderId,
			})
		case string(model.PunishmentTypeKick):
			s.metrics.Write(&model.PlayerKicked{
				PlayerId:   request.Body.PlayerId,
				ExecutorId: request.Body.ExecutorId,
			})
		}
	}()

	return CreatePunishment200JSONResponse(punishmentToAPI(punishment)), nil
}

func (s *server) GetPunishmentLadders(_ context.Context, request GetPunishmentLaddersRequestObject) (GetPunishmentLaddersResponseObject, error) {
	var punishmentType model.PunishmentType
	if request.Params.PunishmentType != nil && *request.Params.PunishmentType != "" {
		punishmentType = model.PunishmentType(*request.Params.PunishmentType)
	} else {
		punishmentType = ""
	}
	var result GetPunishmentLadders200JSONResponse
	for _, ladder := range s.punishmentLadders {
		if punishmentType != "" && ladder.Type != punishmentType {
			continue // Not for the required punishment type - ignore
		}

		apiLadder := ladderToAPI(ladder)
		if request.Params.Id != nil && *request.Params.Id != "" {
			if ladder.Id == *request.Params.Id {
				// If it is the exact ID, we just return that single ladder
				return append(result, apiLadder), nil
			}

			if strings.Contains(ladder.Id, *request.Params.Id) {
				// Do a partial match search
				result = append(result, apiLadder)
			}
		} else {
			// No ID specified, we've already filtered the type - always add
			result = append(result, apiLadder)
		}
	}

	return result, nil
}

func (s *server) GetPunishmentLadderEntry(_ context.Context, request GetPunishmentLadderEntryRequestObject) (GetPunishmentLadderEntryResponseObject, error) {
	ladder := s.punishmentLadders[request.Params.LadderId]
	if ladder == nil || request.Params.Index == "" {
		return GetPunishmentLadderEntry404Response{}, nil
	}
	entryIndex, err := strconv.ParseInt(request.Params.Index, 10, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse index: %w", err)
	}

	entry := ladder.Entries[entryIndex]
	if entry == nil {
		return GetPunishmentLadderEntry404Response{}, nil
	}

	return GetPunishmentLadderEntry200JSONResponse{
		Id:       ladder.Id,
		Name:     ladder.Name,
		Duration: int(entry.Duration),
	}, nil
}

func (s *server) RevokePunishment(ctx context.Context, request RevokePunishmentRequestObject) (RevokePunishmentResponseObject, error) {
	p, err := s.store.RevokePunishment(ctx, db.RevokePunishmentParams{
		Type:          string(request.Body.Type),
		PlayerID:      request.Body.PlayerId,
		RevokedBy:     &request.Body.RevokedBy,
		RevokedReason: &request.Body.RevokedReason,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to revoke punishment: %w", err)
	}

	err = s.sendPunishmentUpdateMessage(ctx, model.PunishmentUpdateAction_Revoke, &p)
	if err != nil {
		return nil, fmt.Errorf("failed to send update message: %w", err)
	}

	go func() {
		if p.Type == string(model.PunishmentTypeBan) {
			s.metrics.Write(&model.PlayerUnbanned{
				PlayerId:  request.Body.PlayerId,
				RevokerId: request.Body.RevokedBy,
			})
		} else if p.Type == string(model.PunishmentTypeMute) { // todo punishment type should be a pg enum
			s.metrics.Write(&model.PlayerUnmuted{
				PlayerId:  request.Body.PlayerId,
				RevokerId: request.Body.RevokedBy,
			})
		}
	}()

	return RevokePunishment200Response{}, nil
}

func (s *server) sendPunishmentUpdateMessage(ctx context.Context, action model.PunishmentUpdateAction, punishment *db.Punishment) error {
	msg := &model.PunishmentUpdateMessage{
		Action:     action,
		Punishment: punishment,
	}

	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return s.producer.WriteMessages(ctx, kafka.Message{
		Topic: kafkafx.TopicPunishmentUpdate,
		Value: raw,
	})
}

func punishmentToAPI(p db.Punishment) Punishment {
	return Punishment{
		PlayerId:   p.PlayerID,
		ExecutorId: p.ExecutorID,
		Type:       PunishmentType(p.Type),
		CreatedAt:  p.CreatedAt,
		LadderId:   p.LadderID,
		Comment:    p.Comment,
		ExpiresAt:  p.ExpiresAt,

		RevokedBy:     p.RevokedBy,
		RevokedAt:     p.RevokedAt,
		RevokedReason: p.RevokedReason,
	}
}

func ladderToAPI(l *model.PunishmentLadder) PunishmentLadder {
	entries := make([]PunishmentLadderEntry, len(l.Entries))
	for i, entry := range l.Entries {
		entries[i] = PunishmentLadderEntry{Duration: int(entry.Duration)}
	}

	reasons := make([]PunishmentLadderReason, len(l.Reasons))
	for i, reason := range l.Reasons {
		reasons[i] = PunishmentLadderReason{Id: reason.Id, Aliases: reason.Aliases}
	}

	return PunishmentLadder{
		Id:      l.Id,
		Name:    l.Name,
		Type:    PunishmentType(l.Type),
		Entries: entries,
		Reasons: &reasons,
	}
}

var punishmentTypeValidationMap = map[PunishmentType]bool{
	PunishmentTypeBan:  true,
	PunishmentTypeMute: true,
	PunishmentTypeKick: true,
}
