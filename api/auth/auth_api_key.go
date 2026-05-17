package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/hollow-cube/api-server/internal/playerdb"
)

// TODO: Add description of api key auth mechanism

func (s *Server) checkApiKey(ctx context.Context, apiKeyStr string) (authState, error) {
	var apiKey playerdb.ApiKey
	var err error

	h := sha256.Sum256([]byte(apiKeyStr))
	hash := hex.EncodeToString(h[:])
	apiKey, err = s.playerStore.GetApiKeyByHash(ctx, hash)
	if errors.Is(err, playerdb.ErrNoRows) {
		apiKey.ID = ""
	} else if err != nil {
		return authState{}, err
	}

	if apiKey.ID == "" {
		return authState{}, nil
	}

	return authState{
		Valid:    true,
		PlayerID: apiKey.PlayerID,
	}, nil
}
