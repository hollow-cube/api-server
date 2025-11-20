package model

import "time"

type VerificationType string

const (
	VerificationTypeDiscord VerificationType = "discord"
)

type PendingVerification struct {
	Type       VerificationType
	UserID     string
	UserSecret string
	Expiration time.Time
}
