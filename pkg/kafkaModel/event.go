package kafkaModel

import "fmt"

type SessionUpdateAction int

const (
	Session_Create SessionUpdateAction = iota
	Session_Delete
	Session_Update
)

func (a SessionUpdateAction) String() string {
	return [...]string{"create", "delete", "update"}[a]
}

type SessionUpdateMessage struct {
	Action   SessionUpdateAction `json:"action"`
	PlayerId string              `json:"playerId"`

	Session *Session `json:"session"` // Present for create, update

	Metadata map[string]interface{} `json:"metadata,omitempty"` // Present for update, sometimes
}

func (m SessionUpdateMessage) Subject() string {
	return fmt.Sprintf("session.%v", m.Action)
}
