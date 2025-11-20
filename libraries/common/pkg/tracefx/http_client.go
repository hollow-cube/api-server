package tracefx

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

var DefaultHTTPClient = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
