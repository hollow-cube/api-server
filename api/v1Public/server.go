//go:generate go run ../../cmd/ox/main.go generate ./.Server

package v1Public

import (
	"context"
	"time"

	sessiondb "github.com/hollow-cube/api-server/internal/db"
	"github.com/hollow-cube/api-server/internal/interaction"
	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/natsutil"
	"github.com/hollow-cube/api-server/internal/pkg/notification"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"github.com/nats-io/nats.go/jetstream"
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
	js           *natsutil.JetStreamWrapper

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
		js:            p.JetStream,
		notifications: p.Notifications,
		interactions:  p.Interactions,
	}

	err := p.JetStream.UpsertStream(context.Background(), jetstream.StreamConfig{
		// Not just map.{id}.files because MAP_MANAGEMENT over reaches :( should sort out later
		Name:       "MAP_FILES",
		Subjects:   []string{"map-files.>"},
		Retention:  jetstream.LimitsPolicy,
		Storage:    jetstream.FileStorage,
		MaxAge:     5 * time.Minute,
		Duplicates: time.Minute,
	})
	if err != nil {
		return nil, err
	}

	return s, nil
}
