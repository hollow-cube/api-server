package intnl

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/storage"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/util"
)

func (s *server) LookupPlayerDataBySocial(ctx context.Context, request LookupPlayerDataBySocialRequestObject) (LookupPlayerDataBySocialResponseObject, error) {
	p, err := s.storageClient.LookupPlayerDataBySocial(ctx, request.PlatformId, request.Platform)
	apiPlayer, err := s.playerDataToAPIWithName(p, err, ctx)
	if err != nil {
		return nil, err
	} else if apiPlayer == nil {
		return PlayerNotFoundResponse{}, nil
	}
	return LookupPlayerDataBySocial200JSONResponse(*apiPlayer), nil
}

func (s *server) LookupSocialByPlayerId(ctx context.Context, request LookupSocialByPlayerIdRequestObject) (LookupSocialByPlayerIdResponseObject, error) {
	socialId, err := s.storageClient.LookupSocialByPlayerId(ctx, request.Platform, request.PlayerId)
	if errors.Is(err, storage.ErrNotFound) {
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
	rep, err := s.storageClient.GetPendingVerification(ctx, verificationType, request.Body.UserSecret)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return AttemptVerification404Response{}, nil
		}
		return nil, fmt.Errorf("failed to fetch pending verification: %w", err)
	}
	if rep.Expiration.UnixNano() <= time.Now().UnixNano() {
		err = s.storageClient.DeletePendingVerification(ctx, &request.Body.UserSecret, verificationType, false)
		if err != nil {
			s.log.Fatalw("Couldn't delete expired verification record: %w", err)
		}

		return AttemptVerification204Response{}, nil
	}

	// Ensure there is not already an account linked with this discord account
	_, err = s.storageClient.LookupPlayerDataBySocial(ctx, rep.UserID, "discord")
	if !errors.Is(err, storage.ErrNotFound) {
		return AttemptVerification409Response{}, nil
	}

	// Fetch the player data, creating it if it doesn't exist.
	pd, err := s.storageClient.GetPlayerData(ctx, request.Body.PlayerId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// Create the player data if they don't exist
			pd = &model.PlayerData{
				Id:         request.Body.PlayerId,
				LastOnline: time.Now(),
			}
			if err = s.storageClient.CreatePlayerData(ctx, pd); err != nil {
				return nil, fmt.Errorf("failed to create player data: %w", err)
			}
		}

		return nil, fmt.Errorf("failed to get player data: %w", err)
	}

	// Delete the pending verification and add the linked account
	err = s.storageClient.RunTransaction(ctx, func(ctx context.Context) error {
		err = s.storageClient.DeletePendingVerification(ctx, &request.Body.UserSecret, verificationType, false)
		if err != nil {
			return fmt.Errorf("failed to delete pending verification: %w", err)
		}

		err = s.storageClient.AddLinkedAccount(ctx, request.Body.PlayerId, rep.UserID, "discord")
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
		UserId: rep.UserID,
	}, nil
}

func (s *server) BeginVerifyDiscord(ctx context.Context, request BeginVerifyDiscordRequestObject) (BeginVerifyDiscordResponseObject, error) {
	_, err := s.storageClient.LookupPlayerDataBySocial(ctx, request.Body.DiscordId, "discord")
	if err == nil {
		return BeginVerifyDiscord409Response{}, nil
	}

	const verificationExpirationTimer = 5 * time.Minute

	var secret = genVerifySecret()
	err = s.storageClient.CreatePendingVerification(ctx, &model.PendingVerification{
		Type:       model.VerificationTypeDiscord,
		UserID:     request.Body.DiscordId,
		UserSecret: secret,
		Expiration: time.Now().Add(verificationExpirationTimer),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create verifcation record: %w", err)
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
