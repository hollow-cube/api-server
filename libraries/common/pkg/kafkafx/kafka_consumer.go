package kafkafx

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type Consumer interface {
	Subscribe(topic string, consumerGroup string, handler func(ctx context.Context, message kafka.Message) error, opts ...SubscribeOption)
	MultiSubscribe(topics []string, consumerGroup string, handler func(ctx context.Context, message kafka.Message) error, opts ...SubscribeOption)
}

type consumerImpl struct {
	conf common.KafkaConfig
	log  *zap.SugaredLogger

	cancelsMtx sync.Mutex
	cancels    []context.CancelFunc
	shutdownWg sync.WaitGroup
}

func NewConsumer(conf common.KafkaConfig, log *zap.SugaredLogger, lc fx.Lifecycle) Consumer {
	c := &consumerImpl{
		conf: conf,
		log:  log,

		cancelsMtx: sync.Mutex{},
		cancels:    make([]context.CancelFunc, 0),
	}

	lc.Append(fx.StopHook(c.shutdown))

	return c
}

type SubscribeOption func(cfg *kafka.ReaderConfig)

func WithIsolationLevel(level kafka.IsolationLevel) SubscribeOption {
	return func(cfg *kafka.ReaderConfig) {
		cfg.IsolationLevel = level
	}
}

func (c *consumerImpl) Subscribe(topic string, consumerGroup string, handler func(ctx context.Context, message kafka.Message) error, opts ...SubscribeOption) {
	cfg := kafka.ReaderConfig{
		Brokers:  strings.Split(c.conf.Brokers, ","),
		GroupID:  consumerGroup,
		Topic:    topic,
		MaxBytes: 10e6, // 10mb
		//Logger:      kafka.LoggerFunc(log.Infof),
		ErrorLogger: kafka.LoggerFunc(c.log.Errorf),
	}

	c.subscribe(cfg, handler, opts...)
}

func (c *consumerImpl) MultiSubscribe(topics []string, consumerGroup string, handler func(ctx context.Context, message kafka.Message) error, opts ...SubscribeOption) {
	cfg := kafka.ReaderConfig{
		Brokers:     strings.Split(c.conf.Brokers, ","),
		GroupID:     consumerGroup,
		GroupTopics: topics,
		MaxBytes:    10e6, // 10mb
		//Logger:      kafka.LoggerFunc(log.Infof),
		ErrorLogger: kafka.LoggerFunc(c.log.Errorf),
	}

	c.subscribe(cfg, handler, opts...)
}

func (c *consumerImpl) subscribe(cfg kafka.ReaderConfig, handler func(ctx context.Context, message kafka.Message) error, opts ...SubscribeOption) {
	for _, opt := range opts {
		opt(&cfg)
	}

	r := kafka.NewReader(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	c.cancelsMtx.Lock()
	defer c.cancelsMtx.Unlock()
	c.cancels = append(c.cancels, cancel)

	c.shutdownWg.Add(1)
	go func() {
		defer c.shutdownWg.Done()
		defer func() {
			err := r.Close()
			if err != nil {
				c.log.Errorf("failed to close kafka reader: %v", err)
				return
			}

			c.log.Infow("kafka reader closed successfully", "topic", cfg.Topic)
		}()

		c.log.Infow("starting kafka reader", "topic", cfg.Topic)

		for {
			m, err := r.FetchMessage(ctx)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
					c.log.Info("Shutting down consumer ", "reason", err)
					break // break since the application should be shutting down
				}

				// don't terminate on this error - it could be transient, e.g. rebalance
				c.log.Warnw("failed to read kafka message: ", err)
				continue
			}

			// Extract trace context from message headers and start a consumer span
			ctx = ExtractTraceContext(ctx, m)
			ctx, span := tracer.Start(ctx, "kafka.consume "+m.Topic,
				trace.WithSpanKind(trace.SpanKindConsumer),
				trace.WithAttributes(
					semconv.MessagingSystemKafka,
					semconv.MessagingDestinationName(m.Topic),
					semconv.MessagingKafkaMessageOffset(int(m.Offset)),
					semconv.MessagingKafkaConsumerGroup(cfg.GroupID),
				),
			)
			if len(m.Key) > 0 {
				span.SetAttributes(semconv.MessagingKafkaMessageKey(string(m.Key)))
			}

			// todo: in the future we could handle automatic error retries and DLQ logic in a common manner
			// **REALLY IMPORTANT NOTE**:
			// As this does not currently do DLQs for you, throwing an error you should consider the message lost.
			// You may be lucky to handle it again, but not necessarily.
			// When committing, Kafka treats that as committing up to that offset, so if a newer message is committed,
			// it commits the previous failed message as well.
			// A handler should implement a DLQ itself where necessary.
			if err := handler(ctx, m); err != nil {
				c.log.Errorf("failed to handle kafka message: %v", err)
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				span.End()
				continue // message not committed, will be redelivered
			}

			if err := r.CommitMessages(ctx, m); err != nil { // commit only after success
				c.log.Errorf("failed to commit kafka message: %v", err)
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}

			span.End()
		}
	}()
}

func (c *consumerImpl) shutdown(_ context.Context) error {
	c.cancelsMtx.Lock()
	defer c.cancelsMtx.Unlock()

	for _, cancel := range c.cancels {
		cancel()
	}
	c.shutdownWg.Wait()

	return nil
}
