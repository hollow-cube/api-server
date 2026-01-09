package http

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
			path := extractRoutePattern(r)

			reqs.WithLabelValues(code, r.Method, path).Inc()
			latency.WithLabelValues(code, r.Method, path).Observe(time.Since(start).Seconds())
		})
	}
}

// TraceNameMiddleware adds additional information that is not available until after the req is processed by Chi.
// - sets the span name to the METHOD + routePattern
// - adds span attributes for path and query parameters.
func TraceNameMiddleware() MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			span := trace.SpanFromContext(r.Context())
			span.SetName(r.Method + " " + extractRoutePattern(r))

			// Add path parameters as span attributes
			rctx := chi.RouteContext(r.Context())
			if rctx != nil {
				for i, key := range rctx.URLParams.Keys {
					if key != "*" && i < len(rctx.URLParams.Values) {
						span.SetAttributes(attribute.String("http.request.path_param."+key, rctx.URLParams.Values[i]))
					}
				}
			}

			// Add query parameters as span attributes
			for key, values := range r.URL.Query() {
				if len(values) == 1 {
					span.SetAttributes(attribute.String("http.request.query_param."+key, values[0]))
				} else {
					span.SetAttributes(attribute.StringSlice("http.request.query_param."+key, values))
				}
			}

			// Don't log an error for NotFound and BadRequest as they are expected in our CRUD use-case
			if ww.Status() == 404 || ww.Status() == 409 {
				span.SetStatus(codes.Ok, "")
			}
		})
	}
}

func extractRoutePattern(r *http.Request) string {
	path := chi.RouteContext(r.Context()).RoutePattern()
	if r.Pattern != "" && strings.Contains(r.Pattern, " ") {
		path = strings.Split(r.Pattern, " ")[1]
	}
	return path
}
