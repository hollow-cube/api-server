package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"time"
)

func GenerateKey() ([]byte, error) {
	key := make([]byte, 30)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

func GenerateRecoveryCodes() ([]string, error) {
	recoveryCodes := make([]string, 6)
	for i := 0; i < 6; i++ {
		code, err := generateSingleRecoveryCode()
		if err != nil {
			return nil, err
		}
		recoveryCodes[i] = code
	}
	return recoveryCodes, nil
}

// generateSingleRecoveryCode generates a single recovery code in the format of xxxx-xxxx
func generateSingleRecoveryCode() (string, error) {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := 0; i < 8; i++ {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return fmt.Sprintf("%s-%s", b[:4], b[4:]), nil
}

func MakeURI(issuer, username string, secret []byte) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA512",
		issuer, username, base32.StdEncoding.EncodeToString(secret), issuer)
}

// GenerateCode Generates a TOTP value for the given key and time interval.
// Key is the previously generated key bytes
// Interval is the interval that is created ie. take the current seconds and divide it by the interval (normally 30)
func GenerateCode(key []byte, interval int64) (string, error) {
	mac := hmac.New(sha512.New, key)
	intervalBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(intervalBytes, uint64(interval))

	_, err := mac.Write(intervalBytes)
	if err != nil {
		return "", err
	}

	hash := mac.Sum(nil)
	offset := hash[len(hash)-1] & 0xF

	var truncatedHash int32
	for i := 0; i < 4; i++ {
		truncatedHash <<= 8
		truncatedHash |= int32(hash[int(offset)+i]) & 0xFF
	}

	truncatedHash &= 0x7FFFFFFF
	truncatedHash %= 1000000

	return fmt.Sprintf("%06d", truncatedHash), nil
}

func TestTriplet(key []byte, code string) (bool, error) {
	now := time.Now().Unix() / 30

	for i := -1; i < 2; i++ {
		testCode, err := GenerateCode(key, now+int64(i))
		if err != nil {
			return false, err
		}

		if testCode == code {
			return true, nil
		}
	}

	return false, nil
}
