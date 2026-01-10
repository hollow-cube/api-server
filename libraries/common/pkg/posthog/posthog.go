// Package posthog is a thin globalized wrapper around the PostHog API client
// because I (A) don't want to pass around the client and (B) want "noop" behavior
// if it is not configured.
package posthog

import (
	"context"
	"time"

	"github.com/posthog/posthog-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
)

// InternalID is a posthog distinct ID used for internal queries not to be
// associated with a user/map/etc
const InternalID = "cccccccb-57f7-45fc-98ef-b4d2f51f5ea6"

var otelTracer = otel.Tracer("github.com/hollow-cube/hc-services/libraries/common/pkg/posthog")
var localClient, nonLocalClient posthog.Client
var defaultValue bool

func Init(clientToUse, nonLocalClientToUse posthog.Client) {
	localClient = clientToUse
	nonLocalClient = nonLocalClientToUse
}

func InitFixedValue(value bool) {
	defaultValue = value
}

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
	if nonLocalClient == nil {
		return
	}

	err := nonLocalClient.Enqueue(posthog.Capture{
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

func IsFeatureEnabledRemote(ctx context.Context, key string, distinctId string) bool {
	ctx, span := otelTracer.Start(ctx, "posthog.IsFeatureEnabledRemote")
	defer span.End()

	if nonLocalClient == nil {
		return defaultValue
	}

	value, err := nonLocalClient.IsFeatureEnabled(posthog.FeatureFlagPayload{
		Key:        key,
		DistinctId: distinctId,
	})
	if err != nil {
		zap.S().Infow("failed to fetch feature flag", "key", key, "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return false
	}

	span.SetStatus(codes.Ok, "")
	if s, ok := value.(string); ok {
		return s != "false" // This handles feature flag payload response
	} else if b, ok := value.(bool); ok {
		return b
	} else {
		return false // something else idk
	}
}
