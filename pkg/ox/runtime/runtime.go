package runtime

import (
	"encoding/json"
	"errors"
	"net/http"

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
