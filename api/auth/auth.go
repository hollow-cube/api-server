package auth

import (
	"context"
	"net/http"
	"strings"

	v31 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/hollow-cube/api-server/config"
	"github.com/hollow-cube/api-server/internal/db"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"github.com/redis/rueidis"
	"go.uber.org/fx"
	"go.uber.org/zap"

	auth "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/genproto/googleapis/rpc/status"
)

var publicEndpoints = []string{
	"/_external/*",
	"/v1/auth/redeem",
	"/v1/auth/token",
	"/v2/players/recap/*",
	"/v2/payments/tebex/webhook",
	"/v2/payments/tebex/basket",
}

type ServerParams struct {
	fx.In

	Conf         *config.Config
	Keyring      *TokenKeyring
	PlayerStore  *playerdb.Store
	SessionStore *db.Queries
	Redis        rueidis.Client
}

type Server struct {
	keyring      *TokenKeyring
	playerStore  *playerdb.Store
	sessionStore *db.Queries
	redis        rueidis.Client
	externalURL  string
}

func NewServer(params ServerParams) *Server {
	return &Server{
		keyring:      params.Keyring,
		playerStore:  params.PlayerStore,
		sessionStore: params.SessionStore,
		redis:        params.Redis,
		externalURL:  params.Conf.Auth.ExternalURL,
	}
}

type authState struct {
	Valid    bool
	PlayerID string
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

	var result authState
	req := request.Attributes.Request.Http

	apiKeyStr := req.Headers["x-api-key"]
	if strings.HasPrefix(apiKeyStr, ApiKeyPrefix) {
		result, err = s.checkApiKey(ctx, apiKeyStr)
	} else {
		authorization := req.Headers["authorization"]
		if strings.HasPrefix(authorization, "DPoP ") {
			rawToken := strings.TrimPrefix(authorization, "DPoP ")
			result, err = s.checkDpopToken(ctx, req, rawToken)
		}
	}

	// Fail closed: an internal error must never become a transport error that
	// a fail-open Envoy could admit. Deny, and don't leak the reason.
	if err != nil {
		zap.S().Errorw("auth check failed internally", "path", path, "error", err)
		return deny(), nil
	}
	if !result.Valid {
		return deny(), nil
	}

	return &auth.CheckResponse{
		Status: &status.Status{Code: /*OK*/ 0},
		HttpResponse: &auth.CheckResponse_OkResponse{
			OkResponse: &auth.OkHttpResponse{
				Headers: []*v31.HeaderValueOption{
					{
						Header: &v31.HeaderValue{
							Key:   "x-auth-user",
							Value: result.PlayerID,
						},
						AppendAction: v31.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
					},
				},
			},
		},
	}, nil
}

// deny is the single 401 response for every authn failure — generic body so
// nothing about the token, proof, or internal state leaks downstream.
func deny() *auth.CheckResponse {
	return &auth.CheckResponse{
		Status: &status.Status{Code: /*PERMISSION_DENIED*/ 7},
		HttpResponse: &auth.CheckResponse_DeniedResponse{
			DeniedResponse: &auth.DeniedHttpResponse{
				Status: &typev3.HttpStatus{Code: http.StatusUnauthorized},
				Body:   `{"error": "unauthorized"}`,
			},
		},
	}
}

var _ auth.AuthorizationServer = (*Server)(nil)

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
