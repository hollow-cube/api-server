//go:generate go run ../../cmd/ox/main.go generate ./.Server

package v1Public

import (
	sessiondb "github.com/hollow-cube/api-server/internal/db"
	"github.com/hollow-cube/api-server/internal/interaction"
	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/notification"
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

	Notifications notification.Manager
	Interactions  *interaction.Handler
}

type Server struct {
	log *zap.SugaredLogger

	playerStore  *playerdb.Store
	mapStore     *mapdb.Store
	sessionStore *sessiondb.Queries

	notifications notification.Manager
	interactions  *interaction.Handler
}

type AuthenticatedRequest struct {
	PlayerID string `header:"x-auth-user"`
}

func NewServer(p ServerParams) (*Server, error) {
	s := &Server{
		log:           p.Log,
		playerStore:   p.PlayerStore,
		mapStore:      p.MapStore,
		sessionStore:  p.SessionStore,
		notifications: p.Notifications,
		interactions:  p.Interactions,
	}

	return s, nil
}
