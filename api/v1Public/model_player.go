package v1Public

import (
	"github.com/hollow-cube/api-server/internal/db"
)

type PlayerActivityType string

const (
	PlayerActivityUnknown   PlayerActivityType = "unknown"
	PlayerActivityHub       PlayerActivityType = "hub"
	PlayerActivityPlaying   PlayerActivityType = "playing"
	PlayerActivityVerifying PlayerActivityType = "verifying"
	PlayerActivityBuilding  PlayerActivityType = "building"
)

type PlayerActivity struct {
	Type PlayerActivityType `json:"type"`
	Name *string            `json:"name"`
	Id   *string            `json:"id"`
	Code *string            `json:"code"`
}

type PlayerStatus struct {
	Online   bool            `json:"online"`
	Activity *PlayerActivity `json:"activity"`
}

func GetPlayerActivityTypeFromSession(session db.PlayerSession) PlayerActivityType {
	if session.PType != nil {
		switch *session.PType {
		case "mapmaker:hub":
			return PlayerActivityHub
		case "mapmaker:map":
			if session.PState != nil {
				switch *session.PState {
				case "editing":
					fallthrough
				case "testing":
					return PlayerActivityBuilding
				case "verifying":
					return PlayerActivityVerifying
				case "playing":
					fallthrough
				case "spectating":
					return PlayerActivityPlaying
				}
			}
		}
	}
	return PlayerActivityUnknown
}
