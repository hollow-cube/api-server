package model

type MapJoinInfoMessage struct {
	ServerId string `json:"serverId"` // The server which is going to host the map.

	PlayerId string `json:"playerId"`
	MapId    string `json:"mapId"`
	State    string `json:"state"`
}

func (m MapJoinInfoMessage) Subject() string {
	return "map-join.incoming"
}

type PlayerSkin struct {
	Texture   string `json:"texture,omitempty"`
	Signature string `json:"signature,omitempty"`
}
