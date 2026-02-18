package main

import (
	"encoding/json"

	"github.com/go-chi/chi/v5"
	httpTransport "github.com/hollow-cube/hc-services/libraries/common/pkg/http"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/httpfx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/natsutil"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/tracefx"
	mapService "github.com/hollow-cube/hc-services/services/map-service/api/v3/intnl"
	playerService2 "github.com/hollow-cube/hc-services/services/player-service/api/v2/intnl"
	intnlV3 "github.com/hollow-cube/hc-services/services/session-service/api/v3/intnl"
	"github.com/hollow-cube/hc-services/services/session-service/config"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/handler"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/player"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/server"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/world"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	fx.New(
		// Config
		fx.Provide(config.NewMergedConfig, newCommonConfigResources),
		fx.Invoke(func(conf *config.Config) {
			result, _ := json.MarshalIndent(conf, "", "  ")
			println(string(result))
		}),

		// Logging
		fx.Provide(
			newZapLogger,
			newZapSugared,
		),
		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: log}
		}),

		// Dependencies
		fx.Invoke(setupPosthogClient),
		fx.Provide(newKubernetesClient),
		fx.Provide(
			func(conf *config.Config, lc fx.Lifecycle) (*nats.Conn, error) {
				nc, err := nats.Connect(conf.NATS.Servers)
				if err != nil {
					return nil, err
				}

				lc.Append(fx.StopHook(nc.Close))
				return nc, nil
			},
			jetstream.New,
			natsutil.NewJetStreamWrapper,
		),

		// Kafka
		kafkafx.Module,

		fx.Provide(newRedisClient),
		fx.Provide(newPlayerSvc2, newMapServiceClient),
		fx.Provide(newDbQuerySet),
		fx.Provide(newAuthzSpiceDB),
		fx.Provide(newGithubClient),

		fx.Invoke(handler.NewChatHandler),

		fx.Provide(player.NewTracker),
		fx.Invoke(func(t *player.Tracker, lc fx.Lifecycle) {
			lc.Append(fx.Hook{OnStart: t.Start, OnStop: t.Stop})
		}),
		fx.Provide(server.NewTracker),
		fx.Invoke(func(t *server.Tracker, lc fx.Lifecycle) {
			if t.K8sNamespace != "disabled" {
				lc.Append(fx.Hook{OnStart: t.Start, OnStop: t.Stop})
			}
		}),
		fx.Provide(world.NewTracker),

		fx.Provide(handler.NewInviteManager), // Possibly legacy

		// HTTP server
		fx.Provide(tracefx.NewHttpExporter),
		tracefx.Module,
		fx.Provide(
			intnlV3.NewServer,
			httpfx.AsRouteProvider(makeV2RouteHandler),
		),
		httpfx.Module,
	).Run()
}

func newPlayerSvc2(conf *config.Config) (playerService2.ClientWithResponsesInterface, error) {
	return playerService2.NewClientWithResponses(conf.PlayerServiceUrl+"/v2/internal", playerService2.WithHTTPClient(tracefx.DefaultHTTPClient))
}

func newMapServiceClient(conf *config.Config) (mapService.ClientWithResponsesInterface, error) {
	return mapService.NewClientWithResponses(conf.MapServiceUrl+"/v3/internal", mapService.WithHTTPClient(tracefx.DefaultHTTPClient))
}

func newKubernetesClient(conf *config.Config) (*kubernetes.Clientset, error) {
	if conf.Kubernetes.Namespace == "disabled" {
		return nil, nil
	}

	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	//todo replace the klog logger with a zap version
	return kubernetes.NewForConfig(k8sConfig)
}

type v2RouteHandlerImpl struct {
	intnl intnlV3.StrictServerInterface
}

func (v *v2RouteHandlerImpl) Apply(r chi.Router) {
	r.Handle("/v3/internal/*", intnlV3.HandlerFromMuxWithBaseURL(intnlV3.NewStrictHandler(v.intnl,
		[]intnlV3.StrictMiddlewareFunc{}), nil, "/v3/internal"))
}

func makeV2RouteHandler(intnl intnlV3.StrictServerInterface) httpTransport.RouteProvider {
	return &v2RouteHandlerImpl{intnl}
}
