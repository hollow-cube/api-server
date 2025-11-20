package intnl

import (
	"context"
	"net/http"

	strictnethttp "github.com/oapi-codegen/runtime/strictmiddleware/nethttp"
)

type contextKey string

const ContextKeyUser contextKey = "authorizer"

var AuthMiddleware StrictMiddlewareFunc = func(f strictnethttp.StrictHTTPHandlerFunc, operationID string) strictnethttp.StrictHTTPHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (response interface{}, err error) {
		authorizer := r.Header.Get("x-hc-user-id")
		ctx = context.WithValue(r.Context(), ContextKeyUser, authorizer)
		return f(ctx, w, r.WithContext(ctx), request)
	}
}
