package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hollow-cube/api-server/internal/pkg/util"
)

var b64 = base64.RawURLEncoding
var be = binary.BigEndian

var (
	ErrMalformed = errors.New("malformed access token")
	ErrBadMAC    = errors.New("bad access token signature")
	ErrExpired   = errors.New("access token expired")
)

// TokenKeyring manages signing/verification of (stateless) access tokens.
// The keys are: "v<keyID>.<b64url(session id + expiry time)>.<b64url(mac)>"
//
//	keyID: index into keys map
//	mac: prefix + payload signed with keys[keyID]
//
// Multiple keys exist to handle rotation
type TokenKeyring struct {
	active byte // key index to use for new tokens
	keys   map[byte][]byte
}

func NewTokenKeyring(active byte, keys map[byte][]byte) *TokenKeyring {
	if _, ok := keys[active]; !ok {
		panic("auth: active key not in keyring")
	}
	return &TokenKeyring{active: active, keys: keys}
}

func (kr *TokenKeyring) Mint(sessionID string, ttl time.Duration) string {
	var p [24]byte
	sid := uuid.MustParse(sessionID)
	copy(p[:16], sid[:])
	be.PutUint64(p[16:], uint64(time.Now().Add(ttl).Unix()))

	prefix := "v" + strconv.Itoa(int(kr.active)) + "." + b64.EncodeToString(p[:])
	mac := util.Mac256(kr.keys[kr.active], prefix)
	return prefix + "." + b64.EncodeToString(mac)
}

func (kr *TokenKeyring) Parse(tok string) (string, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || len(parts[0]) < 2 || parts[0][0] != 'v' {
		return "", ErrMalformed
	}

	keyId, err := strconv.Atoi(parts[0][1:])
	if err != nil || keyId < 0 || keyId > 255 {
		return "", ErrMalformed
	}
	key, ok := kr.keys[byte(keyId)]
	if !ok {
		return "", ErrBadMAC
	}

	expectedSignature := util.Mac256(key, parts[0]+"."+parts[1])
	actualSignature, err := b64.DecodeString(parts[2])
	if err != nil {
		return "", ErrMalformed
	}
	if subtle.ConstantTimeCompare(actualSignature, expectedSignature) != 1 {
		return "", ErrBadMAC
	}

	payload, err := b64.DecodeString(parts[1])
	if err != nil || len(payload) != 24 {
		return "", ErrMalformed
	}

	if time.Now().After(time.Unix(int64(be.Uint64(payload[16:])), 0)) {
		return "", ErrExpired
	}

	return uuid.UUID(payload[:16]).String(), nil
}
