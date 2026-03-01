package zapfx

import (
	"github.com/hollow-cube/api-server/internal/pkg/common"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var Module = fx.Module("zap",
	fx.Provide(
		NewZapLogger,
		NewZapSugared,
	),
)

func NewZapLogger(conf common.ServiceConfig) (*zap.Logger, error) {
	if conf.Env == "prod" {
		return zap.NewProduction()
	}
	return zap.NewDevelopment()
}

func NewZapSugared(log *zap.Logger) *zap.SugaredLogger {
	zap.ReplaceGlobals(log)
	return log.Sugar()
}
