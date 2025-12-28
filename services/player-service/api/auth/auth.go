package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	v31 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"go.uber.org/fx"

	auth "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"google.golang.org/genproto/googleapis/rpc/status"
)

const ApiKeyPrefix = "sk-hc-"

var publicEndpoints = []string{
	"/v2/players/stats",
	"/v2/players/recap/*",
	"/v2/payments/tebex/webhook",
	"/v2/payments/tebex/basket",
	"/v3/maps/stats",
}

type ServerParams struct {
	fx.In

	Store *db.Store
}

type Server struct {
	store *db.Store
}

func NewServer(params ServerParams) *Server {
	return &Server{
		store: params.Store,
	}
}

func (s *Server) Check(ctx context.Context, request *auth.CheckRequest) (res *auth.CheckResponse, err error) {
	path := request.Attributes.Request.Http.Path
	if isPublicEndpoint(path) {
		return &auth.CheckResponse{
			Status: &status.Status{Code: 0},
			HttpResponse: &auth.CheckResponse_OkResponse{
				OkResponse: &auth.OkHttpResponse{
					HeadersToRemove: []string{"x-auth-user"},
				},
			},
		}, nil
	}

	apiKeyStr := request.Attributes.Request.Http.Headers["x-api-key"]

	var apiKey db.ApiKey
	if strings.HasPrefix(apiKeyStr, ApiKeyPrefix) {
		apiKey, err = s.store.GetApiKeyByHash(ctx, apiKeyStr[len(ApiKeyPrefix):])
		if errors.Is(err, db.ErrNoRows) {
			apiKey.ID = ""
		} else if err != nil {
			return nil, err
		}
	}

	if apiKey.ID == "" {
		return &auth.CheckResponse{
			Status: &status.Status{Code: /*PERMISSION_DENIED*/ 7},
			HttpResponse: &auth.CheckResponse_DeniedResponse{
				DeniedResponse: &auth.DeniedHttpResponse{
					Status: &typev3.HttpStatus{Code: http.StatusUnauthorized},
					Body:   `{"error": "invalid api key"}`,
				},
			},
		}, nil
	}

	return &auth.CheckResponse{
		Status: &status.Status{Code: /*OK*/ 0},
		HttpResponse: &auth.CheckResponse_OkResponse{
			OkResponse: &auth.OkHttpResponse{
				Headers: []*v31.HeaderValueOption{
					{
						Header: &v31.HeaderValue{
							Key:   "x-auth-user",
							Value: apiKey.PlayerID,
						},
						// TODO: scopes or anything else
					},
				},
			},
		},
	}, nil
}

var _ auth.AuthorizationServer = (*Server)(nil)

func GenerateAPIKey() (key string, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}

	key = ApiKeyPrefix + base64.URLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(key))
	hash = hex.EncodeToString(h[:])
	return key, hash, nil
}

func isPublicEndpoint(path string) bool {
	if idx := strings.Index(path, "?"); idx != -1 {
		path = path[:idx]
	}
	for _, pattern := range publicEndpoints {
		if matchPath(pattern, path) {
			return true
		}
	}
	return false
}

func matchPath(pattern, path string) bool {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	if len(patternParts) != len(pathParts) {
		return false
	}

	for i, p := range patternParts {
		if p == "*" {
			continue
		}
		if p != pathParts[i] {
			return false
		}
	}
	return true
}
