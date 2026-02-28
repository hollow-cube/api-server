package natsutil

import (
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("github.com/hollow-cube/hc-services/libraries/common/natsutil")

// headerCarrier implements propagation.TextMapCarrier for nats.Header.
type headerCarrier nats.Header

func (c headerCarrier) Get(key string) string {
	return nats.Header(c).Get(key)
}

func (c headerCarrier) Set(key, value string) {
	nats.Header(c).Set(key, value)
}

func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(nats.Header(c)))
	for k := range nats.Header(c) {
		keys = append(keys, k)
	}
	return keys
}
