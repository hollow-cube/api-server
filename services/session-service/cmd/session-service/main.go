package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	mapIntnlV3 "github.com/hollow-cube/hc-services/services/session-service/api/mapsV3/intnl"
	mapObungusV3 "github.com/hollow-cube/hc-services/services/session-service/api/mapsV3/obungus"
	mapPublicV3 "github.com/hollow-cube/hc-services/services/session-service/api/mapsV3/public"
	mapTerraformV3 "github.com/hollow-cube/hc-services/services/session-service/api/mapsV3/terraform"
	v2Internal "github.com/hollow-cube/hc-services/services/session-service/api/v2/intnl"
	v2Payments "github.com/hollow-cube/hc-services/services/session-service/api/v2/payments"
	v2Public "github.com/hollow-cube/hc-services/services/session-service/api/v2/public"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	awsCredentials "github.com/aws/aws-sdk-go-v2/credentials"
	s3manager "github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bwmarrin/discordgo"
	envoyAuth "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"github.com/go-chi/chi/v5"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	httpTransport "github.com/hollow-cube/hc-services/libraries/common/pkg/http"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/httpfx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/natsutil"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/tracefx"
	"github.com/hollow-cube/hc-services/services/session-service/api/auth"
	posthogProxy "github.com/hollow-cube/hc-services/services/session-service/api/posthog"
	intnlV3 "github.com/hollow-cube/hc-services/services/session-service/api/v3/intnl"
	"github.com/hollow-cube/hc-services/services/session-service/config"
	"github.com/hollow-cube/hc-services/services/session-service/internal/consumers"
	"github.com/hollow-cube/hc-services/services/session-service/internal/discord"
	"github.com/hollow-cube/hc-services/services/session-service/internal/object"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/handler"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/player"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/server"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/world"
	"github.com/hollow-cube/tebex-go"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
	"google.golang.org/grpc"
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
		fx.Provide(newPosthogClient, metric.NewPosthogWriter),
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

		fx.Provide(newRedisClient),
		fx.Provide(newSessionPostgresStore, newPlayerPostgresStore, newMapsPostgresStore),
		fx.Provide(newGithubClient),
		fx.Provide(newTebexHeadlessClient),

		// Converted punishment ladders - for internal handler
		fx.Provide(newLaddersFromConfig),

		fx.Provide(
			newS3Client,
			newS3Downloader,
			newS3Uploader,
			fx.Annotate(
				object.NewS3ClientFactory("mapmaker"),
				fx.As(new(object.Client)),
				fx.ResultTags(`name:"object-mapmaker"`),
			),
			fx.Annotate(
				object.NewS3ClientFactory("mapmaker-replays"),
				fx.As(new(object.Client)),
				fx.ResultTags(`name:"object-mapmaker-replays"`),
			),
			fx.Annotate(
				object.NewS3ClientFactory("legacy-maps-v3"),
				fx.As(new(object.Client)),
				fx.ResultTags(`name:"object-mapmaker-legacy-maps"`),
			),
			fx.Annotate(
				object.NewS3ClientFactory("mapmaker-profdump"),
				fx.As(new(object.Client)),
				fx.ResultTags(`name:"object-mapmaker-perfdumps"`),
			),
		),

		fx.Provide(newDiscordClient, discord.NewHandler),

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

		// Generic consumer (e.g., denormalized data from other services)
		fx.Invoke(consumers.NewConsumerSet),

		// HTTP server
		fx.Provide(newDynamicExporter),
		tracefx.Module,
		fx.Provide(
			v2Public.NewServer,
			v2Internal.NewServer,
			v2Payments.NewServer,

			mapPublicV3.NewServer,
			mapIntnlV3.NewServer,
			mapTerraformV3.NewServer,
			mapObungusV3.NewServer,

			intnlV3.NewServer,

			posthogProxy.NewProxy,
			httpfx.AsRouteProvider(makeV2RouteHandler),
		),
		httpfx.Module,

		// GRPC server (for Envoy)
		fx.Provide(auth.NewServer),
		fx.Invoke(newGrpcServer),
	).Run()
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

type routeHandlerImpl struct {
	public   v2Public.StrictServerInterface
	internal v2Internal.StrictServerInterface
	payments v2Payments.ServerInterface

	mapPublic    mapPublicV3.StrictServerInterface
	mapIntnl     mapIntnlV3.StrictServerInterface
	mapTerraform mapTerraformV3.StrictServerInterface
	mapObungus   mapObungusV3.StrictServerInterface

	intnl intnlV3.StrictServerInterface

	posthog *posthogProxy.Proxy

	discord *discord.Handler
}

