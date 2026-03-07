package notification

import "time"

type CreateInput struct {
	Key       string
	Type      string
	ExpiresIn *int

	ReplaceUnread bool

	Data *map[string]interface{}
}

type Notification struct {
	CreatedAt time.Time               `json:"createdAt"`
	Data      *map[string]interface{} `json:"data,omitempty"`
	ExpiresAt *time.Time              `json:"expiresAt,omitempty"`
	Id        string                  `json:"id"`
	Key       string                  `json:"key"`
	ReadAt    *time.Time              `json:"readAt,omitempty"`
	Type      string                  `json:"type"`
}

type PaginatedNotifications struct {
	Page      int32          `json:"page"`
	PageCount int32          `json:"pageCount"`
	Results   []Notification `json:"results"`
}
