package main

import (
	"context"
	"time"

	"github.com/hollow-cube/hc-services/services/session-service/config"
	"github.com/hollow-cube/hc-services/services/session-service/internal/db"
	"github.com/hollow-cube/hc-services/services/session-service/internal/mapdb"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/common"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/metric"
	posthog2 "github.com/hollow-cube/hc-services/services/session-service/internal/pkg/posthog"
	"github.com/hollow-cube/hc-services/services/session-service/internal/playerdb"
	"github.com/posthog/posthog-go"
	"github.com/redis/rueidis"
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
		Service: common.ServiceConfig{Name: "session-service", Env: conf.Env},
		HTTP:    conf.HTTP,
		OTLP:    conf.OTLP,
	}
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

func newSessionPostgresStore(lc fx.Lifecycle, conf *config.Config) (*db.Queries, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	queries, pool, err := db.NewQuerySet(ctx, conf.Postgres.URI)
	if err != nil {
		return nil, err
	}

	lc.Append(fx.StopHook(pool.Close))

	return queries, nil
}

func newPlayerPostgresStore(conf *config.Config, metrics metric.Writer, lc fx.Lifecycle) (*playerdb.Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	store, pool, err := playerdb.NewQuerySet(ctx, metrics, conf.Postgres.PlayersURI)
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
