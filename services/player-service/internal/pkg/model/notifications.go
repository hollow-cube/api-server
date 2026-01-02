package model

const (
	NotificationCreatedTopic = "notification_created"
)

type NotificationCreatedMessage struct {
	PlayerId string                  `json:"playerId"`
	Type     string                  `json:"type"`
	Key      string                  `json:"key"`
	Data     *map[string]interface{} `json:"data,omitempty"`
}
