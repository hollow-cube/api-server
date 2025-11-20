package public

import (
	"context"
	"net/http"

	strictnethttp "github.com/oapi-codegen/runtime/strictmiddleware/nethttp"
)

type contextKey string

const userIdContextKey contextKey = "userId"

func StaticApiUserFromContext(ctx context.Context) string {
	userId := ctx.Value(userIdContextKey)
	if userId == nil {
		return ""
	}

	return userId.(string)
}

var staticApiKeys = map[string]string{
	"BRchgmthmUj7x9q9VeqxMnGuUcswSnsC9zJhvfaydm5VGKXknT4KAf7EFUqfduJp": "7bd5b459-1e6b-4753-8274-1fbd2fe9a4d5", // emortaldev
	"huYKeUbQVgWfTWCvpWaSn5Wv2PgrX98eP26vxg8qSSNWsXp2PAC4UPnXq9fYfNm2": "aceb326f-da15-45bc-bf2f-11940c21780c", // notmattw
	"WHnFUGVNS6uJ4MAhgq8mxTDc279QsCaBZ5frtbK3edvLRpkwyXAhgq8mxTDc279Q": "5406471a-a19a-41cf-aa94-f8bcd8d6037a", // Virtued
}

var AuthMiddleware StrictMiddlewareFunc = func(f strictnethttp.StrictHTTPHandlerFunc, operationID string) strictnethttp.StrictHTTPHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (response interface{}, err error) {
		if r.URL.Path == "/v3/maps/stats" {
			return f(ctx, w, r.WithContext(ctx), request)
		}

		apiKey := r.Header.Get("x-hc-api-key")
		uuid, ok := staticApiKeys[apiKey]
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), userIdContextKey, uuid))
		return f(ctx, w, r.WithContext(ctx), request)
	}
}
