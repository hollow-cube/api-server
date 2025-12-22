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

type ServerParams struct {
	fx.In

	Queries *db.Queries
}

type Server struct {
	queries *db.Queries
}

func NewServer(params ServerParams) *Server {
	return &Server{
		queries: params.Queries,
	}
}

func (s *Server) Check(ctx context.Context, request *auth.CheckRequest) (res *auth.CheckResponse, err error) {
	apiKeyStr := request.Attributes.Request.Http.Headers["x-api-key"]

	var apiKey *db.ApiKey
	if strings.HasPrefix(apiKeyStr, ApiKeyPrefix) {
		apiKey, err = s.queries.GetApiKeyByHash(ctx, apiKeyStr[len(ApiKeyPrefix):])
		if err != nil && !errors.Is(err, db.ErrNoRows) {
			return nil, err
		}
	}

	if apiKey == nil {
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
