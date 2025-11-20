package wkafka

//go:generate mockgen -source=writer.go -destination=mock_wkafka/writer.gen.go

import (
	"context"

	"github.com/segmentio/kafka-go"
)

type Writer interface {
	WriteMessages(ctx context.Context, messages ...kafka.Message) error
}

type SyncWriter interface {
	Writer
}
