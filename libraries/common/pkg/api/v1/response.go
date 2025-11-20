package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	ErrCodeUnknown = 1
)

type Response struct {
	HTTPCode int    `json:"-"`                 // Default to 200 w/ payload, 500 w/ error
	TraceId  string `json:"traceId"`           // Always present
	Payload  any    `json:"payload,omitempty"` // Never present with Error
	Error    *Error `json:"error,omitempty"`   // Never present with Payload
}

func (r *Response) Write(ctx context.Context, w http.ResponseWriter) error {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().HasTraceID() {
		r.TraceId = span.SpanContext().TraceID().String()
	}

	// Write the status code or default
	httpCode := r.HTTPCode
	if httpCode == 0 {
		if r.Error != nil {
			httpCode = http.StatusInternalServerError
		} else {
			httpCode = http.StatusOK
		}
	}

	// If it is a special case, write the payload directly
	t := reflect.TypeOf(r.Payload)
	if t != nil && t.Implements(writableDataType) {
		data := r.Payload.(WritableData)
		return data.Write(w)
	}

	// Otherwise, write the response as JSON
	if r.Error != nil {
		span.RecordError(r.Error)
		if r.Error.Code == ErrCodeUnknown {
			zap.L().Error("http handler failure", zap.Error(r.Error))
		} else {
			zap.L().Info("http handler failure", zap.String("message", r.Error.String()))
		}
	}

	w.Header().Set("content-type", "application/json")
	w.WriteHeader(httpCode)
	return json.NewEncoder(w).Encode(r)
}

var writableDataType = reflect.TypeOf((*WritableData)(nil)).Elem()

type WritableData interface {
	Write(w http.ResponseWriter) error
}

type Error struct {
	Code    int    `json:"code"`             // Internal error code (not HTTP status code)
	Message string `json:"message"`          // Raw error message
	Detail  string `json:"detail,omitempty"` // User-facing detail, if any
	Source  error  `json:"-"`                // Source error, if any
}

func (e *Error) String() string {
	if e.Source != nil {
		return fmt.Sprintf("%s: %s", e.Message, e.Source.Error())
	}
	return e.Message
}

func (e *Error) Error() string {
	return e.String()
}

func WrapUnknown(err error, message ...string) *Response {
	m := "internal server error"
	if len(message) > 0 {
		m = message[0]
	}
	return &Response{
		HTTPCode: http.StatusInternalServerError,
		Error: &Error{
			Code:    ErrCodeUnknown,
			Message: m,
			Source:  err,
		},
	}
}

func NewError(httpCode, code int, message string, detail ...string) *Response {
	var detailSingle string
	if len(detail) > 0 {
		detailSingle = detail[0]
	}
	return &Response{
		HTTPCode: httpCode,
		Error: &Error{
			Code:    code,
			Message: message,
			Detail:  detailSingle,
		},
	}
}
