package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	awsCredentials "github.com/aws/aws-sdk-go-v2/credentials"
	s3manager "github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	httpTransport "github.com/hollow-cube/hc-services/libraries/common/pkg/http"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/httpfx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/tracefx"
	intnlV3 "github.com/hollow-cube/hc-services/services/map-service/api/v3/intnl"
	obungusV3 "github.com/hollow-cube/hc-services/services/map-service/api/v3/obungus"
	publicV3 "github.com/hollow-cube/hc-services/services/map-service/api/v3/public"
	terraformV3 "github.com/hollow-cube/hc-services/services/map-service/api/v3/terraform"
	"github.com/hollow-cube/hc-services/services/map-service/config"
	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/object"
	oapi_rt "github.com/mworzala/openapi-go/pkg/oapi-rt"
	"github.com/posthog/posthog-go"
	"github.com/redis/go-redis/v9"
	"github.com/redis/rueidis"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
	"go.opentelemetry.io/otel/sdk/trace"

	v1 "github.com/hollow-cube/hc-services/services/map-service/api/v1"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/handler"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

const serviceName = "map-service"

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

		// Storage
		fx.Provide(newPostgresStore),

		// Authz
		fx.Provide(newAuthzSpiceDB),

		// Kafka
		kafkafx.Module,

		// Metrics
		fx.Provide(newPosthogClient, metric.NewPosthogWriter),

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

		fx.Provide(newRedisClient, newRueidisClient),

		// HTTP server
		fx.Provide(newDynamicExporter),
		tracefx.Module,
		fx.Provide(
			fx.Annotate(
				func() *v1.AuthorizerMiddleware {
					return &v1.AuthorizerMiddleware{}
				},
				fx.As(new(oapi_rt.Middleware)),
				fx.ResultTags(`group:"internal_middleware"`),
			),
			handler.NewInternalHandler,
			httpfx.AsRouteProvider(v1.NewInternalServerWrapper),

			publicV3.NewServer,
			intnlV3.NewServer,
			terraformV3.NewServer,
			obungusV3.NewServer,
			httpfx.AsRouteProvider(makeV2RouteHandler),
		),
		httpfx.Module,
	).Run()
}

type v2RouteHandlerImpl struct {
	public    publicV3.StrictServerInterface
	intnl     intnlV3.StrictServerInterface
	terraform terraformV3.StrictServerInterface
	obungus   obungusV3.StrictServerInterface
}

func (v *v2RouteHandlerImpl) Apply(r chi.Router) {
	r.Handle("/v3/maps/*", publicV3.HandlerFromMuxWithBaseURL(publicV3.NewStrictHandler(v.public,
		[]intnlV3.StrictMiddlewareFunc{publicV3.AuthMiddleware}), nil, "/v3/maps"))
	r.Handle("/v3/internal/*", intnlV3.HandlerFromMuxWithBaseURL(intnlV3.NewStrictHandler(v.intnl,
		[]intnlV3.StrictMiddlewareFunc{intnlV3.AuthMiddleware}), nil, "/v3/internal"))
	r.Handle("/v3/internal/terraform/*", terraformV3.HandlerFromMuxWithBaseURL(terraformV3.NewStrictHandler(v.terraform,
		[]terraformV3.StrictMiddlewareFunc{}), nil, "/v3/internal/terraform"))
	r.Handle("/v3/obungus/*", obungusV3.HandlerFromMuxWithBaseURL(obungusV3.NewStrictHandler(v.obungus,
		[]obungusV3.StrictMiddlewareFunc{}), nil, "/v3/obungus"))
}

func makeV2RouteHandler(
	public publicV3.StrictServerInterface,
	intnl intnlV3.StrictServerInterface,
	terraform terraformV3.StrictServerInterface,
	obungus obungusV3.StrictServerInterface,
) httpTransport.RouteProvider {
	return &v2RouteHandlerImpl{public, intnl, terraform, obungus}
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
	Kafka   common.KafkaConfig
}

func newCommonConfigResources(conf *config.Config) CommonConfigResources {
	return CommonConfigResources{
		Service: common.ServiceConfig{Name: "map-service"},
		HTTP:    conf.HTTP,
		OTLP:    conf.OTLP,
		Kafka:   conf.Kafka,
	}
}

func newDynamicExporter(config common.OtlpConfig) (trace.SpanExporter, error) {
	if config.Endpoint != "" {
		return tracefx.NewHttpExporter(config)
	} else {
		return tracefx.NewNoopExporter()
	}
}

func newAuthzSpiceDB(conf *config.Config) (authz.Client, error) {
	return authz.NewSpiceDBClient(
		conf.SpiceDB.Endpoint,
		conf.SpiceDB.Token,
		conf.SpiceDB.TLS,
	)
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

func newRedisClient(conf *config.Config) (*redis.Client, error) {
	c := redis.NewClient(&redis.Options{
		Addr: conf.Redis.Address,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return c, c.Ping(ctx).Err()
}

func newPosthogClient(conf *config.Config, log *zap.SugaredLogger, lc fx.Lifecycle) (posthog.Client, error) {
	apiKey := "phc_mK0jji1aC3hvMBGLOLjuVARqolDGPS9AiuNUOhMwVyA" // Not a secret, included on website
	if conf.Env == "tilt" {
		log.Info("dropping posthog key because we are in tilt")
		apiKey = ""
	}

	client, err := posthog.NewWithConfig(apiKey, posthog.Config{
		Endpoint:       conf.Posthog.Endpoint,
		PersonalApiKey: conf.Posthog.PersonalApiKey,
	})
	if err != nil {
		return nil, err
	}

	lc.Append(fx.StopHook(client.Close))
	return client, nil
}

func newRueidisClient(lc fx.Lifecycle, conf *config.Config) (rueidis.Client, error) {
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
