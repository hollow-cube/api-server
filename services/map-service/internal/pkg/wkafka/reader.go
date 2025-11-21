package wkafka

import (
	"context"

	"github.com/segmentio/kafka-go"
)

type ReaderFactory interface {
	// NewReader returns a new managed reader, meaning it will be automatically cleaned up
	// when the service is shutting down.
	NewReader(topic string) Reader
}

type ReaderFactoryFunc func(topic string) Reader

func (f ReaderFactoryFunc) NewReader(topic string) Reader {
	return f(topic)
}

type Reader interface {
	ReadMessage(ctx context.Context) (kafka.Message, error)

	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
}
