package v4Internal

import (
	"context"
	"errors"
	"fmt"

	sessiondb "github.com/hollow-cube/api-server/internal/db"
	"github.com/hollow-cube/api-server/internal/interaction"
	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/natsutil"
	"github.com/hollow-cube/api-server/internal/pkg/notification"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"github.com/hollow-cube/api-server/pkg/ox"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type ServerParams struct {
	fx.In

	Log          *zap.SugaredLogger
	PlayerStore  *playerdb.Store
	MapStore     *mapdb.Store
	SessionStore *sessiondb.Queries
	JetStream    *natsutil.JetStreamWrapper

	Notifications notification.Manager
	Interactions  *interaction.Handler
}

type Server struct {
	log *zap.SugaredLogger

	playerStore  *playerdb.Store
	mapStore     *mapdb.Store
	sessionStore *sessiondb.Queries
	jetStream    *natsutil.JetStreamWrapper

	notifications notification.Manager
	interactions  *interaction.Handler
}

func NewServer(p ServerParams) (*Server, error) {
	s := &Server{
		log:           p.Log,
		playerStore:   p.PlayerStore,
		mapStore:      p.MapStore,
		sessionStore:  p.SessionStore,
		jetStream:     p.JetStream,
		notifications: p.Notifications,
		interactions:  p.Interactions,
	}

	return s, nil
}

// player fetches the given player's data, mapping db errors to the correct api error.
func (s *Server) player(ctx context.Context, id string) (pd playerdb.PlayerData, err error) {
	pd, err = s.playerStore.GetPlayerData(ctx, id)
	if errors.Is(err, playerdb.ErrNoRows) {
		return pd, ox.NotFound{}
	} else if err != nil {
		return pd, fmt.Errorf("failed to get player data: %w", err)
	}
	return
}

const (
	defaultPageSize    = 10
	defaultMaxPageSize = 100
)

func defaultPageParams(page, pageSize int) (int32, int32) {
	return pageParams(page, pageSize, defaultMaxPageSize)
}

func pageParams(page, pageSize, maxPageSize int) (offset int32, limit int32) {
	if pageSize <= 0 || pageSize > maxPageSize {
		limit = int32(defaultPageSize)
	} else {
		limit = int32(pageSize)
	}
	if page <= 0 {
		offset = int32(0)
	} else {
		offset = int32(page) * limit
	}
	return offset, limit
}
