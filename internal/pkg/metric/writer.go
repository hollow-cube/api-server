package metric

import (
	"reflect"

	"go.uber.org/zap"
)

type Writer interface {
	Write(metric any)
}

type noop struct {
	log *zap.SugaredLogger
}

func NewWriterNoop(log *zap.SugaredLogger) Writer {
	return &noop{log}
}

func (m *noop) Write(metric any) {
	ty := reflect.TypeOf(metric)
	if ty.Kind() == reflect.Ptr {
		ty = ty.Elem()
	}
	m.log.Infow("write metric", "name", ty.Name(), "value", metric)
}
