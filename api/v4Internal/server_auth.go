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
		URL:       fmt.Sprintf("http://localhost:5173/editor#§k=%s", code),
		Code:      code,
		ExpiresAt: expiresAt,
	}, nil
}
