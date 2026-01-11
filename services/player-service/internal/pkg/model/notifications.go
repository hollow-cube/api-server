package model

const (
	NotificationCreateAction = "create"
	NotificationDeleteAction = "delete"
)

type NotificationUpdateMessage struct {
	Action         string                  `json:"action"`
	PlayerId       string                  `json:"playerId"`
	NotificationId string                  `json:"notificationId,omitempty"` // Used by delete
	Type           string                  `json:"type,omitempty"`           // Used by create
	Key            string                  `json:"key,omitempty"`            // Used by create
	Data           *map[string]interface{} `json:"data,omitempty"`           // Used by create
}
