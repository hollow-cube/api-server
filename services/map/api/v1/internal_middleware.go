package v1

import (
	"context"
	"net/http"
)

type contextKey string

const ContextKeyUser contextKey = "authorizer" // TODO

type AuthorizerMiddleware struct{}

func (*AuthorizerMiddleware) Run(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizer := r.Header.Get("x-hc-user-id")
		r = r.WithContext(context.WithValue(r.Context(), ContextKeyUser, authorizer))
		next.ServeHTTP(w, r)
	})
}
