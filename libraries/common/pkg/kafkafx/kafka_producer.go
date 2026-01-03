package kafkafx

import (
	"context"
	"strings"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"github.com/segmentio/kafka-go"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ SyncProducer = (*producerImpl)(nil)
var _ AsyncProducer = (*producerImpl)(nil)

type SyncProducer interface {
	WriteMessages(ctx context.Context, messages ...kafka.Message) error
}

type AsyncProducer interface {
	WriteMessages(ctx context.Context, messages ...kafka.Message) error
}

type producerImpl struct {
	*kafka.Writer
}

func newProducer(conf common.KafkaConfig, lc fx.Lifecycle, log *zap.SugaredLogger, async bool) *producerImpl {
	w := &kafka.Writer{
		Addr:                   kafka.TCP(strings.Split(conf.Brokers, ",")...),
		Balancer:               &kafka.Hash{},
		Async:                  async,
		AllowAutoTopicCreation: true,
		ErrorLogger:            kafka.LoggerFunc(log.Errorf),

		WriteBackoffMin: 20 * time.Millisecond,
		WriteBackoffMax: 100 * time.Millisecond,
		BatchTimeout:    100 * time.Millisecond,
	}

	if async {
		w.Completion = func(messages []kafka.Message, err error) {
			if err != nil {
				log.Errorw("failed to write message", "error", err)
			}
		}

		// Async is a lazy producer
		w.WriteBackoffMin = 20 * time.Millisecond
		w.WriteBackoffMax = 100 * time.Millisecond
		w.BatchTimeout = 100 * time.Millisecond
	} else {
		// Sync is an instant producer
		w.WriteBackoffMin = 0
		w.WriteBackoffMax = 0
		w.BatchSize = 1
	}

	lc.Append(fx.StopHook(w.Close))
	return &producerImpl{Writer: w}
}

// NewSyncKafkaProducer create a synchronous and instant publish producer
func NewSyncKafkaProducer(conf common.KafkaConfig, lc fx.Lifecycle, log *zap.SugaredLogger) SyncProducer {
	return newProducer(conf, lc, log, false)
}

// NewAsyncKafkaProducer creates a new asynchronous and lazy Kafka producer
func NewAsyncKafkaProducer(conf common.KafkaConfig, lc fx.Lifecycle, log *zap.SugaredLogger) AsyncProducer {
	return newProducer(conf, lc, log, true)
}
