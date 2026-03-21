package player

type TransferMessage struct {
	PlayerId string `json:"playerId"`

	From  *string `json:"from"`  // A map id, 'hub', or nil for anywhere
	To    string  `json:"to"`    // A map id, or 'hub'
	State string  `json:"state"` // playing/building/verifying/etc. Must be playing for hub.
}

func (t TransferMessage) Subject() string {
	return "player.transfer"
}
