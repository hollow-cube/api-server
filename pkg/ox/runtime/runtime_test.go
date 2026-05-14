package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hollow-cube/api-server/pkg/ox"
	"github.com/stretchr/testify/require"
)

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		value      any
		wantStatus int
		wantBody   string
	}{
		{
			name:       "string value",
			status:     200,
			value:      "hello",
			wantStatus: 200,
			wantBody:   `"hello"`,
		},
		{
			name:       "map value",
			status:     200,
			value:      map[string]string{"key": "val"},
			wantStatus: 200,
			wantBody:   `{"key":"val"}`,
		},
		{
			name:       "nil value",
			status:     200,
			value:      nil,
			wantStatus: 200,
			wantBody:   `null`,
		},
		{
			name:       "slice value",
			status:     200,
			value:      []int{1, 2, 3},
			wantStatus: 200,
			wantBody:   `[1,2,3]`,
		},
		{
			name:       "created status",
			status:     201,
			value:      map[string]int{"id": 1},
			wantStatus: 201,
			wantBody:   `{"id":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteJSON(w, tt.status, tt.value)

			require.Equal(t, tt.wantStatus, w.Code)
			require.Equal(t, "application/json", w.Header().Get("Content-Type"))

			// json.Encoder appends a newline
			got := w.Body.String()
			require.JSONEq(t, tt.wantBody, got)
		})
	}
}

func TestHandleError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantJSON   bool
		wantError  string
	}{
		{
			name:       "NotFound",
			err:        ox.NotFound{},
			wantStatus: 404,
			wantJSON:   true,
			wantError:  "not found",
		},
		{
			name:       "BadRequest",
			err:        ox.BadRequest{},
			wantStatus: 400,
			wantJSON:   true,
			wantError:  "bad request",
		},
		{
			name:       "Conflict",
			err:        ox.Conflict{},
			wantStatus: 409,
			wantJSON:   true,
			wantError:  "conflict",
		},
		{
			name:       "Unauthorized",
			err:        ox.Unauthorized{},
			wantStatus: 401,
			wantJSON:   true,
			wantError:  "unauthorized",
		},
		{
			name:       "Forbidden",
			err:        ox.Forbidden{},
			wantStatus: 403,
			wantJSON:   true,
			wantError:  "forbidden",
		},
		{
			name:       "ValidationError",
			err:        ox.ValidationError{},
			wantStatus: 422,
			wantJSON:   true,
			wantError:  "validation error",
		},
		{
			name:       "wrapped NotFound",
			err:        fmt.Errorf("wrap: %w", ox.NotFound{}),
			wantStatus: 404,
			wantJSON:   true,
			wantError:  "not found",
		},
		{
			name:       "plain error returns 500",
			err:        fmt.Errorf("something broke"),
			wantStatus: 500,
			wantJSON:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			HandleError(w, tt.err)

			require.Equal(t, tt.wantStatus, w.Code)

			if tt.wantJSON {
				require.Equal(t, "application/json", w.Header().Get("Content-Type"))
				var body map[string]string
				err := json.Unmarshal(w.Body.Bytes(), &body)
				require.NoError(t, err)
				require.Equal(t, tt.wantError, body["error"])
			} else {
				// http.Error uses text/plain
				require.Contains(t, w.Header().Get("Content-Type"), "text/plain")
				require.Contains(t, w.Body.String(), "internal server error")
			}
		})
	}
}

type closingReader struct {
	io.Reader
	closed bool
}

func (c *closingReader) Close() error {
	c.closed = true
	return nil
}

func TestWriteStream(t *testing.T) {
	t.Run("writes content type, length, and body", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := []byte("hello world")
		WriteStream(w, 200, &ox.Stream{
			ContentType:   "application/vnd.hollowcube.polar",
			Body:          bytes.NewReader(body),
			ContentLength: int64(len(body)),
		})

		require.Equal(t, 200, w.Code)
		require.Equal(t, "application/vnd.hollowcube.polar", w.Header().Get("Content-Type"))
		require.Equal(t, "11", w.Header().Get("Content-Length"))
		require.Equal(t, "hello world", w.Body.String())
	})

	t.Run("omits content-length when zero", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteStream(w, 200, &ox.Stream{
			ContentType: "application/octet-stream",
			Body:        bytes.NewReader([]byte("data")),
		})

		require.Equal(t, "", w.Header().Get("Content-Length"))
		require.Equal(t, "data", w.Body.String())
	})

	t.Run("defaults empty content type to octet-stream", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteStream(w, 200, &ox.Stream{
			Body: bytes.NewReader([]byte("x")),
		})

		require.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
	})

	t.Run("closes body when it implements io.Closer", func(t *testing.T) {
		w := httptest.NewRecorder()
		cr := &closingReader{Reader: bytes.NewReader([]byte("abc"))}
		WriteStream(w, 200, &ox.Stream{
			ContentType: "application/octet-stream",
			Body:        cr,
		})

		require.True(t, cr.closed, "expected body Close to have been called")
	})

	t.Run("nil stream returns 500 without panic", func(t *testing.T) {
		w := httptest.NewRecorder()
		require.NotPanics(t, func() {
			WriteStream(w, 200, nil)
		})
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("nil body returns 500 without panic", func(t *testing.T) {
		w := httptest.NewRecorder()
		require.NotPanics(t, func() {
			WriteStream(w, 200, &ox.Stream{ContentType: "application/octet-stream"})
		})
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("custom status code", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteStream(w, 202, &ox.Stream{
			ContentType: "application/octet-stream",
			Body:        bytes.NewReader([]byte("queued")),
		})

		require.Equal(t, 202, w.Code)
	})
}

func TestWriteSSE(t *testing.T) {
	type payload struct {
		Path string `json:"path"`
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/events", nil)

	seq := func(yield func(ox.Event[payload], error) bool) {
		if !yield(ox.Event[payload]{ID: "1", Name: "change", Data: payload{Path: "a.txt"}}, nil) {
			return
		}
		if !yield(ox.Event[payload]{ID: "2", Data: payload{Path: "b.txt"}}, nil) {
			return
		}
	}

	WriteSSE(w, r, seq)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	require.Equal(t, "no-cache", w.Header().Get("Cache-Control"))

	body := w.Body.String()
	require.Contains(t, body, "id: 1\n")
	require.Contains(t, body, "event: change\n")
	require.Contains(t, body, `data: {"path":"a.txt"}`+"\n\n")
	require.Contains(t, body, "id: 2\n")
	require.Contains(t, body, `data: {"path":"b.txt"}`+"\n\n")
}

func TestWriteSSE_ErrorEndsStream(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/events", nil)

	seq := func(yield func(ox.Event[string], error) bool) {
		if !yield(ox.Event[string]{ID: "1", Data: "ok"}, nil) {
			return
		}
		if !yield(ox.Event[string]{}, fmt.Errorf("boom")) {
			return
		}
		if !yield(ox.Event[string]{ID: "2", Data: "unreached"}, nil) {
			return
		}
	}

	WriteSSE(w, r, seq)

	body := w.Body.String()
	require.Contains(t, body, `data: "ok"`)
	require.NotContains(t, body, `"unreached"`)
}

func TestWriteBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	WriteBadRequest(w, "invalid input")

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)
	require.Equal(t, "invalid input", body["error"])
}
