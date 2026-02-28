package v4Internal

import (
	"github.com/hollow-cube/api-server/internal/playerdb"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type ServerParams struct {
	fx.In

	Log   *zap.SugaredLogger
	Store *playerdb.Store
}

type Server struct {
	log *zap.SugaredLogger

	store *playerdb.Store
}

func NewServer(p ServerParams) (*Server, error) {
	s := &Server{
		log:   p.Log,
		store: p.Store,
	}

	return s, nil
}
