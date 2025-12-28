package intnl

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/util"
)

func (s *server) LookupPlayerDataBySocial(ctx context.Context, request LookupPlayerDataBySocialRequestObject) (LookupPlayerDataBySocialResponseObject, error) {
	pd, err := s.store.LookupPlayerDataBySocialId(ctx, request.Platform, request.PlatformId)
	if errors.Is(err, db.ErrNoRows) {
		return LookupPlayerDataBySocial404Response{}, nil
	} else if err != nil {
		return nil, err
	}

	apiPlayer, err := s.hydratePlayerData(ctx, pd)
	if err != nil {
		return nil, err
	}
	return LookupPlayerDataBySocial200JSONResponse(*apiPlayer), nil
}

func (s *server) LookupSocialByPlayerId(ctx context.Context, request LookupSocialByPlayerIdRequestObject) (LookupSocialByPlayerIdResponseObject, error) {
	socialId, err := s.store.LookupSocialIdByPlayerId(ctx, request.Platform, request.PlayerId)
	if errors.Is(err, db.ErrNoRows) {
		return LookupSocialByPlayerId404Response{}, nil
	} else if err != nil {
		return nil, err
	}

	return LookupSocialByPlayerId200JSONResponse{SocialId: socialId}, nil
}

func (s *server) AttemptVerification(ctx context.Context, request AttemptVerificationRequestObject) (AttemptVerificationResponseObject, error) {
	request.Body.PlayerId = util.RemapUUID(request.Body.PlayerId)
	verificationType := model.VerificationType(request.Body.VerificationType)

	// Fetch the existing verification, handling a case where it's expired.
	pending, err := s.store.GetPendingVerificationBySecret(ctx, string(verificationType), request.Body.UserSecret)
	if errors.Is(err, db.ErrNoRows) {
		return AttemptVerification404Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch pending verification: %w", err)
	}
	if pending.Expiration.UnixNano() <= time.Now().UnixNano() {
		err = s.store.DeletePendingVerificationBySecret(ctx, string(verificationType), request.Body.UserSecret)
		if err != nil {
			s.log.Errorw("Couldn't delete expired verification record: %w", err)
		}

		return AttemptVerification204Response{}, nil
	}

	// Ensure there is not already an account linked with this discord account
	_, err = s.store.LookupPlayerDataBySocialId(ctx, "discord", pending.UserID)
	if !errors.Is(err, db.ErrNoRows) {
		return AttemptVerification409Response{}, nil
	}

	// Ensure the player exists already - they always should so error if not as they must have logged into the server before.
	if pExists, err := s.store.PlayerExistsById(ctx, request.Body.PlayerId); err != nil {
		return nil, fmt.Errorf("failed to get player data: %w", err)
	} else if !pExists {
		s.log.Warnw("player attempted to link discord account but does not exist", "playerId", request.Body.PlayerId, "discordId", pending.UserID)
		return AttemptVerification404Response{}, nil
	}

	// Delete the pending verification and add the linked account
	err = db.TxNoReturn(ctx, s.store, func(ctx context.Context, tx *db.Store) (err error) {
		err = tx.DeletePendingVerificationBySecret(ctx, string(verificationType), request.Body.UserSecret)
		if err != nil {
			return fmt.Errorf("failed to delete pending verification: %w", err)
		}

		err = tx.AddLinkedAccount(ctx, "discord", request.Body.PlayerId, pending.UserID)
		if err != nil {
			return fmt.Errorf("failed to add linked account: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return AttemptVerification200JSONResponse{
		Type:   request.Body.VerificationType,
		UserId: pending.UserID,
	}, nil
}

func (s *server) BeginVerifyDiscord(ctx context.Context, request BeginVerifyDiscordRequestObject) (BeginVerifyDiscordResponseObject, error) {
	_, err := s.store.LookupPlayerDataBySocialId(ctx, "discord", request.Body.DiscordId)
	if !errors.Is(err, db.ErrNoRows) {
		return BeginVerifyDiscord409Response{}, nil
	}

	const verificationExpirationTimer = 5 * time.Minute

	var secret = genVerifySecret()
	err = s.store.UpsertPendingVerification(ctx, db.UpsertPendingVerificationParams{
		Type:       string(model.VerificationTypeDiscord),
		UserID:     request.Body.DiscordId,
		UserSecret: secret,
		Expiration: time.Now().Add(verificationExpirationTimer),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create verification record: %w", err)
	}

	return BeginVerifyDiscord201JSONResponse{UserSecret: secret}, nil
}

func genVerifySecret() string {
	var table = []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789")
	var tableSize = len(table)

	var sb strings.Builder
	for i := 0; i < 7; i++ {
		randomChar := table[rand.Intn(tableSize)]
		sb.WriteRune(randomChar)
	}

	return sb.String()
}
