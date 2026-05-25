package auth

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"github.com/hollow-cube/api-server/internal/db"
	"github.com/hollow-cube/api-server/internal/pkg/util"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

// DPoP (RFC 9449) per-request proof. The proof is a short-lived JWS signed by
// the client's key; its embedded JWK *is* the client public key, and the
// base64url SHA-256 thumbprint of that JWK is the stable key_id we pin a
// session to. The same verification is used for the per-request access-token
// check and for the unauthenticated redeem/token bootstrap endpoints.

const (
	dpopTyp      = "dpop+jwt"
	maxDPoPAge   = 90 * time.Second
	maxClockSkew = 30 * time.Second
)

var dpopB64 = base64.RawURLEncoding

var (
	errProof       = errors.New("invalid proof")
	errReplay      = errors.New("proof replay")
	errKeyMismatch = errors.New("client key mismatch")

	validAlgorithms = map[jwa.SignatureAlgorithm]bool{jwa.ES256: true, jwa.EdDSA: true}
)

type DPoPParams struct {
	Proof       string
	Method      string
	Path        string
	AccessToken string
	ExpectKeyID string
}

func VerifyDPoP(ctx context.Context, redis rueidis.Client, externalURL string, p DPoPParams) (keyID string, derSPKI []byte, err error) {
	now := time.Now()

	// Parse the JWS untrusted to read the protected header.
	msg, err := jws.Parse([]byte(p.Proof))
	if err != nil || len(msg.Signatures()) != 1 {
		return "", nil, errProof
	}
	hdr := msg.Signatures()[0].ProtectedHeaders()
	if hdr.Type() != dpopTyp || !validAlgorithms[hdr.Algorithm()] {
		return "", nil, errProof
	}
	embedded := hdr.JWK()
	if embedded == nil {
		return "", nil, errProof
	}

	// The thumbprint of the embedded JWK is the key_id. Pin it to the
	// expected session key when one is supplied.
	thumb, err := embedded.Thumbprint(crypto.SHA256)
	if err != nil {
		return "", nil, errProof
	}
	keyID = dpopB64.EncodeToString(thumb)
	if p.ExpectKeyID != "" && p.ExpectKeyID != keyID {
		return "", nil, errKeyMismatch
	}

	// Verify the signature with the embedded JWK itself (NOT raw bytes — a
	// []byte cannot verify ES256/EdDSA).
	tok, err := jwt.Parse([]byte(p.Proof),
		jwt.WithKey(hdr.Algorithm(), embedded),
		jwt.WithValidate(true),
		jwt.WithAcceptableSkew(maxClockSkew),
	)
	if err != nil {
		return "", nil, errProof
	}

	pc := tok.PrivateClaims()
	if s, _ := pc["htm"].(string); !strings.EqualFold(s, p.Method) {
		return "", nil, errProof
	}
	if s, _ := pc["htu"].(string); s != buildHTU(externalURL, p.Path) {
		return "", nil, errProof
	}
	if p.AccessToken != "" {
		athWant := dpopB64.EncodeToString(util.Sha256b([]byte(p.AccessToken)))
		if s, _ := pc["ath"].(string); s != athWant {
			return "", nil, errProof
		}
	}

	iat := tok.IssuedAt()
	if iat.IsZero() || now.Sub(iat) > maxDPoPAge || iat.After(now.Add(maxClockSkew)) {
		return "", nil, errProof
	}

	// Only consume the jti once everything else is valid, so unsigned/forged
	// proofs cannot burn arbitrary nonces.
	jti := tok.JwtID()
	if jti == "" {
		return "", nil, errProof
	}
	if err = claimJTI(ctx, redis, jti); err != nil {
		return "", nil, err
	}

	var raw any
	if err = embedded.Raw(&raw); err != nil {
		return "", nil, errProof
	}
	der, err := x509.MarshalPKIXPublicKey(raw)
	if err != nil {
		return "", nil, errProof
	}
	return keyID, der, nil
}

// buildHTU reconstructs the public request URI (no query/fragment) from the
// configured external origin, since Envoy's internal scheme/host differ from
// what the client signed.
func buildHTU(externalURL, path string) string {
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if path != "" && path[0] != '/' {
		path = "/" + path
	}
	return strings.TrimRight(externalURL, "/") + path
}

// claimJTI atomically reserves a proof's jti for maxDPoPAge+maxClockSkew. A
// failed reserve (the nonce already exists) is a replay. The retention window
// must cover maxClockSkew on top of maxDPoPAge because a proof whose iat is up
// to maxClockSkew in the future is still accepted, so its effective validity
// extends that far past now.
func claimJTI(ctx context.Context, redis rueidis.Client, jti string) error {
	cmd := redis.B().Set().
		Key("dpop:jti:" + jti).
		Value("1").
		Nx().
		ExSeconds(int64((maxDPoPAge + maxClockSkew).Seconds())).
		Build()
	err := redis.Do(ctx, cmd).Error()
	if err == nil {
		return nil
	}
	if rueidis.IsRedisNil(err) {
		return errReplay
	}
	return err
}

func (s *Server) checkDpopToken(ctx context.Context, r *authv3.AttributeContext_HttpRequest, accessToken string) (authState, error) {
	// Stateless token validation to extract the session id.
	sessionID, err := s.keyring.Parse(accessToken)
	if err != nil {
		zap.S().Infow("access token parse failed", "path", r.Path, "err", err)
		return authState{}, nil
	}

	// DB lookup is the liveness/revocation gate.
	session, err := s.sessionStore.GetActiveSession(ctx, sessionID)
	if errors.Is(err, db.ErrNoRows) {
		zap.S().Infow("no active session for token", "path", r.Path, "sessionID", sessionID)
		return authState{}, nil
	} else if err != nil {
		return authState{}, err
	}

	_, _, err = VerifyDPoP(ctx, s.redis, s.externalURL, DPoPParams{
		Proof:       r.Headers["dpop"],
		Method:      r.Method,
		Path:        r.Path,
		AccessToken: accessToken,
		ExpectKeyID: session.ClientKeyID,
	})
	if err != nil {
		zap.S().Infow("proof verification failed",
			"path", r.Path,
			"method", r.Method,
			"sessionID", sessionID,
			"err", err,
		)
		return authState{}, nil
	}

	return authState{
		Valid:    true,
		PlayerID: session.PlayerID,
	}, nil
}
