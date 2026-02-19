package model

const (
	NotificationCreateAction = "create"
)

type NotificationUpdateMessage struct {
	Action   string                  `json:"action"`
	PlayerId string                  `json:"playerId"`
	Type     string                  `json:"type"`
	Key      string                  `json:"key"`
	Data     *map[string]interface{} `json:"data,omitempty"`
}

func (m NotificationUpdateMessage) Subject() string {
	return "notification.created"
}
