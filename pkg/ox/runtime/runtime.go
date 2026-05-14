package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strconv"

	"github.com/hollow-cube/api-server/pkg/ox"
	"go.uber.org/zap"
)

// WriteJSON writes v as JSON with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// WriteText writes v as plain text with the given status code.
func WriteText(w http.ResponseWriter, status int, v string) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(status)
	w.Write([]byte(v))
}

// WriteStream writes a binary/streaming response. ContentType defaults to
// application/octet-stream if empty. Content-Length is written only when
// s.ContentLength > 0. If s.Body implements io.Closer it is closed after the
// copy. Once headers are written there is no recovery from a copy failure;
// failures are logged.
func WriteStream(w http.ResponseWriter, status int, s *ox.Stream) {
	if s == nil || s.Body == nil {
		zap.S().Errorw("stream response has nil body")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	ct := s.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	if s.ContentLength > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(s.ContentLength, 10))
	}
	w.WriteHeader(status)
	if c, ok := s.Body.(io.Closer); ok {
		defer c.Close()
	}
	if _, err := io.Copy(w, s.Body); err != nil {
		zap.S().Warnw("stream copy failed mid-response", "error", err)
	}
}

// HandleError writes an HTTP error response. If err implements HTTPError,
// its status code is used. Otherwise a 500 is returned.
func HandleError(w http.ResponseWriter, err error) {
	var httpErr ox.HTTPError
	if errors.As(err, &httpErr) {
		WriteJSON(w, httpErr.StatusCode(), map[string]string{
			"error": httpErr.Error(),
		})
		return
	}
	// TODO: should give info about what route and everything
	zap.S().Errorw("internal server error", "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// WriteBadRequest writes a 400 response with the given message.
func WriteBadRequest(w http.ResponseWriter, msg string) {
	WriteJSON(w, http.StatusBadRequest, map[string]string{
		"error": msg,
	})
}

// DecodeJSON decodes the request body as JSON into v.
func DecodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// WriteSSE streams events as a text/event-stream response. The response is
// committed with status 200 as soon as the first byte is written; there is no
// way to surface an HTTP error after this. If the iterator yields a non-nil
// error, the connection is closed and clients will reconnect per standard
// SSE retry semantics.
func WriteSSE[T any](w http.ResponseWriter, r *http.Request, seq iter.Seq2[ox.Event[T], error]) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		zap.S().Errorw("SSE response writer does not support flushing")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for ev, err := range seq {
		if err != nil {
			zap.S().Warnw("SSE stream ended with error", "error", err)
			return
		}
		if err := writeSSEFrame(w, ev); err != nil {
			zap.S().Warnw("SSE write failed", "error", err)
			return
		}
		flusher.Flush()
	}
}

func writeSSEFrame[T any](w io.Writer, ev ox.Event[T]) error {
	if ev.ID != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", ev.ID); err != nil {
			return err
		}
	}
	if ev.Name != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", ev.Name); err != nil {
			return err
		}
	}
	if ev.Retry > 0 {
		if _, err := fmt.Fprintf(w, "retry: %d\n", ev.Retry.Milliseconds()); err != nil {
			return err
		}
	}
	data, err := json.Marshal(ev.Data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}
