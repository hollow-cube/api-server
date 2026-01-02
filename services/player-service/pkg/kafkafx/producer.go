package kafkafx

import (
	"context"
	"strings"
	"time"

	"github.com/hollow-cube/hc-services/services/player-service/config"
	"github.com/segmentio/kafka-go"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

func NewWriter(lc fx.Lifecycle, log *zap.SugaredLogger, config *config.Config) (*kafka.Writer, error) {
	w := &kafka.Writer{
		Addr:         kafka.TCP(strings.Split(config.Kafka.Brokers, ",")...),
		RequiredAcks: kafka.RequireAll,
		Async:        false,
		Logger:       kafka.LoggerFunc(log.Infof),
		ErrorLogger:  kafka.LoggerFunc(log.Errorf),

		WriteBackoffMin: 20 * time.Millisecond,
		WriteBackoffMax: 100 * time.Millisecond,
		BatchTimeout:    100 * time.Millisecond,

		AllowAutoTopicCreation: true,
	}

	lc.Append(fx.Hook{
		OnStop: func(_ context.Context) error {
			return w.Close()
		},
	})

	return w, nil
}
