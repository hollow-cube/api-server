package ox

import "io"

// Stream is a binary/streaming response. Handlers return *Stream to write raw
// bytes with a specific content type instead of JSON.
//
// ContentType is written verbatim to the Content-Type header. If empty, the
// runtime defaults to "application/octet-stream". The value should match one
// of the types declared via //ox:produces on the handler.
//
// ContentLength, if > 0, is written to the Content-Length header. Pass 0 if
// the length is unknown.
//
// ETag, if non-empty, is written verbatim to the ETag header. It must already
// be a valid header value (a quoted string, optionally W/-prefixed).
//
// Headers are written before the typed fields above. ContentType,
// ContentLength, and ETag take precedence over values with the same names.
//
// StatusCode, if non-zero, overrides the status inferred by the generator.
// This is intended for endpoints whose successful status depends on the
// request, such as a create-or-update operation or a byte-range response.
//
// If Body implements io.Closer, the runtime closes it after writing. Body
// ownership transfers to the runtime when the handler returns successfully.
type Stream struct {
	ContentType   string
	Body          io.Reader
	ContentLength int64
	ETag          string
	Headers       map[string]string
	StatusCode    int
}
