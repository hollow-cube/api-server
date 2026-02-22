package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	awsCredentials "github.com/aws/aws-sdk-go-v2/credentials"
	s3manager "github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bwmarrin/discordgo"
	"github.com/go-chi/chi/v5"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/natsutil"
	posthog2 "github.com/hollow-cube/hc-services/libraries/common/pkg/posthog"
	"github.com/hollow-cube/hc-services/services/player-service/api/auth"
	mapIntnlV3 "github.com/hollow-cube/hc-services/services/player-service/api/mapsV3/intnl"
	mapObungusV3 "github.com/hollow-cube/hc-services/services/player-service/api/mapsV3/obungus"
	mapPublicV3 "github.com/hollow-cube/hc-services/services/player-service/api/mapsV3/public"
	mapTerraformV3 "github.com/hollow-cube/hc-services/services/player-service/api/mapsV3/terraform"
	"github.com/hollow-cube/hc-services/services/player-service/internal/consumers"
	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/player-service/internal/discord"
	"github.com/hollow-cube/hc-services/services/player-service/internal/mapdb"
	"github.com/hollow-cube/hc-services/services/player-service/internal/object"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/hollow-cube/tebex-go"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/posthog/posthog-go"
	"github.com/redis/rueidis"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
	"google.golang.org/grpc"

	envoyAuth "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	httpTransport "github.com/hollow-cube/hc-services/libraries/common/pkg/http"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/httpfx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/tracefx"
	posthogProxy "github.com/hollow-cube/hc-services/services/player-service/api/posthog"
	v2Internal "github.com/hollow-cube/hc-services/services/player-service/api/v2/intnl"
	v2Payments "github.com/hollow-cube/hc-services/services/player-service/api/v2/payments"
	v2Public "github.com/hollow-cube/hc-services/services/player-service/api/v2/public"
	"github.com/hollow-cube/hc-services/services/player-service/config"
	"go.opentelemetry.io/otel/sdk/trace"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
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

		fx.Provide(newPostgresStore, newMapsPostgresStore),
		fx.Provide(newRedisClient),
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

		fx.Provide(newPosthogClient, metric.NewPosthogWriter),
		fx.Provide(newTebexHeadlessClient),

		// Converted punishment ladders - for internal handler
		fx.Provide(newLaddersFromConfig),

		fx.Provide(newDiscordClient, discord.NewHandler),

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

			posthogProxy.NewProxy,
			httpfx.AsRouteProvider(makeV2RouteHandler),
		),
		httpfx.Module,

		// Generic consumer (e.g., denormalized data from other services)
		fx.Invoke(consumers.NewConsumerSet),

		// GRPC server (for Envoy)
		fx.Provide(auth.NewServer),
		fx.Invoke(newGrpcServer),
	).Run()
}

type v2RouteHandlerImpl struct {
	public   v2Public.StrictServerInterface
	internal v2Internal.StrictServerInterface
	payments v2Payments.ServerInterface

	mapPublic    mapPublicV3.StrictServerInterface
	mapIntnl     mapIntnlV3.StrictServerInterface
	mapTerraform mapTerraformV3.StrictServerInterface
	mapObungus   mapObungusV3.StrictServerInterface

	posthog *posthogProxy.Proxy

	discord *discord.Handler
}

func (v *v2RouteHandlerImpl) Apply(r chi.Router) {
	r.Handle("/v2/players/*", v2Public.HandlerFromMuxWithBaseURL(v2Public.NewStrictHandler(v.public, nil), nil, "/v2/players"))
	r.Handle("/v2/internal/*", v2Internal.HandlerFromMuxWithBaseURL(v2Internal.NewStrictHandler(v.internal, nil), nil, "/v2/internal"))
	r.Handle("/v2/payments/*", v2Payments.HandlerFromMuxWithBaseURL(v.payments, nil, "/v2/payments"))

	r.Handle("/v3/maps/*", mapPublicV3.HandlerFromMuxWithBaseURL(mapPublicV3.NewStrictHandler(v.mapPublic,
		[]mapPublicV3.StrictMiddlewareFunc{mapPublicV3.AuthMiddleware}), nil, "/v3/maps"))
	r.Handle("/v3/internal/*", mapIntnlV3.HandlerFromMuxWithBaseURL(mapIntnlV3.NewStrictHandler(v.mapIntnl,
		[]mapIntnlV3.StrictMiddlewareFunc{mapIntnlV3.AuthMiddleware}), nil, "/v3/internal"))
	r.Handle("/v3/internal/terraform/*", mapTerraformV3.HandlerFromMuxWithBaseURL(mapTerraformV3.NewStrictHandler(v.mapTerraform,
		[]mapTerraformV3.StrictMiddlewareFunc{}), nil, "/v3/internal/terraform"))
	r.Handle("/v3/obungus/*", mapObungusV3.HandlerFromMuxWithBaseURL(mapObungusV3.NewStrictHandler(v.mapObungus,
		[]mapObungusV3.StrictMiddlewareFunc{}), nil, "/v3/obungus"))

	r.Handle("/posthog/*", v.posthog)

	if v.discord != nil {
		r.Post("/_external/discord", v.discord.OnDiscordWebhook)
	}
}

