package server

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	mapIsolateCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "hc_map_isolate_count",
			Help: "Number of map isolate pods by state",
		},
		[]string{"state"},
	)

	mapIsolateCreationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "hc_map_isolate_creation_duration_seconds",
			Help:    "Duration of map isolate pod creation in seconds",
			Buckets: []float64{0.25, 0.5, 0.75, 1, 1.5, 2, 3, 5, 10},
		},
	)
)

func init() {
	prometheus.MustRegister(mapIsolateCount)
	prometheus.MustRegister(mapIsolateCreationDuration)
}
