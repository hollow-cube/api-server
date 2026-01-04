package consumers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/session-service/pkg/kafkaModel"
	"github.com/segmentio/kafka-go"
)

const consumerGroup = "player-service"

func NewConsumerSet(consumer kafkafx.Consumer, store *db.Store) {

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
}
