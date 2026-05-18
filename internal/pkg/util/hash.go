package util

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
)

func Sha256b(input []byte) []byte {
	h := sha256.New()
	h.Write(input)
	return h.Sum(nil)
}

func Sha256(input string) string {
	return fmt.Sprintf("%x", Sha256b([]byte(input)))
}

func Mac256(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}
