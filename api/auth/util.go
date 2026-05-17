package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
)

const ApiKeyPrefix = "sk-hc-"

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

type authContextKey string

func SetFromHeaders(r *http.Request) *http.Request {
	// Set by envoy so we know its valid.
	authUser := r.Header.Get("x-auth-user")
	if authUser == "" {
		return r
	}

	return r.WithContext(context.WithValue(r.Context(), authContextKey("playerID"), authUser))
}

func GetPlayerID(ctx context.Context) (string, bool) {
	playerID, ok := ctx.Value(authContextKey("playerID")).(string)
	return playerID, ok
}
