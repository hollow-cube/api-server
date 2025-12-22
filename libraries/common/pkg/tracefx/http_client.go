package tracefx

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// DefaultHTTPClient is a tracing-enabled HTTP client for internal uses.
// It adopts trace headers into the context for requests, and forwards it on via headers for any requests made.
var DefaultHTTPClient = &http.Client{
	Transport: &clientTransport{
		rt: otelhttp.NewTransport(http.DefaultTransport),
		okStatusCodes: map[int]bool{
			http.StatusNotFound: true,
			http.StatusConflict: true,
		},
	},
}

type clientTransport struct {
	rt http.RoundTripper

	// okStatusCodes are codes that aren't usually considered successful (4xx/5xx), but should be.
	// Not included status codes are ignored and use whatever OTEL set as the status code.
	okStatusCodes map[int]bool
}

func (t *clientTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.rt.RoundTrip(req)

	if err == nil && t.okStatusCodes[resp.StatusCode] {
		span := trace.SpanFromContext(req.Context())
		span.SetStatus(codes.Ok, "")
	}

	return resp, err
}
