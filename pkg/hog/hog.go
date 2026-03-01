// Package hog is a thin globalized wrapper around the PostHog API client to
// (A) avoid passing around the client and
// (B) provide some "noop" behavior if not configured.
package hog

import (
	"time"

	"github.com/posthog/posthog-go"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

var otelTracer = otel.Tracer("github.com/hollow-cube/hc-services/libraries/common/pkg/posthog")
var client posthog.Client

type Capture struct {
	// You don't usually need to specify this field - Posthog will generate it automatically.
	// Use it only when necessary - for example, to prevent duplicate events.
	Uuid       string
	DistinctId string
	Event      string
	Timestamp  time.Time
	Properties Properties
}

// Enqueue sends the provided capture to the PostHog API.
// Does not block, logs errors if any occur.
func Enqueue(capture Capture) {
	if client == nil {
		return
	}

	err := client.Enqueue(posthog.Capture{
		Uuid:       capture.Uuid,
		DistinctId: capture.DistinctId,
		Event:      capture.Event,
		Timestamp:  capture.Timestamp,
		Properties: posthog.Properties(capture.Properties),
	})
	if err != nil {
		zap.S().Errorw("failed to enqueue posthog event", "error", err)
	}
}

type Properties map[string]interface{}

func NewProperties() Properties {
	return make(Properties, 10)
}

func (p Properties) Set(name string, value interface{}) Properties {
	p[name] = value
	return p
}

// Merge adds the properties from the provided `props` into the receiver `p`.
// If a property in `props` already exists in `p`, its value will be overwritten.
func (p Properties) Merge(props Properties) Properties {
	if props == nil {
		return p
	}

	for k, v := range props {
		p[k] = v
	}

	return p
}