func (v *routeHandlerImpl) Apply(r chi.Router) {
	r.Handle("/v2/players/*", v2Public.HandlerFromMuxWithBaseURL(v2Public.NewStrictHandler(v.public, nil), nil, "/v2/players"))
	r.Handle("/v2/internal/*", v2Internal.HandlerFromMuxWithBaseURL(v2Internal.NewStrictHandler(v.internal, nil), nil, "/v2/internal"))
	r.Handle("/v2/payments/*", v2Payments.HandlerFromMuxWithBaseURL(v.payments, nil, "/v2/payments"))

	r.Handle("/v3/maps/*", mapPublicV3.HandlerFromMuxWithBaseURL(mapPublicV3.NewStrictHandler(v.mapPublic,
		[]mapPublicV3.StrictMiddlewareFunc{mapPublicV3.AuthMiddleware}), nil, "/v3/maps"))
	mapV3Int := mapIntnlV3.HandlerFromMuxWithBaseURL(mapIntnlV3.NewStrictHandler(v.mapIntnl,
		[]mapIntnlV3.StrictMiddlewareFunc{mapIntnlV3.AuthMiddleware}), nil, "/v3/internal")
	r.Handle("/v3/internal/maps", mapV3Int)
	r.Handle("/v3/internal/maps/*", mapV3Int)
	r.Handle("/v3/internal/map-players/*", mapV3Int)
	r.Handle("/v3/internal/terraform/*", mapTerraformV3.HandlerFromMuxWithBaseURL(mapTerraformV3.NewStrictHandler(v.mapTerraform,
		[]mapTerraformV3.StrictMiddlewareFunc{}), nil, "/v3/internal/terraform"))
	r.Handle("/v3/obungus/*", mapObungusV3.HandlerFromMuxWithBaseURL(mapObungusV3.NewStrictHandler(v.mapObungus,
		[]mapObungusV3.StrictMiddlewareFunc{}), nil, "/v3/obungus"))

	r.Handle("/v3/internal/*", intnlV3.HandlerFromMuxWithBaseURL(intnlV3.NewStrictHandler(v.intnl,
		[]intnlV3.StrictMiddlewareFunc{}), nil, "/v3/internal"))

	r.Handle("/posthog/*", v.posthog)

	if v.discord != nil {
		r.Post("/_external/discord", v.discord.OnDiscordWebhook)
	}
}

func makeV2RouteHandler(p struct {
	fx.In

	Public       v2Public.StrictServerInterface
	Internal     v2Internal.StrictServerInterface
	Payments     v2Payments.ServerInterface
	MapPublic    mapPublicV3.StrictServerInterface
	MapIntnl     mapIntnlV3.StrictServerInterface
	MapTerraform mapTerraformV3.StrictServerInterface
	MapObungus   mapObungusV3.StrictServerInterface
	Intnl        intnlV3.StrictServerInterface
	Posthog      *posthogProxy.Proxy
	Discord      *discord.Handler
}) httpTransport.RouteProvider {
	return &routeHandlerImpl{
		public:       p.Public,
		internal:     p.Internal,
		payments:     p.Payments,
		mapPublic:    p.MapPublic,
		mapIntnl:     p.MapIntnl,
		mapTerraform: p.MapTerraform,
		mapObungus:   p.MapObungus,
		intnl:        p.Intnl,
		posthog:      p.Posthog,
		discord:      p.Discord,
	}
}

func newS3Client(conf *config.Config) (*s3.Client, error) {
	appCreds := aws.NewCredentialsCache(awsCredentials.NewStaticCredentialsProvider(conf.S3.AccessKey, conf.S3.SecretKey, ""))

	cfg, err := awsConfig.LoadDefaultConfig(context.TODO(), // todo figure out context since it's created by Fx, there isnt one
		awsConfig.WithRegion(conf.S3.Region),
		awsConfig.WithCredentialsProvider(appCreds),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load aws config: %w", err)
	}

	otelaws.AppendMiddlewares(&cfg.APIOptions)

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String(conf.S3.Endpoint)
	}), nil
}

func newS3Downloader(s3Client *s3.Client) *s3manager.Downloader {
	return s3manager.NewDownloader(s3Client)
}

func newS3Uploader(s3Client *s3.Client) *s3manager.Uploader {
	return s3manager.NewUploader(s3Client)
}

func newDiscordClient(conf *config.Config) (*discordgo.Session, error) {
	if conf.Discord.Token == "" {
		return nil, nil
	}

	s, err := discordgo.New(fmt.Sprintf("Bot %s", conf.Discord.Token))
	if err != nil {
		return nil, fmt.Errorf("failed to create discord client: %w", err)
	}

	// Note that we do not connect to the gateway. The bot is http interactions only for now.

	return s, nil
}

func newGrpcServer(lc fx.Lifecycle, authServer *auth.Server) {
	lis, _ := net.Listen("tcp", ":9001")
	grpcServer := grpc.NewServer()
	envoyAuth.RegisterAuthorizationServer(grpcServer, authServer)
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go grpcServer.Serve(lis)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			grpcServer.GracefulStop()
			return nil
		},
	})
}

func newLaddersFromConfig(conf *config.Config) (map[string]*model.PunishmentLadder, error) {
	return model.ConvertConfigLadders2Model(conf.PunishmentLadders)
}

func newTebexHeadlessClient(conf *config.Config) *tebex.HeadlessClient {
	return tebex.NewHeadlessClientWithOptions(tebex.HeadlessClientParams{
		Url:        tebex.DefaultBaseUrl,
		PrivateKey: conf.Tebex.PrivateKey,
	})
}

func newDynamicExporter(config common.OtlpConfig) (trace.SpanExporter, error) {
	if config.Endpoint != "" {
		return tracefx.NewHttpExporter(config)
	} else {
		// Uncomment the below if you want to print out traces to stdout - for debugging traces
		//return stdouttrace.New(
		//	stdouttrace.WithPrettyPrint(),
		//)
		return tracefx.NewNoopExporter()
	}
}
