package util

import (
	"math/rand"
	"strings"
)

func NewVerifySecret() string {
	var table = []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789")
	var tableSize = len(table)

	var sb strings.Builder
	for i := 0; i < 7; i++ {
		randomChar := table[rand.Intn(tableSize)]
		sb.WriteRune(randomChar)
	}

	return sb.String()
}
