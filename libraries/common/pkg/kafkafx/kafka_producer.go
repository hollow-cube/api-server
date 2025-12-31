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

func newProducer(conf common.KafkaConfig, lc fx.Lifecycle, log *zap.SugaredLogger, async bool, opts ...ProducerOption) *producerImpl {
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
	}

	for _, opt := range opts {
		opt(w)
	}
	lc.Append(fx.StopHook(w.Close))
	return &producerImpl{Writer: w}
}

func NewSyncKafkaProducer(conf common.KafkaConfig, lc fx.Lifecycle, log *zap.SugaredLogger, opts ...ProducerOption) SyncProducer {
	return newProducer(conf, lc, log, false, opts...)
}

func NewAsyncKafkaProducer(conf common.KafkaConfig, lc fx.Lifecycle, log *zap.SugaredLogger, opts ...ProducerOption) AsyncProducer {
	return newProducer(conf, lc, log, true, opts...)
}

type ProducerOption func(writer *kafka.Writer)

func WithInstantWrite() ProducerOption {
	return func(writer *kafka.Writer) {
		writer.BatchSize = 1
	}
}
