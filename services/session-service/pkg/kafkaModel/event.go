package kafkaModel

type SessionUpdateAction int

const (
	Session_Create SessionUpdateAction = iota
	Session_Delete
	Session_Update
)

type SessionUpdateMessage struct {
	Action   SessionUpdateAction `json:"action"`
	PlayerId string              `json:"playerId"`

	Session *Session `json:"session"` // Present for create, update

	Metadata map[string]interface{} `json:"metadata,omitempty"` // Present for update, sometimes
}
