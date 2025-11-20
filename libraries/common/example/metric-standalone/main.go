package main

import (
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	"go.uber.org/zap"
)

type Test struct {
	Timestamp time.Time `avro:"time"`
	Name      string    `avro:"name"`
	Count     int       `avro:"count"`
}

func main() {
	l, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(l)

	mw, _ := metric.NewWriter(zap.S(),
		"proxy.metrics.hollowcube.dev:30212",
		"schema-registry.metrics.hollowcube.dev:30212",
		"AtjaJHdN2bh6",
		"bdfnFauVN2Zet4W9BsCgqQyhU8RES3DjJvxrKcz6L7HwAmXPMk")

	mw.Write(&Test{
		Timestamp: time.Now(),
		Name:      "testing_1",
		Count:     125125,
	})
}
