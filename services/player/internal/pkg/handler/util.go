package handler

import (
	"context"
	"encoding/json"

	"github.com/hollow-cube/hc-services/services/player/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player/internal/pkg/wkafka"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

func sendPlayerDataUpdateMessage(w wkafka.Writer, ctx context.Context, msg *model.PlayerDataUpdateMessage) {
	log := zap.S()

	content, err := json.Marshal(msg)
	if err != nil {
		log.Errorw("failed to marshal player data update message", "error", err)
		return
	}

	kafkaRecord := kafka.Message{
		Topic: "player_data_updates",
		Key:   []byte(msg.Id),
		Value: content,
	}

	if err = w.WriteMessages(context.Background(), kafkaRecord); err != nil {
		log.Errorw("failed to write to kafka", "error", err)
	}
}
