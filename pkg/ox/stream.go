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
// If Body implements io.Closer, the runtime closes it after writing. Body
// ownership transfers to the runtime when the handler returns successfully.
type Stream struct {
	ContentType   string
	Body          io.Reader
	ContentLength int64
}
