package util

import (
	"github.com/prometheus/client_golang/prometheus"
	"time"
)

func CurrentTime() time.Time {
	return time.Now() //todo mock for tests
}

func Observe(metric prometheus.Histogram, now time.Time) {
	metric.Observe(time.Since(now).Seconds())
}
