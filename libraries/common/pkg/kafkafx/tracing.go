package kafkafx

import (
	"context"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("github.com/hollow-cube/hc-services/libraries/common/kafkafx")

// HeaderCarrier implements propagation.TextMapCarrier for kafka.Message headers.
type HeaderCarrier []kafka.Header

func (c *HeaderCarrier) Get(key string) string {
	for _, h := range *c {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c *HeaderCarrier) Set(key, value string) {
	// Remove existing header with same key
	for i, h := range *c {
		if h.Key == key {
			(*c)[i] = kafka.Header{Key: key, Value: []byte(value)}
			return
		}
	}
	// Add new header
	*c = append(*c, kafka.Header{Key: key, Value: []byte(value)})
}

func (c *HeaderCarrier) Keys() []string {
	keys := make([]string, len(*c))
	for i, h := range *c {
		keys[i] = h.Key
	}
	return keys
}

func InjectTraceContext(ctx context.Context, msg *kafka.Message) {
	carrier := HeaderCarrier(msg.Headers)
	otel.GetTextMapPropagator().Inject(ctx, &carrier)
	msg.Headers = carrier
}

func ExtractTraceContext(ctx context.Context, msg kafka.Message) context.Context {
	carrier := HeaderCarrier(msg.Headers)
	return otel.GetTextMapPropagator().Extract(ctx, &carrier)
}
