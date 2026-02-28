package natsutil

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.38.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type Message interface {
	Subject() string
}

type JetStreamWrapper struct {
	log *zap.SugaredLogger
	js  jetstream.JetStream
}

func NewJetStreamWrapper(js jetstream.JetStream) *JetStreamWrapper {
	return &JetStreamWrapper{zap.S(), js}
}

func (w *JetStreamWrapper) UpsertStream(ctx context.Context, config jetstream.StreamConfig) error {
	_, err := w.js.CreateOrUpdateStream(ctx, config)
	if err != nil {
		return fmt.Errorf("create %s: %w", config.Name, err)
	}

	return nil
}

type Consumer struct {
	Start func() error
	Stop  func() error
}

// Subscribe creates a consumer for the given stream and starts consuming messages with the provided handler.
// The returned Consumer has Start and Stop methods to control the consumption lifecycle.
//
// If acks are enabled and an error is returned a Nak will be done automatically.
// If you want to avoid this behavior do not return an error.
// Acks will never be done automatically currently.
func (w *JetStreamWrapper) Subscribe(ctx context.Context, stream string, config jetstream.ConsumerConfig, handler func(ctx context.Context, msg jetstream.Msg) error) (Consumer, error) {
	chatConsumer, err := w.js.CreateOrUpdateConsumer(ctx, stream, config)
	if err != nil {
		return Consumer{}, fmt.Errorf("create consumer %s: %w", stream, err)
	}

	var consumeContext jetstream.ConsumeContext
	return Consumer{
		Start: func() error {
			consumeContext, err = chatConsumer.Consume(func(msg jetstream.Msg) {
				carrier := headerCarrier(msg.Headers())
				ctx := otel.GetTextMapPropagator().Extract(ctx, &carrier)
				ctx, span := tracer.Start(ctx, "nats.consume "+stream,
					trace.WithSpanKind(trace.SpanKindConsumer),
					trace.WithAttributes(
						semconv.MessagingSystemKey.String("nats"),
					),
				)
				defer span.End()

				err = handler(ctx, msg)
				if err != nil {
					w.log.Errorf("failed to handle nats message: %v", err)
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())

					// If we have acks enabled, we should Nak the message to trigger redelivery.
					if config.AckPolicy != jetstream.AckNonePolicy {
						if err = msg.Nak(); err != nil {
							w.log.Errorf("failed to nak nats message: %v", err)
						}
					}
				}
			})
			return err
		},
		Stop: func() error {
			if consumeContext != nil {
				consumeContext.Stop()
			}
			return nil
		},
	}, nil
}

// PublishJSONAsync
// Note that the ctx passed is only used for tracing data, and is not required after this
// method returns so its valid to use a request (or otherwise temporary) context.
func (w *JetStreamWrapper) PublishJSONAsync(ctx context.Context, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	return w.PublishAsync(ctx, msg.Subject(), data)
}

func (w *JetStreamWrapper) PublishAsync(ctx context.Context, subject string, data []byte) error {
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  nats.Header{},
	}

	carrier := headerCarrier(msg.Header)
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	_, err := w.js.PublishMsgAsync(msg)
	return err
}

func (w *JetStreamWrapper) PublishAsyncWithHeader(ctx context.Context, subject string, data []byte, header nats.Header) error {
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  header,
	}

	carrier := headerCarrier(msg.Header)
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	_, err := w.js.PublishMsgAsync(msg)
	return err
}
