package main

import (
	"github.com/go-chi/chi/v5"
	"github.com/hollow-cube/api-server/internal/pkg/common"
	httpTransport "github.com/hollow-cube/api-server/internal/pkg/http"
	"github.com/hollow-cube/api-server/internal/pkg/httpfx"
	"github.com/hollow-cube/api-server/internal/pkg/tracefx"
	"github.com/hollow-cube/api-server/web"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

func main() {
	fx.New(
		fx.Provide(
			newZapLogger,
			newZapSugared,
		),
		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: log}
		}),

		fx.Provide(func() (common.HTTPConfig, common.ServiceConfig) {
			return common.HTTPConfig{
					Port: 8080,
				}, common.ServiceConfig{
					Env:  "dev",
					Name: "web-minimal",
				}
		}),

		fx.Provide(newDynamicExporter),
		tracefx.Module,
		fx.Provide(httpfx.AsRouteProvider(makeV2RouteHandler)),
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

type routeHandlerImpl struct {
}

func (v *routeHandlerImpl) Apply(r chi.Router) {
	web.Run(r)
}

func makeV2RouteHandler(p struct {
	fx.In
}) httpTransport.RouteProvider {
	return &routeHandlerImpl{}
}

func newDynamicExporter() (trace.SpanExporter, error) {
	return tracefx.NewNoopExporter()
}
