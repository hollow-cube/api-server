package metric

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/mitchellh/mapstructure"
	"github.com/posthog/posthog-go"
	"go.uber.org/zap"
)

var _ Writer = (*posthogWriter)(nil)

type posthogWriter struct {
	log    *zap.SugaredLogger
	client posthog.Client
}

func NewPosthogWriter(log *zap.SugaredLogger, client posthog.Client) Writer {
	return &posthogWriter{log, client}
}

func (p *posthogWriter) Write(metric any) {
	name, properties, err := p.serializeMetric(metric)
	if err != nil {
		p.log.Errorw("failed to serialize metric", "metric", metric, "error", err)
		return
	}

	playerId, ok := properties["player_id"].(string)
	if ok {
		delete(properties, "player_id")
	} else {
		playerId = "00000000-0000-0000-0000-000000000000"
	}

	err = p.client.Enqueue(posthog.Capture{
		Event:      name,
		DistinctId: playerId,
		Properties: properties,
	})
	if err != nil {
		p.log.Errorw("failed to enqueue metric", "metric", metric, "error", err)
	}
}

func (p *posthogWriter) serializeMetric(metric any) (name string, value map[string]interface{}, err error) {
	ty := reflect.TypeOf(metric)
	if ty.Kind() == reflect.Ptr {
		ty = ty.Elem()
	}
	if ty.Kind() != reflect.Struct {
		return "", nil, fmt.Errorf("metric must be a struct")
	}
	name = upperCamelToLowerSnake(transformName(ty.Name()))

	if err = mapstructure.Decode(metric, &value); err != nil {
		return "", nil, err
	}

	return
}

func upperCamelToLowerSnake(input string) (output string) {
	runes := []rune(input)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i != 0 {
				output += "_"
			}
			output += string(unicode.ToLower(r))
		} else {
			output += string(r)
		}
	}
	return output
}

func transformName(name string) string {
	if strings.HasPrefix(name, "Metric") {
		name = name[6:]
	}
	if strings.HasSuffix(name, "Event") {
		name = name[:len(name)-5]
	}
	return name
}
