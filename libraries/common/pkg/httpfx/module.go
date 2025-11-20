package httpfx

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	httpTransport "github.com/hollow-cube/hc-services/libraries/common/pkg/http"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var Module = fx.Module("http",
	fx.Provide(
		AsMiddleware(httpTransport.ZapMiddleware),
		AsMiddleware(httpTransport.PrometheusMiddleware),
		NewChiServer,
	),

	// Start the HTTP server
	fx.Invoke(func(*http.Server) {}),
)

type ChiServerParams struct {
	fx.In
	Lifecycle fx.Lifecycle

	Log    *zap.SugaredLogger
	Config common.HTTPConfig

	Middleware          []httpTransport.Middleware         `group:"middleware"`
	MiddlewareProviders []httpTransport.MiddlewareProvider `group:"middleware_provider"`
	Routes              []httpTransport.RouteProvider      `group:"routes"`
}

func NewChiServer(p ChiServerParams) *http.Server {
	r := chi.NewRouter()
	// It is important that providers go first, the trace provider must be before the logging middleware.
	for _, provider := range p.MiddlewareProviders {
		r.Use(provider.Provide(r).Run)
	}
	for _, middleware := range p.Middleware {
		r.Use(middleware.Run)
	}

	r.Get("/metrics", promhttp.Handler().ServeHTTP)
	for _, routeProvider := range p.Routes {
		p.Log.Infof("Registering route %T", routeProvider)
		routeProvider.Apply(r)
	}

	// todo make these configurable
	aliveHandler := &httpTransport.AliveHandler{}
	r.Get("/alive", aliveHandler.ServeHTTP)
	readyHandler := &httpTransport.ReadyHandler{}
	r.Get("/ready", readyHandler.ServeHTTP)

	address := fmt.Sprintf("%s:%d", p.Config.Address, p.Config.Port)
	srv := &http.Server{Addr: address, Handler: r}
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
