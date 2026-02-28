package httpfx

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hollow-cube/api-server/internal/pkg/common"
	httpTransport "github.com/hollow-cube/api-server/internal/pkg/http"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var Module = fx.Module("http",
	fx.Provide(
		AsMiddleware(httpTransport.ZapMiddleware),
		AsMiddleware(httpTransport.PrometheusMiddleware),
		AsMiddleware(httpTransport.TraceNameMiddleware),
		NewChiServer,
	),

	// Start the HTTP server
	fx.Invoke(func(*http.Server) {}),
)

type ChiServerParams struct {
	fx.In
	Lifecycle fx.Lifecycle

	Log        *zap.SugaredLogger
	SvcConfig  common.ServiceConfig
	HTTPConfig common.HTTPConfig

	Middleware          []httpTransport.Middleware         `group:"middleware"`
	MiddlewareProviders []httpTransport.MiddlewareProvider `group:"middleware_provider"`
	Routes              []httpTransport.RouteProvider      `group:"routes"`

	// force the TracerProvider to be created if not already - we depend on it for traces to be properly stored and exported
	TracerProvider trace.TracerProvider
}

func NewChiServer(p ChiServerParams) *http.Server {
	chiR := chi.NewRouter()
	// It is important that providers go first, the trace provider must be before the logging middleware.
	for _, provider := range p.MiddlewareProviders {
		chiR.Use(provider.Provide(chiR).Run)
	}
	for _, middleware := range p.Middleware {
		chiR.Use(middleware.Run)
	}

	chiR.Get("/metrics", promhttp.Handler().ServeHTTP)
	for _, routeProvider := range p.Routes {
		p.Log.Infof("Registering route %T", routeProvider)
		routeProvider.Apply(chiR)
	}

	// todo make these configurable
	aliveHandler := &httpTransport.AliveHandler{}
	chiR.Get("/alive", aliveHandler.ServeHTTP)
	readyHandler := &httpTransport.ReadyHandler{}
	chiR.Get("/ready", readyHandler.ServeHTTP)

	traceIgnoredPaths := map[string]bool{
		"/alive":   true,
		"/ready":   true,
		"/metrics": true,
	}
	otelR := otelhttp.NewHandler(chiR, p.SvcConfig.Name+"-chi",
		otelhttp.WithFilter(func(r *http.Request) bool {
			return !traceIgnoredPaths[r.URL.Path]
		}),
	)

	address := fmt.Sprintf("%s:%d", p.HTTPConfig.Address, p.HTTPConfig.Port)
	srv := &http.Server{Addr: address, Handler: otelR}
	p.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return err
			}

			p.Log.Infow("Starting HTTP server", "addr", srv.Addr)
			go srv.Serve(ln)
			return nil
		},
		OnStop: srv.Shutdown,
	})
	return srv
}

func AsMiddleware(f any) any {
	return fx.Annotate(
		f,
		fx.As(new(httpTransport.Middleware)),
		fx.ResultTags(`group:"middleware"`),
	)
}

func AsMiddlewareProvider(f any) any {
	return fx.Annotate(
		f,
		fx.As(new(httpTransport.MiddlewareProvider)),
		fx.ResultTags(`group:"middleware_provider"`),
	)
}

func AsRouteProvider(f any) any {
	return fx.Annotate(
		f,
		fx.As(new(httpTransport.RouteProvider)),
		fx.ResultTags(`group:"routes"`),
	)
}
