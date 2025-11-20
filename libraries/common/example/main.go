package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/httpfx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/tracefx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/zapfx"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

func main() {
	fx.New(
		fx.Provide(func() common.Config {
			return common.Config{
				ServiceConfig: common.ServiceConfig{
					Env:  "dev",
					Name: "example",
				},
				HTTPConfig: common.HTTPConfig{
					Address: "0.0.0.0",
					Port:    9120,
				},
			}
		}),

		// Logging
		zapfx.Module,
		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: log}
		}),

		fx.Provide(
			httpfx.AsRouteProvider(NewExampleRP),
		),

		// HTTP server w/ tracing
		tracefx.Module,
		httpfx.Module,
	).Run()
}

func newZapLogger() (*zap.Logger, error) {
	return zap.NewDevelopment()
}

func newZapSugared(log *zap.Logger) *zap.SugaredLogger {
	zap.ReplaceGlobals(log)
	return log.Sugar()
}

type ExampleRP struct {
}

func NewExampleRP() *ExampleRP {
	return &ExampleRP{}
}

func (e *ExampleRP) Apply(r chi.Router) {
	r.Get("/example", func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte("hello world"))
	})
}
