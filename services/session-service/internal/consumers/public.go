package consumers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/natsutil"
	"github.com/hollow-cube/hc-services/services/session-service/internal/playerdb"
	"github.com/hollow-cube/hc-services/services/session-service/pkg/kafkaModel"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/fx"
)

func NewConsumerSet(lc fx.Lifecycle, jetStream *natsutil.JetStreamWrapper, store *playerdb.Store) error {
	// if you add more, separate into multiple files :)

	// currently forces one processor with maxAckPending. its fine for now will eventually just merge and this will be solved.
	cons, err := jetStream.Subscribe(context.Background(), "SESSIONS", jetstream.ConsumerConfig{
		Name:          "players-session-updates",
		Durable:       "players-session-updates",
		FilterSubject: "session.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       10 * time.Second,
		MaxAckPending: 1,
		MaxDeliver:    3,
	}, func(ctx context.Context, rawMsg jetstream.Msg) error {
		var msg kafkaModel.SessionUpdateMessage
		if err := json.Unmarshal(rawMsg.Data(), &msg); err != nil {
			return err
		}

		var newOnline bool
		switch msg.Action {
		case kafkaModel.Session_Create:
			newOnline = true
		case kafkaModel.Session_Delete:
			newOnline = false
		default:
			return nil // do nothing
		}

		if err := store.UpdatePlayerData(ctx, playerdb.UpdatePlayerDataParams{ID: msg.PlayerId, Online: &newOnline}); err != nil {
			return fmt.Errorf("failed to update player data: %w", err)
		}

		return rawMsg.Ack()
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to session updates: %w", err)
	}
	lc.Append(fx.StartStopHook(cons.Start, cons.Stop))

	return nil
}
