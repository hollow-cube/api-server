package v4Internal

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/hollow-cube/api-server/internal/db"
	"github.com/hollow-cube/api-server/internal/pkg/util"
)

type (
	CreateLaunchGrantRequest struct {
		PlayerID string  `json:"playerId"`
		MapID    *string `json:"mapId"`
	}
	LaunchGrant struct {
		URL       string    `json:"url"`
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
)

// POST /auth/grant
func (s *Server) CreateLaunchGrant(ctx context.Context, body CreateLaunchGrantRequest) (*LaunchGrant, error) {
	const launchGrantExp = 2 * time.Minute

	var b [9]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	code := base64.RawURLEncoding.EncodeToString(b[:])
	expiresAt := time.Now().Add(launchGrantExp)

	err := s.sessionStore.CreateLaunchGrant(ctx, db.CreateLaunchGrantParams{
		CodeHash:  util.Sha256b([]byte(code)),
		PlayerID:  body.PlayerID,
		MapID:     body.MapID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, err
	}

	return &LaunchGrant{
		// URL hash is so the token isnt sent to servers, §k is used to make the mc
		// client render it obfuscated so someone screensharing/streaming doesnt leak
		// the code.
		// TODO: should convert to do base64-like encoding with a custom alphabet
		//       of zero width private use unicode characters so its invisible.
		URL:       fmt.Sprintf("%s#§k=%s", s.editorUrl, code),
		Code:      code,
		ExpiresAt: expiresAt,
	}, nil
}
