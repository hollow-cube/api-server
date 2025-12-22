package main

import (
	"encoding/json"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	httpTransport "github.com/hollow-cube/hc-services/libraries/common/pkg/http"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/httpfx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/tracefx"
	v2Internal "github.com/hollow-cube/hc-services/services/player-service/api/v2/intnl"
	v2Payments "github.com/hollow-cube/hc-services/services/player-service/api/v2/payments"
	v2Public "github.com/hollow-cube/hc-services/services/player-service/api/v2/public"
	"github.com/hollow-cube/hc-services/services/player-service/config"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/handler"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/storage"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/wkafka"
	"github.com/hollow-cube/hc-services/services/player-service/pkg/kafkafx"
	"github.com/hollow-cube/tebex-go"
	"github.com/posthog/posthog-go"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/sdk/trace"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

const serviceName = "player-service"

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

		fx.Provide(newStoragePostgres),
		fx.Provide(newAuthzSpiceDB),
		fx.Provide(newSyncKafkaWriter, newKafkaReaderFactory),
		fx.Provide(newPosthogClient, metric.NewPosthogWriter),
		fx.Provide(newTebexHeadlessClient),

		// Kafka
		fx.Provide(kafkafx.NewWriter),

		// Converted punishment ladders - for internal handler
		fx.Provide(newLaddersFromConfig),

		// HTTP server
		fx.Provide(newDynamicExporter),
		tracefx.Module,
		fx.Provide(
			v2Public.NewServer,
			v2Internal.NewServer,
			v2Payments.NewServer,
			httpfx.AsRouteProvider(makeV2RouteHandler),
		),
		httpfx.Module,

		// Vote handler
		fx.Provide(handler.NewVoteHandler),

		// Init tasks
		fx.Invoke(func(*handler.VoteHandler) {}),
	).Run()
}

type v2RouteHandlerImpl struct {
	public   v2Public.StrictServerInterface
	internal v2Internal.StrictServerInterface
	payments v2Payments.ServerInterface
}

func (v *v2RouteHandlerImpl) Apply(r chi.Router) {
	r.Handle("/v2/players/*", v2Public.HandlerFromMuxWithBaseURL(v2Public.NewStrictHandler(v.public, nil), nil, "/v2/players"))
	r.Handle("/v2/internal/*", v2Internal.HandlerFromMuxWithBaseURL(v2Internal.NewStrictHandler(v.internal, nil), nil, "/v2/internal"))
	r.Handle("/v2/payments/*", v2Payments.HandlerFromMuxWithBaseURL(v.payments, nil, "/v2/payments"))
}

func makeV2RouteHandler(
	public v2Public.StrictServerInterface,
	internal v2Internal.StrictServerInterface,
	payments v2Payments.ServerInterface,
) httpTransport.RouteProvider {
	return &v2RouteHandlerImpl{public, internal, payments}
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

func newStoragePostgres(conf *config.Config, lc fx.Lifecycle, metrics metric.Writer) (storage.Client, error) {
	c, err := storage.NewPostgresClient(conf.Postgres.URI, metrics)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStart: c.Start, OnStop: c.Shutdown})
	return c, nil
}

func newAuthzSpiceDB(conf *config.Config) (authz.Client, error) {
	return authz.NewSpiceDBClient(
		conf.SpiceDB.Endpoint,
		conf.SpiceDB.Token,
		conf.SpiceDB.TLS,
	)
}

func newSyncKafkaWriter(conf *config.Config, lc fx.Lifecycle, log *zap.SugaredLogger) wkafka.SyncWriter {
	w := &kafka.Writer{
		Addr:                   kafka.TCP(strings.Split(conf.Kafka.Brokers, ",")...),
		Balancer:               &kafka.Hash{},
		Async:                  false,
		AllowAutoTopicCreation: true,
		//Logger:                 kafka.LoggerFunc(log.Infof),
		ErrorLogger: kafka.LoggerFunc(log.Errorf),
	}

	lc.Append(fx.StopHook(w.Close))
	return w
}

func newKafkaReaderFactory(conf *config.Config, lc fx.Lifecycle, log *zap.SugaredLogger) wkafka.ReaderFactory {
	brokers := strings.Split(conf.Kafka.Brokers, ",")
	return wkafka.ReaderFactoryFunc(func(topic string) wkafka.Reader {
		r := kafka.NewReader(kafka.ReaderConfig{
			Brokers:  brokers,
			GroupID:  serviceName,
			Topic:    topic,
			MaxBytes: 10e6, // 10mb
			//Logger:      kafka.LoggerFunc(log.Infof),
			ErrorLogger: kafka.LoggerFunc(log.Errorf),
		})
		lc.Append(fx.StopHook(r.Close))
		return r
	})
}

func newPosthogClient(conf *config.Config, log *zap.SugaredLogger, lc fx.Lifecycle) (posthog.Client, error) {
	apiKey := "phc_mK0jji1aC3hvMBGLOLjuVARqolDGPS9AiuNUOhMwVyA" // Not a secret, included on website
	if conf.Env == "tilt" {
		log.Warn("dropping posthog client because tilt is enabled")
		apiKey = ""
	}

	client, err := posthog.NewWithConfig(apiKey, posthog.Config{
		PersonalApiKey: conf.Posthog.PersonalApiKey,
		Endpoint:       "https://us.i.posthog.com",
	})
	if err != nil {
		return nil, err
	}

	lc.Append(fx.StopHook(client.Close))
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
