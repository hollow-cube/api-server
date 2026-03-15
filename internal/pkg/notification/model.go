package notification

import (
	"fmt"
)

type CreateInput struct {
	Key       string
	Type      string
	ExpiresIn *int

	ReplaceUnread bool

	Data map[string]any
}

type UpdateAction string

const (
	CreateAction = "create"
	DeleteAction = "delete"
)

type UpdateMessage struct {
	Action   UpdateAction   `json:"action"`
	PlayerId string         `json:"playerId"`
	Type     string         `json:"type"`
	Key      string         `json:"key"`
	Data     map[string]any `json:"data"`
}

func (m UpdateMessage) Subject() string {
	return fmt.Sprintf("notification.%sd", m.Action)
}
