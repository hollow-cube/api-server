package consumers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/natsutil"
	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/session-service/pkg/kafkaModel"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/segmentio/kafka-go"
	"go.uber.org/fx"
)

const consumerGroup = "player-service"

func NewConsumerSet(lc fx.Lifecycle, consumer kafkafx.Consumer, jetStream *natsutil.JetStreamWrapper, store *db.Store) error {

	// if you add more, separate into multiple files :)
	consumer.Subscribe(kafkafx.TopicSessionUpdates, consumerGroup, func(ctx context.Context, rawMsg kafka.Message) error {
		var msg kafkaModel.SessionUpdateMessage
		if err := json.Unmarshal(rawMsg.Value, &msg); err != nil {
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

		if err := store.UpdatePlayerData(ctx, db.UpdatePlayerDataParams{ID: msg.PlayerId, Online: &newOnline}); err != nil {
			return fmt.Errorf("failed to update player data: %w", err)
		}

		return nil
	})

	// currently forces one processor with maxAckPending. its fine for now will eventually just merge and this will be solved.
	cons, err := jetStream.Subscribe(context.Background(), "SESSIONS", jetstream.ConsumerConfig{
		Durable:       "chat-processor",
		FilterSubject: "chat.raw.>",
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

		if err := store.UpdatePlayerData(ctx, db.UpdatePlayerDataParams{ID: msg.PlayerId, Online: &newOnline}); err != nil {
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
