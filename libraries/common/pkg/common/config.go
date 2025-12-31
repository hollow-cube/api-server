package common

import "go.uber.org/fx"

type Config struct {
	fx.Out

	ServiceConfig `mapstructure:",squash"`
	HTTP          HTTPConfig `mapstructure:"http"`
}

type ServiceConfig struct {
	Env string `yaml:"env" json:"env" mapstructure:"env"`

	Name string `yaml:"name" json:"name" mapstructure:"name"`
}

type HTTPConfig struct {
	Address string `yaml:"address" json:"address" mapstructure:"address"`
	Port    int    `yaml:"port" json:"port" mapstructure:"port"`
}

type OtlpConfig struct {
	Endpoint string `yaml:"endpoint" json:"endpoint" mapstructure:"endpoint"`
}

type KafkaConfig struct {
	Brokers string `mapstructure:"brokers"`
}