func makeV2RouteHandler(params struct {
	fx.In

	Public   v2Public.StrictServerInterface
	Internal v2Internal.StrictServerInterface
	Payments v2Payments.ServerInterface

	MapPublicV3    mapPublicV3.StrictServerInterface
	MapIntnlV3     mapIntnlV3.StrictServerInterface
	MapTerraformV3 mapTerraformV3.StrictServerInterface
	MapObungusV3   mapObungusV3.StrictServerInterface

	Posthog *posthogProxy.Proxy

	Discord *discord.Handler `optional:"true"`
}) httpTransport.RouteProvider {
	return &v2RouteHandlerImpl{
		public:   params.Public,
		internal: params.Internal,
		payments: params.Payments,

		mapPublic:    params.MapPublicV3,
		mapIntnl:     params.MapIntnlV3,
		mapTerraform: params.MapTerraformV3,
		mapObungus:   params.MapObungusV3,

		posthog: params.Posthog,

		discord: params.Discord,
	}
}

func newZapLogger(conf *config.Config) (*zap.Logger, error) {
	if conf.Env == "prod" {
		return zap.NewProduction()
	}
	return zap.NewDevelopment()
}

func newZapSugared(log *zap.Logger) *zap.SugaredLogger {
	zap.ReplaceGlobals(log)
	return log.Sugar()
}

type CommonConfigResources struct {
	fx.Out

	Service common.ServiceConfig
	HTTP    common.HTTPConfig
	OTLP    common.OtlpConfig
}

func newCommonConfigResources(conf *config.Config) CommonConfigResources {
	return CommonConfigResources{
		Service: common.ServiceConfig{Name: "player-service"},
		HTTP:    conf.HTTP,
		OTLP:    conf.OTLP,
	}
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

func newPostgresStore(conf *config.Config, metrics metric.Writer, lc fx.Lifecycle) (*db.Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	store, pool, err := db.NewQuerySet(ctx, metrics, conf.Postgres.URI)
	if err != nil {
		return nil, err
	}

	lc.Append(fx.StopHook(pool.Close))

	return store, nil
}

func newMapsPostgresStore(conf *config.Config, metrics metric.Writer, lc fx.Lifecycle) (*mapdb.Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	store, pool, err := mapdb.NewQuerySet(ctx, metrics, conf.Postgres.MapsURI)
	if err != nil {
		return nil, err
	}

	lc.Append(fx.StopHook(pool.Close))

	return store, nil
}

func newPosthogClient(conf *config.Config, log *zap.SugaredLogger, lc fx.Lifecycle) (posthog.Client, error) {
	apiKey := "phc_mK0jji1aC3hvMBGLOLjuVARqolDGPS9AiuNUOhMwVyA" // Not a secret, included on website

	client, err := posthog.NewWithConfig(apiKey, posthog.Config{
		Endpoint:       conf.Posthog.Endpoint,
		PersonalApiKey: conf.Posthog.PersonalApiKey,
	})
	if err != nil {
		return nil, err
	}

	if conf.Env == "tilt" {
		log.Info("dropping posthog client because tilt is enabled")
		posthog2.InitFixedValue(true)
		apiKey = ""
		return client, nil
	}

	nonLocalClient, err := posthog.NewWithConfig(apiKey, posthog.Config{
		Endpoint: conf.Posthog.Endpoint,
	})
	if err != nil {
		return nil, err
	}

	lc.Append(fx.StopHook(client.Close))
	lc.Append(fx.StopHook(nonLocalClient.Close))
	posthog2.Init(client, nonLocalClient)
	return client, nil
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

func newRedisClient(lc fx.Lifecycle, conf *config.Config) (rueidis.Client, error) {
	c, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{conf.Redis.Address},
	})
	if err != nil {
		return nil, err
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return c.Do(ctx, c.B().Ping().Build()).Error()
		},
		OnStop: func(_ context.Context) error {
			c.Close()
			return nil
		},
	})

	return c, nil
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
