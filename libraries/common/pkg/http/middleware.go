package http

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type Middleware interface {
	Run(next http.Handler) http.Handler
}
type MiddlewareProvider interface {
	Provide(r chi.Router) Middleware
}

type MiddlewareFunc func(next http.Handler) http.Handler

func (m MiddlewareFunc) Run(next http.Handler) http.Handler {
	return m(next)
}

type MiddlewareProviderFunc func(r chi.Router) Middleware

func (m MiddlewareProviderFunc) Provide(r chi.Router) Middleware {
	return m(r)
}

func ZapMiddleware(log *zap.SugaredLogger) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			t1 := time.Now()
			defer func() {
				reqLogger := log.With(
					zap.String("proto", r.Proto),
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.String("reqId", middleware.GetReqID(r.Context())),
					zap.Duration("lat", time.Since(t1)),
					zap.Int("status", ww.Status()),
					zap.Int("size", ww.BytesWritten()),
				)

				span := trace.SpanFromContext(r.Context())
				if span.IsRecording() {
					reqLogger = reqLogger.With(
						zap.String("trace", span.SpanContext().TraceID().String()),
					)
				}

				ua := ww.Header().Get("User-Agent")
				if ua == "" {
					ua = r.Header.Get("User-Agent")
				}
				if ua != "" {
					reqLogger = reqLogger.With(zap.String("ua", ua))
				}

				isHealthCheck := strings.HasSuffix(r.URL.Path, "/alive") || strings.HasSuffix(r.URL.Path, "/ready")
				if !isHealthCheck || ww.Status() != http.StatusOK {
					reqLogger.Info("Served")
				}
			}()
			next.ServeHTTP(ww, r)
		}
		return http.HandlerFunc(fn)
	}
}

var defaultBuckets = []float64{0.05, 0.1, 0.2, 0.5, 1, 5}

func PrometheusMiddleware() MiddlewareFunc {
	reqs := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "How many HTTP requests processed, partitioned by status code, method and HTTP path.",
		},
		[]string{"code", "method", "path"},
	)
	prometheus.Unregister(reqs)
	prometheus.MustRegister(reqs)

	latency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "How long it took to process the request, partitioned by status code, method and HTTP path.",
		Buckets: defaultBuckets,
	},
		[]string{"code", "method", "path"},
	)
	prometheus.Unregister(latency)
	prometheus.MustRegister(latency)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			code := strconv.Itoa(ww.Status())
			path := chi.RouteContext(r.Context()).RoutePattern()
			if r.Pattern != "" && strings.Contains(r.Pattern, " ") {
				path = strings.Split(r.Pattern, " ")[1]
			}
			reqs.WithLabelValues(code, r.Method, path).Inc()
			latency.WithLabelValues(code, r.Method, path).Observe(time.Since(start).Seconds())
		})
	}
}
