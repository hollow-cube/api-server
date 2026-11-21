//go:generate go run ../../cmd/ox/main.go generate ./.Server

package v4Internal

import (
	"github.com/hollow-cube/api-server/config"
	sessiondb "github.com/hollow-cube/api-server/internal/db"
	"github.com/hollow-cube/api-server/internal/interaction"
	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type ServerParams struct {
	fx.In

	Log          *zap.SugaredLogger
	Config       *config.Config
	PlayerStore  *playerdb.Store
	MapStore     *mapdb.Store
	SessionStore *sessiondb.Queries

	Interactions *interaction.Handler
}

type Server struct {
	log *zap.SugaredLogger

	playerStore  *playerdb.Store
	mapStore     *mapdb.Store
	sessionStore *sessiondb.Queries

	interactions *interaction.Handler

	editorUrl string
}

func NewServer(p ServerParams) (*Server, error) {
	s := &Server{
		log:          p.Log,
		playerStore:  p.PlayerStore,
		mapStore:     p.MapStore,
		sessionStore: p.SessionStore,
		interactions: p.Interactions,
		editorUrl:    p.Config.EditorURL,
	}

	return s, nil
}
