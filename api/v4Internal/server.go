package v4Internal

import (
	sessiondb "github.com/hollow-cube/api-server/internal/db"
	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type ServerParams struct {
	fx.In

	Log          *zap.SugaredLogger
	PlayerStore  *playerdb.Store
	MapStore     *mapdb.Store
	SessionStore *sessiondb.Queries
}

type Server struct {
	log *zap.SugaredLogger

	playerStore  *playerdb.Store
	mapStore     *mapdb.Store
	sessionStore *sessiondb.Queries
}

func NewServer(p ServerParams) (*Server, error) {
	s := &Server{
		log:          p.Log,
		playerStore:  p.PlayerStore,
		mapStore:     p.MapStore,
		sessionStore: p.SessionStore,
	}

	return s, nil
}

const (
	defaultPageSize    = 10
	defaultMaxPageSize = 100
)

func defaultPageParams(page, pageSize int) (int32, int32) {
	return pageParams(page, pageSize, defaultMaxPageSize)
}

func pageParams(page, pageSize, maxPageSize int) (int32, int32) {
	var offset, limit int32
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
