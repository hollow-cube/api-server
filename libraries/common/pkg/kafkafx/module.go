package kafkafx

import "go.uber.org/fx"

var Module = fx.Module("kafka",
	fx.Provide(NewSyncKafkaProducer),
	fx.Provide(NewAsyncKafkaProducer),
	fx.Provide(NewConsumer),
)
