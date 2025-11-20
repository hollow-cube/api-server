package main

import (
	"context"
	"strings"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"github.com/hollow-cube/hc-services/services/session/config"
	"github.com/hollow-cube/hc-services/services/session/internal/db"
	"github.com/hollow-cube/hc-services/services/session/internal/pkg/authz"
	posthog2 "github.com/hollow-cube/hc-services/services/session/internal/pkg/posthog"
	"github.com/hollow-cube/hc-services/services/session/internal/pkg/wkafka"
	"github.com/posthog/posthog-go"
	"github.com/redis/rueidis"
	"github.com/segmentio/kafka-go"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

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
		Service: common.ServiceConfig{Name: "session-service"},
		HTTP:    conf.HTTP,
		OTLP:    conf.OTLP,
	}
}

func newSyncKafkaWriter(conf *config.Config, lc fx.Lifecycle, log *zap.SugaredLogger) wkafka.SyncWriter {
	w := &kafka.Writer{
		Addr:                   kafka.TCP(strings.Split(conf.Kafka.Brokers, ",")...),
		Balancer:               &kafka.Hash{},
		Async:                  false,
		AllowAutoTopicCreation: true,

		WriteBackoffMin: 20 * time.Millisecond,
		WriteBackoffMax: 100 * time.Millisecond,
		BatchTimeout:    100 * time.Millisecond,

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
			GroupID:  "session-service",
			Topic:    topic,
			MaxBytes: 10e6, // 10mb
			//Logger:      kafka.LoggerFunc(log.Infof),
			ErrorLogger: kafka.LoggerFunc(log.Errorf),
		})
		lc.Append(fx.StopHook(r.Close))
		return r
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

func newAuthzSpiceDB(conf *config.Config) (authz.Client, error) {
	return authz.NewSpiceDBClient(
		conf.SpiceDB.Endpoint,
		conf.SpiceDB.Token,
		conf.SpiceDB.TLS,
	)
}

func setupPosthogClient(conf *config.Config, log *zap.SugaredLogger, lc fx.Lifecycle) error {
	// Making 2 clients here is kinda cursed, but some cases like maintenance we want to always eval the flag
	// remotely no matter what and the normal client doesnt support that.

	apiKey := "phc_mK0jji1aC3hvMBGLOLjuVARqolDGPS9AiuNUOhMwVyA" // Not a secret, included on website
	if conf.Env == "tilt" {
		log.Info("dropping posthog api key because we are in tilt")
		posthog2.InitFixedValue(true)
		return nil
	}

	client, err := posthog.NewWithConfig(apiKey, posthog.Config{
		PersonalApiKey: conf.Posthog.PersonalApiKey,
		Endpoint:       "https://us.i.posthog.com",
	})
	if err != nil {
		return err
	}

	nonLocalClient, err := posthog.NewWithConfig(apiKey, posthog.Config{
		Endpoint: "https://us.i.posthog.com",
	})
	if err != nil {
		return err
	}

	lc.Append(fx.StopHook(client.Close))
	lc.Append(fx.StopHook(nonLocalClient.Close))
	posthog2.Init(client, nonLocalClient)
	return nil
}

func newDbQuerySet(lc fx.Lifecycle, conf *config.Config) (*db.Queries, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	queries, pool, err := db.NewQuerySet(ctx, conf.Postgres.URI)
	if err != nil {
		return nil, err
	}

	lc.Append(fx.StopHook(pool.Close))

	return queries, nil
}
