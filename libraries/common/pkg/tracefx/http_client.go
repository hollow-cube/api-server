package tracefx

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// DefaultHTTPClient is a tracing-enabled HTTP client for internal uses.
// It adopts trace headers into the context for requests, and forwards it on via headers for any requests made.
var DefaultHTTPClient = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
