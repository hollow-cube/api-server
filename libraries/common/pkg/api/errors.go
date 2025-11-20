package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func WrapError(err error) *Error {
	return &Error{
		HTTP:    http.StatusInternalServerError,
		Code:    "unknown",
		Message: err.Error(),
	}
}

type Error struct {
	HTTP    int                    `json:"-"`        // eg 400
	Code    string                 `json:"code"`     // eg `invalid_request`
	Message string                 `json:"message" ` // eg `Missing required parameter: "grant_type"`
	Context map[string]interface{} `json:"context,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Implement WritableError from github.com/mworzala/openapi-go
func (e *Error) Write(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(e.HTTP)
	if err := json.NewEncoder(w).Encode(e); err != nil {
		panic(err)
	}
}
